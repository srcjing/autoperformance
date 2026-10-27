package report

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"auto_performance/internal/domain"
	"auto_performance/internal/evaluator"
)

// TestGenerateForTaskCreatesArtifacts 任务完成后全量生成 html/pdf/word，产物落在报告目录内。
func TestGenerateForTaskCreatesArtifacts(t *testing.T) {
	repo := newFakeReportRepo()
	task := sampleTask(9)
	repo.tasks[9] = task
	pdf := &fakePDF{}
	svc := newTestService(t, repo, Options{PDF: pdf})

	if err := svc.Generate(context.Background(), task, evaluator.Evaluation{}); err != nil {
		t.Fatalf("生成报告失败: %v", err)
	}

	reports, total, err := repo.ListReports(context.Background(), domain.ReportQuery{Page: 1, PageSize: 10})
	if err != nil || total != 1 {
		t.Fatalf("应生成 1 份报告记录, total=%d err=%v", total, err)
	}
	rep := reports[0]
	if rep.Status != domain.ReportStatusReady {
		t.Fatalf("报告状态 = %s, want %s", rep.Status, domain.ReportStatusReady)
	}
	if rep.Source != domain.ReportSourceInternal || rep.TaskID != 9 {
		t.Fatalf("报告来源/归属错误: %+v", rep)
	}
	if len(rep.Artifacts) != 3 {
		t.Fatalf("产物数量 = %d, want 3（html/pdf/word）", len(rep.Artifacts))
	}
	if pdf.calls != 1 {
		t.Fatalf("PDF 生成调用次数 = %d, want 1", pdf.calls)
	}
	for _, format := range []string{domain.ReportFormatHTML, domain.ReportFormatPDF, domain.ReportFormatWord} {
		if !hasFormat(rep.Artifacts, format) {
			t.Fatalf("缺少 %s 产物", format)
		}
	}
	for _, artifact := range rep.Artifacts {
		if !strings.HasPrefix(artifact.Path, svc.Dir()+string(os.PathSeparator)) {
			t.Fatalf("产物路径不在报告目录内: %s", artifact.Path)
		}
		info, err := os.Stat(artifact.Path)
		if err != nil || info.Size() == 0 {
			t.Fatalf("产物文件不存在或为空: %s (%v)", artifact.Path, err)
		}
		if info.Mode().Perm()&0o111 != 0 {
			t.Fatalf("产物不应具备可执行权限: %s (%v)", artifact.Path, info.Mode())
		}
	}

	summary := rep.Summary
	if summary == nil {
		t.Fatalf("报告摘要为空")
	}
	if summary.TotalInterfaces != 2 || summary.PassedInterfaces != 1 || summary.PassedThresholds != 1 {
		t.Fatalf("达标统计错误: %+v", summary)
	}
	if summary.PassRate != 50 || len(summary.Items) != 2 {
		t.Fatalf("达标率/逐条摘要错误: %+v", summary)
	}
	if summary.DurationSeconds != 65 {
		t.Fatalf("执行用时 = %v, want 65", summary.DurationSeconds)
	}
}

// TestPathTraversalBlocked 产物路径越界（../、绝对路径外置、非法 format）一律 24001（DES-T-002）。
func TestPathTraversalBlocked(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "reports")

	// 报告根目录之外的「真实存在」文件：用于验证前缀校验而非仅存在性检查。
	outside := filepath.Join(base, "secret.html")
	if err := os.WriteFile(outside, []byte("TOP-SECRET"), 0o600); err != nil {
		t.Fatalf("准备越界文件失败: %v", err)
	}

	inside := filepath.Join(dir, "1", "report.html")
	if err := os.MkdirAll(filepath.Dir(inside), 0o750); err != nil {
		t.Fatalf("准备报告目录失败: %v", err)
	}
	if err := os.WriteFile(inside, []byte("<html>ok</html>"), 0o640); err != nil {
		t.Fatalf("准备报告文件失败: %v", err)
	}

	repo := newFakeReportRepo()
	svc := newTestService(t, repo, Options{Dir: dir})

	cases := []struct {
		name   string
		format string
		path   string
	}{
		{"绝对路径越界（真实存在）", domain.ReportFormatHTML, outside},
		{"相对路径", domain.ReportFormatHTML, "report.html"},
		{"Clean 后越界", domain.ReportFormatHTML, filepath.Join(dir, "1", "..", "..", "secret.html")},
		{"目录而非文件", domain.ReportFormatHTML, filepath.Dir(inside)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rep := &domain.Report{
				ID:     1,
				Type:   domain.ReportFormatHTML,
				Source: domain.ReportSourceInternal,
				Status: domain.ReportStatusReady,
			}
			repo.reports[1] = rep
			repo.artifacts[1] = []domain.ReportArtifact{{ReportID: 1, Format: tc.format, Path: tc.path}}

			_, err := svc.Artifact(context.Background(), 1, tc.format)
			if err == nil {
				t.Fatalf("越界路径不应放行: %s", tc.path)
			}
			if !errors.Is(err, ErrReportFileNotFound) || errCode(err) != 24001 {
				t.Fatalf("越界路径应返回 24001, got %v", err)
			}
		})
	}

	// 非法 format 参数（../../etc/passwd）同样返回 24001，且不读取任何文件。
	repo.artifacts[1] = []domain.ReportArtifact{{ReportID: 1, Format: domain.ReportFormatHTML, Path: inside}}
	if _, err := svc.Artifact(context.Background(), 1, "../../etc/passwd"); errCode(err) != 24001 {
		t.Fatalf("非法 format 应返回 24001, got %v", err)
	}

	// 合法产物可正常解析（对照组）。
	file, err := svc.Artifact(context.Background(), 1, domain.ReportFormatHTML)
	if err != nil {
		t.Fatalf("合法产物解析失败: %v", err)
	}
	if !file.Inline || file.ContentType != "text/html; charset=utf-8" {
		t.Fatalf("HTML 应内联查看: %+v", file)
	}
	raw, err := os.ReadFile(file.Path)
	if err != nil || string(raw) != "<html>ok</html>" {
		t.Fatalf("读取产物失败: %v (%s)", err, string(raw))
	}
}

