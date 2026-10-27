package cleaner

import (
	"context"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// 远程命令/参数白名单（DES-T-003：拒绝任意用户脚本直连与 shell 拼接）。
var (
	remoteCommandRE = regexp.MustCompile(`^(/[A-Za-z0-9._-]+)+$`)
	remoteArgRE     = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)
)

// HostCredential SSH 主机凭据（私钥由系统设置模块 AES-256-GCM 解密后注入）。
type HostCredential struct {
	Addr       string
	Port       int
	User       string
	PrivateKey []byte
	// HostKey 固定的主机公钥（严格校验，禁止 InsecureIgnoreHostKey）。
	HostKey ssh.PublicKey
}

// HostResolver 解析 SSH 主机凭据（Phase 7 系统设置实现；测试可注入假实现）。
type HostResolver interface {
	ResolveHost(ctx context.Context, hostID int64) (*HostCredential, error)
}

// SSHHostRunner 基于 x/crypto/ssh 的远程执行器。
type SSHHostRunner struct {
	resolve HostResolver
	timeout time.Duration
}

// NewSSHHostRunner 构造 SSH 执行器。
func NewSSHHostRunner(resolve HostResolver) *SSHHostRunner {
	return &SSHHostRunner{resolve: resolve, timeout: 30 * time.Second}
}

// Run 在指定主机执行「固定命令 + 白名单参数」。
//
// 安全约束：命令必须是绝对路径且不含 shell 元字符；参数必须是 [a-z0-9_-] 枚举；
// 主机公钥固定校验；连接超时受限。
func (r *SSHHostRunner) Run(ctx context.Context, hostID int64, command string, args []string) (string, error) {
	if r == nil || r.resolve == nil {
		return "", ErrNoRunner
	}
	if !remoteCommandRE.MatchString(command) {
		return "", fmt.Errorf("%w: 命令非法", ErrInvalidPlan)
	}
	for _, arg := range args {
		if !remoteArgRE.MatchString(arg) {
			return "", fmt.Errorf("%w: 命令参数非法", ErrInvalidPlan)
		}
	}

	cred, err := r.resolve.ResolveHost(ctx, hostID)
	if err != nil {
		return "", fmt.Errorf("解析 SSH 主机失败: %w", err)
	}
	if cred == nil || cred.Addr == "" || cred.User == "" || len(cred.PrivateKey) == 0 || cred.HostKey == nil {
		return "", fmt.Errorf("%w: SSH 主机凭据不完整", ErrInvalidPlan)
	}

	signer, err := ssh.ParsePrivateKey(cred.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("解析 SSH 私钥失败: %w", err)
	}
	port := cred.Port
	if port <= 0 {
		port = 22
	}
	timeout := r.timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	cfg := &ssh.ClientConfig{
		User:            cred.User,
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.FixedHostKey(cred.HostKey), // 严格主机公钥校验
		Timeout:         timeout,
	}

	addr := net.JoinHostPort(cred.Addr, strconv.Itoa(port))
	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", fmt.Errorf("连接 SSH 主机失败: %w", err)
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return "", fmt.Errorf("SSH 握手失败: %w", err)
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer func() { _ = client.Close() }()

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			_ = client.Close()
		case <-done:
		}
	}()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("创建 SSH 会话失败: %w", err)
	}
	defer func() { _ = session.Close() }()

	out, err := session.Output(strings.TrimSpace(command + " " + strings.Join(args, " ")))
	if err != nil {
		return string(out), fmt.Errorf("远程命令执行失败: %w", err)
	}
	return string(out), nil
}
