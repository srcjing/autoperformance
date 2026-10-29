package report

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
	"auto_performance/internal/repository"
)

// headBytes 文件头读取长度（http.DetectContentType 最多读取 512 字节）。
const headBytes = 512

// ImportInput 导入报告输入（multipart 文件流 + 可选归属任务/产品）。
type ImportInput struct {
	// Filename 浏览器上传的原始文件名（仅用于扩展名校验与展示，不参与落盘路径）。
	Filename string
	// Size 客户端声明的大小（0 表示未知；实际大小由流式写入时二次限制）。
	Size int64
	// Content 文件内容流。
	Content io.Reader
	// TaskID 归属任务（0 = 孤立归档，source=external）。
	TaskID int64
	// ProductID 归属产品（可选）。
	ProductID int64
}

// Import 导入外部报告（DES-T-001）。
//
// 安全流程：
//  1. 扩展名白名单（html/htm/pdf/docx）→ 24002；
//  2. 大小限制（先看声明大小，再在流式写入时硬限制）→ 24003；
//  3. 内容 magic bytes 双校验（pdf 前缀/ docx zip + word/document.xml / html 文本）→ 24002；
//  4. 随机文件名落盘（crypto/rand，忽略用户文件名），目录固定为 <reportDir>/imports/<yyyymmdd>/；
//  5. 记录 SHA256 便于溯源（DES-R-001 审计）。
func (s *Service) Import(ctx context.Context, in ImportInput) (*View, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	ext := strings.ToLower(filepath.Ext(filepath.Base(filepath.FromSlash(in.Filename))))
	format, ok := domain.ReportTypeByExtension(ext)
	if !ok {
		return nil, ErrReportTypeNotAllowed
	}
	if in.Content == nil {
		return nil, errcode.ErrParamMiss
	}
	if in.TaskID < 0 || in.ProductID < 0 {
		return nil, errcode.ErrParamFmt
	}
	limit := s.MaxImportSize()
	if in.Size > limit {
		return nil, ErrReportTooLarge
	}
	if in.TaskID > 0 {
		// 归属任务的导入报告：任务必须存在（不存在 → 10301）。
		if _, err := s.repo.GetTask(ctx, in.TaskID); err != nil {
			return nil, err
		}
	}

	displayName := sanitizeFilename(in.Filename)
	dir := filepath.Join(s.dir, "imports", s.now().UTC().Format("20060102"))
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("创建导入目录失败: %w", err)
	}

	// 先写临时文件（0600），校验通过后再以随机名归档，避免不完整/非法文件落到正式路径。
	tmp, err := os.CreateTemp(dir, ".upload-*")
	if err != nil {
		return nil, fmt.Errorf("创建导入临时文件失败: %w", err)
	}
	tmpPath := tmp.Name()
	written, digest, copyErr := copyWithLimit(tmp, in.Content, limit)
	closeErr := tmp.Close()
	if copyErr != nil {
		_ = os.Remove(tmpPath)
		return nil, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("写入导入文件失败: %w", closeErr)
	}
	if err := validateReportFile(tmpPath, ext); err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}

	name, err := randomFilename(ext)
	if err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}
	finalPath := filepath.Join(dir, name)
	if err := os.Rename(tmpPath, finalPath); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("归档导入文件失败: %w", err)
	}
	// 产物仅需服务账号可读写，禁止可执行位（存储目录禁止脚本执行）。
	_ = os.Chmod(finalPath, 0o640)

	source := domain.ReportSourceInternal
	if in.TaskID == 0 {
		source = domain.ReportSourceExternal
	}
	rep := &domain.Report{
		TaskID:    in.TaskID,
		ProductID: in.ProductID,
		Source:    source,
		Type:      format,
		Title:     importTitle(displayName, in.TaskID),
		Status:    domain.ReportStatusReady,
		Summary: &domain.ReportSummary{
			// imported：导入报告无阈值评估结论，前端展示「外部导入」标签。
			State:            "imported",
			OriginalFilename: displayName,
			SHA256:           hex.EncodeToString(digest),
			SizeBytes:        written,
		},
	}
	artifacts := []domain.ReportArtifact{{
		Format: format,
		Path:   finalPath,
		Size:   written,
	}}
	if err := s.repo.WithTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		if err := s.repo.CreateReport(ctx, tx, rep); err != nil {
			return err
		}
		return s.repo.ReplaceArtifacts(ctx, tx, rep.ID, artifacts)
	}); err != nil {
		_ = os.Remove(finalPath)
		return nil, err
	}
	return s.Get(ctx, rep.ID)
}