// TestArtifactNotFound 报告不存在 / 格式不存在 → 24001。
func TestArtifactNotFound(t *testing.T) {
	repo := newFakeReportRepo()
	svc := newTestService(t, repo, Options{})

	if _, err := svc.Artifact(context.Background(), 999, domain.ReportFormatHTML); errCode(err) != 24001 {
		t.Fatalf("报告不存在应返回 24001, got %v", err)
	}
	repo.reports[5] = &domain.Report{ID: 5, Type: domain.ReportFormatHTML, Status: domain.ReportStatusReady}
	if _, err := svc.Artifact(context.Background(), 5, domain.ReportFormatPDF); errCode(err) != 24001 {
		t.Fatalf("格式不存在应返回 24001, got %v", err)
	}
}

// TestRegenerateMarksFailed 生成失败：状态置 generation_failed 且返回 24004（可重新生成）。
func TestRegenerateMarksFailed(t *testing.T) {
	repo := newFakeReportRepo()
	task := sampleTask(9)
	repo.tasks[9] = task
	repo.reports[1] = &domain.Report{
		ID: 1, TaskID: 9, Source: domain.ReportSourceInternal,
		Type: domain.ReportFormatHTML, Title: "订单服务压测 性能测试报告",
		Status: domain.ReportStatusGenerationFailed,
	}
	pdf := &fakePDF{err: errors.New("chromium 不可用")}
	svc := newTestService(t, repo, Options{PDF: pdf})

	_, err := svc.Regenerate(context.Background(), 1)
	if err == nil {
		t.Fatalf("PDF 失败时应返回错误")
	}
	if !errors.Is(err, ErrReportGenerateFailed) || errCode(err) != 24004 {
		t.Fatalf("生成失败应返回 24004, got %v", err)
	}
	if got := repo.reports[1].Status; got != domain.ReportStatusGenerationFailed {
		t.Fatalf("失败后状态 = %s, want %s", got, domain.ReportStatusGenerationFailed)
	}
}

// TestRegenerateRejectsGenerating 生成中禁止并发重复生成（10401）。
func TestRegenerateRejectsGenerating(t *testing.T) {
	repo := newFakeReportRepo()
	repo.reports[2] = &domain.Report{
		ID: 2, TaskID: 9, Source: domain.ReportSourceInternal,
		Type: domain.ReportFormatHTML, Status: domain.ReportStatusGenerating,
	}
	svc := newTestService(t, repo, Options{})

	if _, err := svc.Regenerate(context.Background(), 2); errCode(err) != 10401 {
		t.Fatalf("生成中重新生成应返回 10401, got %v", err)
	}
}

// TestRegenerateExternalImportRejected 外部导入报告无任务数据，不可重新生成（24004）。
func TestRegenerateExternalImportRejected(t *testing.T) {
	repo := newFakeReportRepo()
	repo.reports[3] = &domain.Report{
		ID: 3, Source: domain.ReportSourceExternal,
		Type: domain.ReportFormatPDF, Status: domain.ReportStatusReady,
	}
	svc := newTestService(t, repo, Options{})

	if _, err := svc.Regenerate(context.Background(), 3); !errors.Is(err, ErrReportGenerateFailed) {
		t.Fatalf("外部导入报告重新生成应返回 24004, got %v", err)
	}
}

// TestListValidatesFilters 列表筛选参数白名单校验（10003）。
func TestListValidatesFilters(t *testing.T) {
	repo := newFakeReportRepo()
	svc := newTestService(t, repo, Options{})

	for _, q := range []ListQuery{
		{Type: "exe"},
		{Status: "unknown"},
		{Source: "other"},
		{CreatedFrom: timePtr(time.Now()), CreatedTo: timePtr(time.Now().Add(-time.Hour))},
	} {
		if _, err := svc.List(context.Background(), q); errCode(err) != 10003 {
			t.Errorf("非法筛选条件应返回 10003: %+v → %v", q, err)
		}
	}
}

// timePtr 时间指针辅助。
func timePtr(t time.Time) *time.Time { return &t }
