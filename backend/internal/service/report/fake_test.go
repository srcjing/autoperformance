package report

import (
	"context"
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
	"auto_performance/internal/repository"
)

// fakeReportRepo 内存报告仓储（无 DB 依赖）：实现 service/report.Repo 全量方法。
type fakeReportRepo struct {
	mu        sync.Mutex
	reports   map[int64]*domain.Report
	artifacts map[int64][]domain.ReportArtifact
	tasks     map[int64]*domain.TestTask
	nextID    int64

	// listErr 模拟仓储异常。
	listErr error
}

func newFakeReportRepo() *fakeReportRepo {
	return &fakeReportRepo{
		reports:   map[int64]*domain.Report{},
		artifacts: map[int64][]domain.ReportArtifact{},
		tasks:     map[int64]*domain.TestTask{},
		nextID:    100,
	}
}

func (f *fakeReportRepo) WithTx(ctx context.Context, fn repository.TxFunc) error {
	return fn(ctx, nil)
}

func (f *fakeReportRepo) clone(rep *domain.Report) *domain.Report {
	cp := *rep
	if rep.Summary != nil {
		summary := *rep.Summary
		summary.Items = append([]domain.ReportSummaryItem(nil), rep.Summary.Items...)
		cp.Summary = &summary
	}
	cp.Artifacts = append([]domain.ReportArtifact(nil), rep.Artifacts...)
	return &cp
}

func (f *fakeReportRepo) ListReports(_ context.Context, q domain.ReportQuery) ([]domain.Report, int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, 0, f.listErr
	}
	out := make([]domain.Report, 0, len(f.reports))
	for _, rep := range f.reports {
		if q.TaskID > 0 && rep.TaskID != q.TaskID {
			continue
		}
		if q.Status != "" && rep.Status != q.Status {
			continue
		}
		if q.Source != "" && rep.Source != q.Source {
			continue
		}
		if q.Keyword != "" && !strings.Contains(rep.Title, q.Keyword) {
			continue
		}
		if q.Type != "" && rep.Type != q.Type && !hasFormat(f.artifacts[rep.ID], q.Type) {
			continue
		}
		clone := f.clone(rep)
		clone.Artifacts = append([]domain.ReportArtifact(nil), f.artifacts[rep.ID]...)
		out = append(out, *clone)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	total := int64(len(out))
	start := (q.Page - 1) * q.PageSize
	if start < 0 {
		start = 0
	}
	if start >= len(out) {
		return []domain.Report{}, total, nil
	}
	end := start + q.PageSize
	if end > len(out) {
		end = len(out)
	}
	return out[start:end], total, nil
}

func hasFormat(artifacts []domain.ReportArtifact, format string) bool {
	for _, artifact := range artifacts {
		if artifact.Format == format {
			return true
		}
	}
	return false
}

func (f *fakeReportRepo) GetReport(_ context.Context, id int64) (*domain.Report, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rep, ok := f.reports[id]
	if !ok {
		return nil, errcode.ErrNotFound
	}
	clone := f.clone(rep)
	clone.Artifacts = append([]domain.ReportArtifact(nil), f.artifacts[id]...)
	return clone, nil
}

func (f *fakeReportRepo) CreateReport(_ context.Context, _ repository.Tx, rep *domain.Report) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	now := time.Now()
	rep.ID = f.nextID
	rep.Version = 1
	rep.CreatedAt = now
	rep.UpdatedAt = now
	f.reports[rep.ID] = f.clone(rep)
	return nil
}

func (f *fakeReportRepo) UpdateReport(_ context.Context, _ repository.Tx, id int64, status string,
	summary *domain.ReportSummary, metrics []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rep, ok := f.reports[id]
	if !ok {
		return errcode.ErrNotFound
	}
	rep.Status = status
	rep.UpdatedAt = time.Now()
	rep.Version++
	if summary != nil {
		rep.Summary = summary
	}
	if len(metrics) > 0 {
		rep.Metrics = metrics
	}
	return nil
}

