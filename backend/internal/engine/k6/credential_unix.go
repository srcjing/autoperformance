//go:build !windows

package k6

import (
	"os"
	"os/exec"
	"syscall"
)

// applyCredential 以专用低权限账号运行 k6 子进程（DES-E-002：对外进程不 root 运行）。
//
// uid/gid <= 0 时保持当前进程身份（开发环境/无法降权场景）；
// 生产由 install.sh 创建无登录 shell 的低权限账号，配置 cleaner.low_priv_uid/gid。
func applyCredential(cmd *exec.Cmd, uid, gid int) {
	if uid <= 0 || gid <= 0 {
		return
	}
	// 非 root 进程无法 setuid（systemd User=autoperf 且 NoNewPrivileges=true 时
	// 强行降权会导致 fork/exec 失败：operation not permitted）。
	// 此时服务进程本身已是低权限账号，k6 继承当前身份即可，无需再降权。
	if os.Geteuid() != 0 {
		return
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)},
	}
}
