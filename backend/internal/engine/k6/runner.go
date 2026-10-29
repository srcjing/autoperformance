package k6

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// 默认 k6 二进制名与工作目录。
const (
	defaultBinary = "k6"
	// maxLogLine 单行输出上限（防止异常输出撑爆内存）。
	maxLogLine = 64 * 1024
)

// Config k6 运行配置。
type Config struct {
	// BinaryPath k6 二进制路径（配置项 k6.binary_path）。
	BinaryPath string
	// WorkDir 脚本/结果落盘沙箱目录（配置项 k6.work_dir）。
	WorkDir string
	// UID/GID 子进程低权限账号（DES-E-002；0 表示不降权，Windows 忽略）。
	UID int
	GID int
	// Env 追加环境变量（不继承父进程全部环境，避免密钥泄漏给子进程）。
	Env []string
}

// Runner k6 子进程执行器。
//
// 安全约束（DES-T-002 / INV-9）：exec.CommandContext 逐参传递，不经过任何 shell 解释器；
// 脚本/结果路径由任务 ID 派生并限定在工作目录内。
type Runner struct {
	cfg Config
}

// NewRunner 构造执行器（缺省补齐二进制名与工作目录）。
func NewRunner(cfg Config) *Runner {
	if strings.TrimSpace(cfg.BinaryPath) == "" {
		cfg.BinaryPath = defaultBinary
	}
	if strings.TrimSpace(cfg.WorkDir) == "" {
		cfg.WorkDir = os.TempDir()
	}
	return &Runner{cfg: cfg}
}

// WorkDir 返回工作目录。
func (r *Runner) WorkDir() string { return r.cfg.WorkDir }

// ScriptPath 任务脚本路径（仅由任务 ID 派生，不含用户输入）。
func (r *Runner) ScriptPath(taskID int64) string {
	return filepath.Join(r.cfg.WorkDir, ScriptFileName(taskID))
}

// ResultPath 任务结果路径（仅由任务 ID 派生，不含用户输入）。
func (r *Runner) ResultPath(taskID int64) string {
	return filepath.Join(r.cfg.WorkDir, ResultFileName(taskID))
}

// Command 构造 k6 命令（参数数组、无 shell）。
//
// 单独导出便于安全测试断言 Args：不得出现 "sh"、"-c"、"bash"、"cmd /c" 等解释器。
func (r *Runner) Command(ctx context.Context, scriptPath, resultPath string) *exec.Cmd {
	args := []string{
		"run",
		// raw：console.log 输出原始文本（供请求详情 AFREQ 行解析；否则被 logrus 包装成 time=... msg="..."）。
		"--log-format", "raw",
		scriptPath,
		"--out", "json=" + resultPath,
	}
	cmd := exec.CommandContext(ctx, r.cfg.BinaryPath, args...)
	cmd.Dir = r.cfg.WorkDir
	cmd.Env = r.environment()
	// 低权限运行：仅 Unix 生效（Windows 无 Credential 概念）。
	applyCredential(cmd, r.cfg.UID, r.cfg.GID)
	return cmd
}

// environment 构造子进程环境（白名单，不继承完整父环境）。
func (r *Runner) environment() []string {
	env := []string{
		"K6_NO_USAGE_REPORT=true",
		"K6_NO_COLOR=true",
		"HOME=" + r.cfg.WorkDir,
		"TMPDIR=" + r.cfg.WorkDir,
	}
	if path := os.Getenv("PATH"); path != "" {
		env = append(env, "PATH="+path)
	}
	env = append(env, r.cfg.Env...)
	return env
}

// Run 执行 k6 并把逐行输出回调给调用方（调用方负责脱敏后推送/落库）。
func (r *Runner) Run(ctx context.Context, scriptPath, resultPath string, onLine func(line string)) error {
	if err := os.MkdirAll(r.cfg.WorkDir, 0o750); err != nil {
		return fmt.Errorf("创建 k6 工作目录失败: %w", err)
	}
	// 结果文件由本次执行独占写入，先清理旧文件避免解析到历史数据。
	if err := os.Remove(resultPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("清理旧 k6 结果失败: %w", err)
	}

	cmd := r.Command(ctx, scriptPath, resultPath)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("获取 k6 stdout 失败: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("获取 k6 stderr 失败: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 k6 失败: %w", err)
	}

	var (
		wg      sync.WaitGroup
		errTail strings.Builder
		mu      sync.Mutex
	)
	scan := func(rd io.Reader, keepTail bool) {
		defer wg.Done()
		scanner := bufio.NewScanner(rd)
		scanner.Buffer(make([]byte, 0, 64*1024), maxLogLine)
		for scanner.Scan() {
			line := scanner.Text()
			if keepTail {
				mu.Lock()
				if errTail.Len() < 4096 {
					errTail.WriteString(line)
					errTail.WriteByte('\n')
				}
				mu.Unlock()
			}
			if onLine != nil {
				onLine(line)
			}
		}
	}
	wg.Add(2)
	go scan(stdout, false)
	go scan(stderr, true)

	waitErr := cmd.Wait()
	wg.Wait()

	if waitErr != nil {
		tail := strings.TrimSpace(errTail.String())
		if tail == "" {
			return fmt.Errorf("k6 执行失败: %w", waitErr)
		}
		return fmt.Errorf("k6 执行失败: %w: %s", waitErr, tail)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return nil
}
