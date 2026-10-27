// Package pdf 报告 PDF 生成（headless Chromium 打印，对齐 DES-D-001）。
//
// 安全要点：
//   - 子进程以参数数组启动（exec.Command，无 shell），HTML 路径/输出路径均来自服务端内部
//     生成目录，不拼接用户输入；
//   - 解析超时由 context 控制（配置项 report.gen_timeout_seconds），超时立即杀死子进程
//     并返回 ErrTimeout，由服务层映射为 24004，防止解析器 DoS；
//   - 产出后校验文件存在、非空且带 PDF magic（%PDF-），避免把错误页当报告。
package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultTimeout Chromium 打印默认超时（配置缺省值与 config.report.gen_timeout_seconds 一致）。
const DefaultTimeout = 120 * time.Second

// stderrLimit 诊断信息保留上限（避免子进程大段输出占用内存）。
const stderrLimit = 4096

// 生成错误（服务层统一映射为 24004）。
var (
	// ErrTimeout Chromium 解析超时。
	ErrTimeout = errors.New("chromium 生成 PDF 超时")
	// ErrUnavailable Chromium 不可用（未安装/路径错误）。
	ErrUnavailable = errors.New("chromium 不可用")
	// ErrOutputMissing 未产出有效 PDF 文件。
	ErrOutputMissing = errors.New("chromium 未产出有效 PDF 文件")
)

// Config PDF 生成配置。
type Config struct {
	// Path Chromium 可执行文件路径（配置项 report.chromium_path）。
	Path string
	// Timeout 单次打印超时（<=0 取 DefaultTimeout）。
	Timeout time.Duration
}

// Generator headless Chromium PDF 生成器。
type Generator struct {
	path    string
	timeout time.Duration
}

// New 构造 PDF 生成器（path 为空时使用 "chromium"）。
func New(cfg Config) *Generator {
	path := strings.TrimSpace(cfg.Path)
	if path == "" {
		path = "chromium"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Generator{path: path, timeout: timeout}
}

// Timeout 返回生效的超时时间。
func (g *Generator) Timeout() time.Duration {
	if g == nil {
		return DefaultTimeout
	}
	return g.timeout
}

// BuildArgs 构造 Chromium 命令行参数（逐参传递，不经 shell）。
//
// 使用 --headless + --print-to-pdf=<out> 打印本地 HTML（file:// URL）。
func BuildArgs(outPath, htmlPath string) []string {
	return []string{
		"--headless",
		"--disable-gpu",
		"--hide-scrollbars",
		"--no-pdf-header-footer",
		"--print-to-pdf=" + outPath,
		fileURL(htmlPath),
	}
}

// fileURL 本地绝对路径 → file:// URL（Windows 盘符亦可在 Chromium 中打开）。
func fileURL(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	slash := filepath.ToSlash(abs)
	if !strings.HasPrefix(slash, "/") {
		slash = "/" + slash
	}
	u := url.URL{Scheme: "file", Path: slash}
	return u.String()
}

// Generate 把 HTML 打印为 PDF（outPath 由服务端报告目录决定）。
func (g *Generator) Generate(ctx context.Context, htmlPath, outPath string) error {
	if g == nil {
		return fmt.Errorf("%w: 生成器未初始化", ErrUnavailable)
	}
	htmlPath = strings.TrimSpace(htmlPath)
	outPath = strings.TrimSpace(outPath)
	if htmlPath == "" || outPath == "" {
		return fmt.Errorf("PDF 生成参数为空")
	}

	absHTML, err := filepath.Abs(htmlPath)
	if err != nil {
		return fmt.Errorf("解析 HTML 报告路径失败: %w", err)
	}
	absOut, err := filepath.Abs(outPath)
	if err != nil {
		return fmt.Errorf("解析 PDF 输出路径失败: %w", err)
	}
	if _, err := os.Stat(absHTML); err != nil {
		return fmt.Errorf("HTML 报告不存在: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absOut), 0o750); err != nil {
		return fmt.Errorf("创建 PDF 输出目录失败: %w", err)
	}
	// 旧产物先删除：避免打印失败时把上一次的 PDF 当成本次结果返回。
	_ = os.Remove(absOut)

	runCtx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, g.path, BuildArgs(absOut, absHTML)...)
	var stderr limitedBuffer
	cmd.Stdout = io.Discard
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
		return ErrTimeout
	}
	if runErr != nil {
		if errors.Is(runErr, exec.ErrNotFound) || errors.Is(runErr, os.ErrNotExist) {
			return fmt.Errorf("%w: %v", ErrUnavailable, runErr)
		}
		return fmt.Errorf("chromium 执行失败: %w（stderr: %s）", runErr, stderr.String())
	}

	fi, err := os.Stat(absOut)
	if err != nil || fi.Size() == 0 {
		return fmt.Errorf("%w（输出: %s）", ErrOutputMissing, absOut)
	}
	head := make([]byte, 5)
	f, err := os.Open(absOut)
	if err != nil {
		return fmt.Errorf("读取 PDF 产物失败: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := io.ReadFull(f, head); err != nil || !bytes.HasPrefix(head, []byte("%PDF-")) {
		return fmt.Errorf("%w（magic 校验失败）", ErrOutputMissing)
	}
	return nil
}

// limitedBuffer 仅保留前 stderrLimit 字节的写入缓冲。
type limitedBuffer struct {
	buf bytes.Buffer
}

// Write 实现 io.Writer（超出上限部分丢弃）。
func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := stderrLimit - b.buf.Len(); room > 0 {
		if len(p) > room {
			b.buf.Write(p[:room])
		} else {
			b.buf.Write(p)
		}
	}
	return len(p), nil
}

// String 返回已保留的诊断信息（单行，便于日志）。
func (b *limitedBuffer) String() string {
	return strings.TrimSpace(strings.ReplaceAll(b.buf.String(), "\n", " "))
}
