//go:build !windows

package k6

import (
	"context"
	"testing"
)

// TestApplyCredentialSetsLowPrivilege k6 子进程以专用低权限账号运行（DES-E-002 / TEST-E-002）。
//
// 仅在 Unix 生效（Windows 无 uid/gid 概念）；生产由 install.sh 创建无登录 shell 的
// 低权限账号，配置 cleaner.low_priv_uid/gid。
func TestApplyCredentialSetsLowPrivilege(t *testing.T) {
	r := NewRunner(Config{BinaryPath: "k6", WorkDir: t.TempDir(), UID: 1001, GID: 1001})
	cmd := r.Command(context.Background(), "script.js", "result.json")

	if cmd.SysProcAttr == nil || cmd.SysProcAttr.Credential == nil {
		t.Fatal("配置低权限 uid/gid 后应设置 SysProcAttr.Credential（服务进程不得以 root 执行压测）")
	}
	if got := cmd.SysProcAttr.Credential.Uid; got != 1001 {
		t.Fatalf("Credential.Uid = %d, want 1001", got)
	}
	if got := cmd.SysProcAttr.Credential.Gid; got != 1001 {
		t.Fatalf("Credential.Gid = %d, want 1001", got)
	}
	// 参数数组、无 shell 的约束在降权后依然成立。
	assertNoShell(t, cmd.Args)
}

// TestApplyCredentialSkippedWhenUnset 未配置低权限账号时保持当前身份（开发环境）。
func TestApplyCredentialSkippedWhenUnset(t *testing.T) {
	r := NewRunner(Config{BinaryPath: "k6", WorkDir: t.TempDir()})
	cmd := r.Command(context.Background(), "script.js", "result.json")
	if cmd.SysProcAttr != nil && cmd.SysProcAttr.Credential != nil {
		t.Fatalf("未配置 uid/gid 时不应设置 Credential: %+v", cmd.SysProcAttr.Credential)
	}
}

// assertNoShell 断言命令参数中不含 shell 解释器（DES-T-002）。
func assertNoShell(t *testing.T, args []string) {
	t.Helper()
	for _, a := range args {
		switch a {
		case "sh", "-c", "bash", "cmd", "/bin/sh", "/bin/bash":
			t.Fatalf("k6 命令出现 shell 解释器参数: %v", args)
		}
	}
}