// copyWithLimit 流式复制并计算 SHA256；超过 limit 立即返回 24003。
func copyWithLimit(dst io.Writer, src io.Reader, limit int64) (int64, []byte, error) {
	hasher := sha256.New()
	written, err := io.Copy(io.MultiWriter(dst, hasher), io.LimitReader(src, limit+1))
	if err != nil {
		return written, nil, fmt.Errorf("读取上传文件失败: %w", err)
	}
	if written > limit {
		return written, nil, ErrReportTooLarge
	}
	if written == 0 {
		return 0, nil, ErrReportTypeNotAllowed
	}
	return written, hasher.Sum(nil), nil
}

// validateReportFile 扩展名 + magic bytes 双校验（24002）。
func validateReportFile(path, ext string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("读取上传文件失败: %w", err)
	}
	defer func() { _ = f.Close() }()

	head := make([]byte, headBytes)
	n, readErr := io.ReadFull(f, head)
	if readErr != nil && readErr != io.ErrUnexpectedEOF {
		return fmt.Errorf("读取上传文件头失败: %w", readErr)
	}
	head = head[:n]
	mime := http.DetectContentType(head)

	switch ext {
	case ".pdf":
		// PDF 文件头必须为 %PDF-，且 mime 推导为 application/pdf。
		if !bytes.HasPrefix(head, []byte("%PDF-")) || !strings.HasPrefix(mime, "application/pdf") {
			return ErrReportTypeNotAllowed
		}
	case ".docx":
		// docx 为 zip 容器：除 zip magic 外还必须是 OOXML 文档（word/document.xml）。
		if !bytes.HasPrefix(head, []byte("PK\x03\x04")) || !isDocx(path) {
			return ErrReportTypeNotAllowed
		}
	case ".html", ".htm":
		// HTML 无固定 magic：拒绝二进制内容（含 NUL），mime 必须为文本。
		if bytes.IndexByte(head, 0) >= 0 {
			return ErrReportTypeNotAllowed
		}
		if !strings.HasPrefix(mime, "text/html") && !strings.HasPrefix(mime, "text/plain") {
			return ErrReportTypeNotAllowed
		}
	default:
		return ErrReportTypeNotAllowed
	}
	return nil
}

// isDocx 校验 zip 容器内存在 OOXML 主文档（避免把任意 zip/伪装文件当 docx）。
func isDocx(path string) bool {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return false
	}
	defer func() { _ = zr.Close() }()
	for _, file := range zr.File {
		if file.Name == "word/document.xml" {
			return true
		}
	}
	return false
}

// randomFilename 生成随机文件名（16 字节密码学随机 + 白名单扩展名）。
func randomFilename(ext string) (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成随机文件名失败: %w", err)
	}
	return hex.EncodeToString(buf) + ext, nil
}

// sanitizeFilename 生成展示用文件名（仅取基名、去控制字符、限长，不参与落盘路径）。
func sanitizeFilename(name string) string {
	base := filepath.Base(filepath.FromSlash(strings.TrimSpace(name)))
	base = filepath.Base(base)
	var b strings.Builder
	for _, r := range base {
		if r == '/' || r == '\\' || unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if out == "" || out == "." || out == ".." {
		out = "report"
	}
	if runes := []rune(out); len(runes) > 200 {
		out = string(runes[:200])
	}
	return out
}

// importTitle 导入报告标题（外部导入需明确标注，PRD §3.4 / 决策 #4）。
func importTitle(filename string, taskID int64) string {
	prefix := "外部导入"
	if taskID > 0 {
		prefix = "导入报告"
	}
	title := fmt.Sprintf("%s · %s", prefix, filename)
	if runes := []rune(title); len(runes) > maxTitleLen {
		return string(runes[:maxTitleLen])
	}
	return title
}
