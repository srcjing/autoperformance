//go:build windows

package k6

import "os/exec"

// applyCredential Windows 下无可用的 uid/gid 降权机制（部署目标为 Linux），
// 保持当前进程身份并在启动日志中提示（DES-E-002 由部署侧兜底）。
func applyCredential(_ *exec.Cmd, _, _ int) {}