func (f *fakeReportRepo) ReplaceArtifacts(_ context.Context, _ repository.Tx, reportID int64, artifacts []domain.ReportArtifact) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	stored := make([]domain.ReportArtifact, 0, len(artifacts))
	for i := range artifacts {
		artifact := artifacts[i]
		artifact.ID = int64(i + 1)
		artifact.ReportID = reportID
		artifact.CreatedAt = time.Now()
		artifacts[i] = artifact
		stored = append(stored, artifact)
	}
	f.artifacts[reportID] = stored
	return nil
}

// count 返回报告记录数量（断言辅助）。
func (f *fakeReportRepo) count() (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.reports), nil
}

func (f *fakeReportRepo) GetTask(_ context.Context, id int64) (*domain.TestTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	task, ok := f.tasks[id]
	if !ok {
		return nil, errcode.ErrNotFound
	}
	cp := *task
	return &cp, nil
}

// fakePDF 假 PDF 生成器：读取 HTML 并写出带 %PDF- magic 的文件（可注入失败）。
type fakePDF struct {
	calls int
	err   error
}

func (f *fakePDF) Generate(_ context.Context, htmlPath, outPath string) error {
	f.calls++
	if f.err != nil {
		return f.err
	}
	raw, err := os.ReadFile(htmlPath)
	if err != nil {
		return err
	}
	return os.WriteFile(outPath, append([]byte("%PDF-1.4\n"), raw...), 0o640)
}

// newTestService 构造报告服务（临时目录 + 假 PDF + 固定时钟）。
func newTestService(t *testing.T, repo Repo, opts Options) *Service {
	t.Helper()
	if opts.Dir == "" {
		opts.Dir = t.TempDir()
	}
	if opts.Clock == nil {
		opts.Clock = func() time.Time { return time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC) }
	}
	if opts.PDF == nil {
		opts.PDF = &fakePDF{}
	}
	svc, err := New(repo, opts)
	if err != nil {
		t.Fatalf("构造报告服务失败: %v", err)
	}
	return svc
}

// errCode 提取业务错误码（非 errcode 错误返回 0）。
func errCode(err error) int {
	var e *errcode.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return 0
}

// sampleTask 构造带执行结果的任务（指标 + 评估 + 接口快照 + 报告配置）。
func sampleTask(id int64) *domain.TestTask {
	return &domain.TestTask{
		ID:           id,
		Name:         "订单服务压测",
		EnvID:        3,
		ProductIDs:   []int64{4},
		ReportConfig: []byte(`{"formats":["html","pdf","word"]}`),
		InterfaceSnapshots: []domain.InterfaceSnapshot{
			{InterfaceID: 11, Name: "查询订单", Method: "GET", URL: "https://api.example.test/orders?token=abc"},
			{InterfaceID: 12, Name: "创建订单", Method: "POST", URL: "https://api.example.test/orders"},
		},
		Result: []byte(`{
			"engine": "k6",
			"duration_seconds": 65,
			"metrics": {"p95": 210, "error_rate": 2.5, "throughput": 128.4},
			"evaluation": {
				"state": "fail",
				"pass": false,
				"items": [
					{"interface_id": 11, "interface_name": "查询订单", "metric": "p95", "operator": "le",
					 "threshold": 500, "actual": 210, "has_actual": true, "pass": true},
					{"interface_id": 12, "interface_name": "创建订单", "metric": "error_rate", "operator": "le",
					 "threshold": 1, "actual": 2.5, "has_actual": true, "pass": false,
					 "reason": "error_rate=2.5 未满足 le 1"}
				]
			}
		}`),
	}
}

// reportPath 报告产物路径（测试断言使用）。
func reportPath(svc *Service, reportID int64, name string) string {
	return svc.Dir() + string(os.PathSeparator) + strconv.FormatInt(reportID, 10) + string(os.PathSeparator) + name
}
