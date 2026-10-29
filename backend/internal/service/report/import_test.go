package report

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"auto_performance/internal/domain"
)

// buildDocxBytes 生成最小可用 docx（zip + word/document.xml），用于导入白名单校验。
func buildDocxBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatalf("创建 docx 条目失败: %v", err)
	}
	if _, err := w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body/></w:document>`)); err != nil {
		t.Fatalf("写入 docx 内容失败: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关闭 docx 归档失败: %v", err)
	}
	return buf.Bytes()
}

// importFixture 构造导入输入。
func importFixture(name string, content []byte, taskID, productID int64) ImportInput {
	return ImportInput{
		Filename:  name,
		Size:      int64(len(content)),
		Content:   bytes.NewReader(content),
		TaskID:    taskID,
		ProductID: productID,
	}
}

// TestImportRejectsDisguisedFile 恶意/伪装文件一律 24002（DES-T-001 双校验）。
func TestImportRejectsDisguisedFile(t *testing.T) {
	php := []byte("<?php system($_GET['c']); ?>")
	zipMagicOnly := []byte("PK\x03\x04not-a-real-docx")

	cases := []struct {
		name     string
		filename string
		content  []byte
	}{
		{"脚本扩展名", "shell.php", php},
		{"PDF 伪装（PHP 内容）", "report.pdf", php},
		{"DOCX 伪装（非 zip）", "report.docx", []byte("plain text, not a zip")},
		{"DOCX 伪装（普通 zip 无主文档）", "report.docx", zipMagicOnly},
		{"HTML 伪装（二进制内容）", "report.html", []byte{0x00, 0x01, 0x02, 0xff}},
		{"无扩展名", "report", php},
	}

	repo := newFakeReportRepo()
	svc := newTestService(t, repo, Options{})
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Import(context.Background(), importFixture(tc.filename, tc.content, 0, 0))
			if err == nil {
				t.Fatalf("非法文件不应导入成功: %s", tc.filename)
			}
			if !errors.Is(err, ErrReportTypeNotAllowed) || errCode(err) != 24002 {
				t.Fatalf("非法文件应返回 24002, got %v", err)
			}
		})
	}

	// 失败导入不得留下任何文件（临时文件已清理）。
	var leftover []string
	_ = filepath.WalkDir(filepath.Join(svc.Dir(), "imports"), func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry != nil && !entry.IsDir() {
			leftover = append(leftover, path)
		}
		return nil
	})
	if len(leftover) > 0 {
		t.Fatalf("非法导入不应残留文件: %v", leftover)
	}
	if total, _ := repo.count(); total != 0 {
		t.Fatalf("非法导入不应产生报告记录, got %d", total)
	}
}

// TestImportAcceptsValidFiles 合法 html/pdf/docx 可导入归档：随机名、目录内、摘要含 SHA256。
func TestImportAcceptsValidFiles(t *testing.T) {
	docx := buildDocxBytes(t)
	cases := []struct {
		name     string
		filename string
		content  []byte
		format   string
	}{
		{"HTML 报告", "外部报告.html", []byte("<!DOCTYPE html><html><body>report</body></html>"), domain.ReportFormatHTML},
		{"PDF 报告", "外部报告.pdf", []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\ntrailer\n%%EOF\n"), domain.ReportFormatPDF},
		{"DOCX 报告", "外部报告.docx", docx, domain.ReportFormatWord},
	}

	repo := newFakeReportRepo()
	svc := newTestService(t, repo, Options{})
	seen := make(map[string]bool)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			view, err := svc.Import(context.Background(), importFixture(tc.filename, tc.content, 0, 3))
			if err != nil {
				t.Fatalf("合法文件应导入成功: %v", err)
			}
			if view.Status != domain.ReportStatusReady || view.Type != tc.format {
				t.Fatalf("报告状态/格式错误: %+v", view)
			}
			if view.Source != domain.ReportSourceExternal {
				t.Fatalf("无任务归属应为 external, got %s", view.Source)
			}
			if len(view.Artifacts) != 1 {
				t.Fatalf("导入报告应只有 1 个产物, got %d", len(view.Artifacts))
			}
			artifact := view.Artifacts[0]
			if !strings.HasPrefix(artifact.Path, svc.Dir()+string(os.PathSeparator)+"imports"+string(os.PathSeparator)) {
				t.Fatalf("导入文件未落在 imports 目录: %s", artifact.Path)
			}
			base := filepath.Base(artifact.Path)
			if base == tc.filename {
				t.Fatalf("不得沿用用户文件名落盘: %s", base)
			}
			if seen[base] {
				t.Fatalf("随机文件名重复: %s", base)
			}
			seen[base] = true
			if !strings.HasSuffix(base, domain.ReportExtension(tc.format)) {
				t.Fatalf("落盘扩展名错误: %s", base)
			}
			if strings.Contains(base, "外部报告") {
				t.Fatalf("落盘文件名不应包含用户输入: %s", base)
			}
			info, err := os.Stat(artifact.Path)
			if err != nil || info.Size() != int64(len(tc.content)) {
				t.Fatalf("导入文件不存在或大小不符: %v", err)
			}
			if info.Mode().Perm()&0o111 != 0 {
				t.Fatalf("导入文件不应可执行: %v", info.Mode())
			}

			if view.Summary == nil {
				t.Fatalf("导入报告应记录摘要（原始文件名/SHA256）")
			}
			want := sha256.Sum256(tc.content)
			if view.Summary.SHA256 != hex.EncodeToString(want[:]) {
				t.Fatalf("SHA256 不匹配: %s", view.Summary.SHA256)
			}
			if view.Summary.OriginalFilename != tc.filename {
				t.Fatalf("原始文件名记录错误: %s", view.Summary.OriginalFilename)
			}
			if !strings.HasPrefix(view.Title, "外部导入") {
				t.Fatalf("外部导入报告标题应标注「外部导入」: %s", view.Title)
			}
		})
	}
}

// TestImportTooLarge 超过大小上限一律 24003（声明大小与真实流两条路径）。
func TestImportTooLarge(t *testing.T) {
	repo := newFakeReportRepo()
	svc := newTestService(t, repo, Options{MaxImportSize: 64})

	big := bytes.Repeat([]byte("a"), 128)
	in := importFixture("report.html", big, 0, 0)
	if _, err := svc.Import(context.Background(), in); !errors.Is(err, ErrReportTooLarge) || errCode(err) != 24003 {
		t.Fatalf("超大文件应返回 24003, got %v", err)
	}

	// 声明大小未知（0）时仍需按实际字节数拦截。
	unknown := ImportInput{Filename: "report.html", Content: bytes.NewReader(big)}
	if _, err := svc.Import(context.Background(), unknown); !errors.Is(err, ErrReportTooLarge) {
		t.Fatalf("声明大小未知时仍应拦截超大文件, got %v", err)
	}
}

// TestImportBoundTask 指定任务导入：任务必须存在，来源标记 internal。
func TestImportBoundTask(t *testing.T) {
	repo := newFakeReportRepo()
	svc := newTestService(t, repo, Options{})
	content := []byte("<!DOCTYPE html><html><body>bound</body></html>")

	if _, err := svc.Import(context.Background(), importFixture("r.html", content, 999, 0)); errCode(err) != 10301 {
		t.Fatalf("任务不存在应返回 10301, got %v", err)
	}

	repo.tasks[7] = &domain.TestTask{ID: 7, Name: "任务"}
	view, err := svc.Import(context.Background(), importFixture("r.html", content, 7, 0))
	if err != nil {
		t.Fatalf("归属任务导入失败: %v", err)
	}
	if view.Source != domain.ReportSourceInternal || view.TaskID != 7 {
		t.Fatalf("归属任务导入来源/归属错误: %+v", view)
	}
	if !strings.HasPrefix(view.Title, "导入报告") {
		t.Fatalf("任务归属导入标题应标注「导入报告」: %s", view.Title)
	}
}

// TestSanitizeFilename 展示文件名去路径穿越/控制字符（不参与落盘）。
func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd":   "passwd",
		`C:\temp\report.pdf`: "report.pdf",
		"a\x00b.html":        "ab.html",
		"..":                 "report",
		"":                   "report",
	}
	for in, want := range cases {
		if got := sanitizeFilename(in); got != want {
			t.Errorf("sanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}
