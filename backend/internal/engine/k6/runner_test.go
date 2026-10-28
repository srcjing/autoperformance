package k6

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunK6NoShell 断言 k6 通过参数数组直接执行，绝不经过 shell（DES-T-002 / INV-9）。
func TestRunK6NoShell(t *testing.T) {
	workDir := t.TempDir()
	r := NewRunner(Config{BinaryPath: "/usr/local/bin/k6", WorkDir: workDir})
	scriptPath := filepath.Join(workDir, "task-1.js")
	resultPath := filepath.Join(workDir, "task-1.json")

	cmd := r.Command(context.Background(), scriptPath, resultPath)

	want := []string{"/usr/local/bin/k6", "run", "--log-format", "raw", scriptPath, "--out", "json=" + resultPath}
	if len(cmd.Args) != len(want) {
		t.Fatalf("Args = %v, want %v", cmd.Args, want)
	}
	for i := range want {
		if cmd.Args[i] != want[i] {
			t.Fatalf("Args[%d] = %q, want %q", i, cmd.Args[i], want[i])
		}
	}

	for _, arg := range cmd.Args {
		switch strings.ToLower(filepath.Base(arg)) {
		case "sh", "bash", "cmd", "cmd.exe", "powershell", "pwsh", "zsh":
			t.Fatalf("子进程参数不得包含 shell 解释器: %v", cmd.Args)
		}
		if arg == "-c" || arg == "/c" {
			t.Fatalf("子进程参数不得包含 shell 执行标志: %v", cmd.Args)
		}
	}
	if cmd.Dir != workDir {
		t.Fatalf("Cmd.Dir = %q, want %q", cmd.Dir, workDir)
	}
}

// TestRunK6PathsStaySingleArgument 结果路径中的注入字符只作为单个参数传递，不触发命令执行。
func TestRunK6PathsStaySingleArgument(t *testing.T) {
	r := NewRunner(Config{BinaryPath: "k6", WorkDir: t.TempDir()})
	evil := filepath.Join(r.WorkDir(), `task-1.json; rm -rf /`)
	cmd := r.Command(context.Background(), "task-1.js", evil)

	found := false
	for i, arg := range cmd.Args {
		if arg == "json="+evil {
			found = true
			if i == 0 {
				t.Fatal("结果路径不得成为可执行文件")
			}
		}
	}
	if !found {
		t.Fatalf("结果路径应作为独立参数传递: %v", cmd.Args)
	}
	if len(cmd.Args) != 7 {
		t.Fatalf("参数数量异常（疑似被拆分/注入）: %v", cmd.Args)
	}
}

// TestRunnerEnvironmentWhitelist 子进程环境不继承完整父环境（避免密钥泄漏）。
func TestRunnerEnvironmentWhitelist(t *testing.T) {
	t.Setenv("AUTO_PERF_SECRET_KEY", "should-not-leak")
	r := NewRunner(Config{BinaryPath: "k6", WorkDir: t.TempDir(), UID: 65534, GID: 65534})
	cmd := r.Command(context.Background(), "task-1.js", "task-1.json")
	for _, env := range cmd.Env {
		if strings.Contains(env, "AUTO_PERF_SECRET_KEY") {
			t.Fatalf("子进程环境泄漏密钥: %s", env)
		}
		if strings.HasPrefix(env, "K6_NO_USAGE_REPORT=") {
			return
		}
	}
	t.Fatal("缺少 K6_NO_USAGE_REPORT 环境变量")
}
