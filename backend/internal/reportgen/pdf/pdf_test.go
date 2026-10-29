package pdf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// buildFakeChromium 编译一个「假 chromium」可执行程序，用于验证参数/超时行为（无需真实浏览器）。
//
// 程序行为：解析 --print-to-pdf=<out>，sleep(delay) 后写出带 %PDF- magic 的文件。
func buildFakeChromium(t *testing.T, delay time.Duration) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	code := fmt.Sprintf(`package main

import (
	"os"
	"strings"
	"time"
)

func main() {
	out := ""
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "--print-to-pdf=") {
			out = strings.TrimPrefix(a, "--print-to-pdf=")
		}
	}
	if out == "" {
		os.Exit(3)
	}
	time.Sleep(%d * time.Millisecond)
	if err := os.WriteFile(out, []byte("%%PDF-1.4\n%% fake chromium\n"), 0o600); err != nil {
		os.Exit(4)
	}
}
`, delay.Milliseconds())
	if err := os.WriteFile(src, []byte(code), 0o600); err != nil {
		t.Fatalf("写入假 chromium 源码失败: %v", err)
	}

	bin := filepath.Join(dir, "fake-chromium")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, src)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("编译假 chromium 失败: %v\n%s", err, out)
	}
	return bin
}

// writeHTML 在临时目录写入待打印的 HTML 文件。
func writeHTML(t *testing.T) (dir, htmlPath string) {
	t.Helper()
	dir = t.TempDir()
	htmlPath = filepath.Join(dir, "report.html")
	if err := os.WriteFile(htmlPath, []byte("<!DOCTYPE html><html><body>report</body></html>"), 0o600); err != nil {
		t.Fatalf("写入 HTML 失败: %v", err)
	}
	return dir, htmlPath
}

// TestChromiumTimeout 超时必须及时中止子进程并返回 ErrTimeout（→ 24004 / DES-D-001）。
func TestChromiumTimeout(t *testing.T) {
	bin := buildFakeChromium(t, 5*time.Second)
	dir, htmlPath := writeHTML(t)

	g := New(Config{Path: bin, Timeout: 300 * time.Millisecond})
	started := time.Now()
	err := g.Generate(context.Background(), htmlPath, filepath.Join(dir, "report.pdf"))
	elapsed := time.Since(started)

	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Generate 超时应返回 ErrTimeout, got %v", err)
	}
	if elapsed > 4*time.Second {
		t.Fatalf("超时未及时返回，耗时 %v", elapsed)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "report.pdf")); statErr == nil {
		t.Fatalf("超时后不应残留 PDF 产物")
	}
}

// TestChromiumGeneratesPDF 正常路径：参数正确时产出带 %PDF- magic 的文件。
func TestChromiumGeneratesPDF(t *testing.T) {
	bin := buildFakeChromium(t, 0)
	dir, htmlPath := writeHTML(t)
	outPath := filepath.Join(dir, "artifacts", "report.pdf")

	g := New(Config{Path: bin, Timeout: 10 * time.Second})
	if err := g.Generate(context.Background(), htmlPath, outPath); err != nil {
		t.Fatalf("Generate 失败: %v", err)
	}
	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("读取 PDF 失败: %v", err)
	}
	if !strings.HasPrefix(string(raw), "%PDF-") {
		t.Fatalf("PDF 内容非法: %q", string(raw))
	}
}

// TestChromiumUnavailable 未安装 Chromium 时返回 ErrUnavailable（服务层映射 24004）。
func TestChromiumUnavailable(t *testing.T) {
	dir, htmlPath := writeHTML(t)
	g := New(Config{Path: filepath.Join(dir, "missing-chromium"), Timeout: time.Second})
	err := g.Generate(context.Background(), htmlPath, filepath.Join(dir, "report.pdf"))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("缺少 chromium 应返回 ErrUnavailable, got %v", err)
	}
}

// TestBuildArgsUsesHeadlessPrintToPDF 校验参数为逐参数组且包含 headless/print-to-pdf/file URL。
func TestBuildArgsUsesHeadlessPrintToPDF(t *testing.T) {
	out := filepath.Join("tmp", "report.pdf")
	htmlPath := filepath.Join("tmp", "report.html")
	args := BuildArgs(out, htmlPath)

	joined := strings.Join(args, " ")
	for _, want := range []string{"--headless", "--print-to-pdf=" + out, "file:///"} {
		if !strings.Contains(joined, want) {
			t.Errorf("参数缺少 %q: %v", want, args)
		}
	}
	for _, a := range args {
		if strings.ContainsAny(a, "|&;`$<>") {
			t.Errorf("参数包含 shell 元字符（应逐参传递、无 shell）: %q", a)
		}
	}
}
