// Package domain 领域模型（实体 + 枚举/状态机）。
//
// 本文件定义接口地址拆分后的地址口径：协议（scheme）+ 端口（port）+ 路径（path），
// 接口不再配置 host；执行期由任务所选环境的 target_host 补齐（PRD §4.5/§4.8）。
package domain

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// 接口协议枚举（PRD §4.5：http / https）。
const (
	SchemeHTTP  = "http"
	SchemeHTTPS = "https"
)

// MaxInterfacePathLen 接口路径长度上限（与产品管理侧地址长度上限一致）。
const MaxInterfacePathLen = 2048

// ErrInvalidInterfaceAddress 接口地址（协议/端口/路径/host）非法。
//
// 服务层映射为参数格式错误（10003），引擎层在生成脚本前做二次校验（DES-T-001）。
var ErrInvalidInterfaceAddress = errors.New("接口地址非法")

// IsValidScheme 判断协议是否合法（大小写与首尾空白不敏感）。
func IsValidScheme(scheme string) bool {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case SchemeHTTP, SchemeHTTPS:
		return true
	default:
		return false
	}
}

// IsValidPort 判断端口是否在 1~65535（0 视为未填写，由调用方区分错误码）。
func IsValidPort(port int) bool { return port >= 1 && port <= 65535 }

// IsValidInterfacePath 判断路径是否合法：以 / 开头，不含空白/控制字符，长度受限。
//
// 入参不做去空白处理（首尾空白本身就是非法字符，由调用方自行 trim 后传入）。
// 允许查询串（如 /api/list?page=1），但禁止换行等可跳出脚本字符串上下文的字符。
func IsValidInterfacePath(path string) bool {
	if path == "" || !strings.HasPrefix(path, "/") || len(path) > MaxInterfacePathLen {
		return false
	}
	for _, r := range path {
		if r <= 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// InterfaceURL 拼接完整压测地址：{scheme}://{host}:{port}{path}。
//
// host 来自任务所选环境的 hosts（target_host），接口自身不保存 host。
// IPv6 字面量自动补方括号（如 ::1 → [::1]），保证 net/url 与 k6 可直接消费。
func InterfaceURL(scheme, host string, port int, path string) (string, error) {
	s := strings.ToLower(strings.TrimSpace(scheme))
	if !IsValidScheme(s) {
		return "", fmt.Errorf("%w: 协议仅支持 http/https", ErrInvalidInterfaceAddress)
	}
	h, err := normalizeInterfaceHost(host)
	if err != nil {
		return "", err
	}
	if !IsValidPort(port) {
		return "", fmt.Errorf("%w: 端口需在 1~65535", ErrInvalidInterfaceAddress)
	}
	p := strings.TrimSpace(path)
	if !IsValidInterfacePath(p) {
		return "", fmt.Errorf("%w: 路径需以 / 开头且不含空白/控制字符", ErrInvalidInterfaceAddress)
	}
	return fmt.Sprintf("%s://%s:%d%s", s, h, port, p), nil
}

// normalizeInterfaceHost 校验并归一化主机名（IP/域名，禁止携带端口、路径与空白）。
func normalizeInterfaceHost(host string) (string, error) {
	h := strings.TrimSpace(host)
	if h == "" {
		return "", fmt.Errorf("%w: 压测目标主机为空", ErrInvalidInterfaceAddress)
	}
	for _, r := range h {
		if r <= 0x20 || r == 0x7f {
			return "", fmt.Errorf("%w: 主机含空白/控制字符", ErrInvalidInterfaceAddress)
		}
	}
	if strings.ContainsAny(h, "/@?#") {
		return "", fmt.Errorf("%w: 主机只能是 IP 或域名", ErrInvalidInterfaceAddress)
	}
	// IPv6 字面量：裸写时补方括号；已带方括号则原样使用；其余含冒号视为携带端口，拒绝。
	if strings.Contains(h, ":") {
		if strings.HasPrefix(h, "[") && strings.HasSuffix(h, "]") {
			return h, nil
		}
		if ip := net.ParseIP(h); ip != nil && ip.To4() == nil {
			return "[" + h + "]", nil
		}
		return "", fmt.Errorf("%w: 主机不能携带端口", ErrInvalidInterfaceAddress)
	}
	return h, nil
}

// IsValidHostAddress 主机地址白名单（IP/域名，不含端口/路径/空白）。
//
// 用于校验环境 hosts / 任务 target_host，保证其可直接拼入 URL。
func IsValidHostAddress(host string) bool {
	_, err := normalizeInterfaceHost(host)
	return err == nil
}

// HostInList 判断主机是否属于候选列表（大小写与首尾空白不敏感）。
//
// 用于校验任务 target_host 必须来自所选环境 hosts（需求 1）。
func HostInList(host string, hosts []string) bool {
	target := strings.TrimSpace(host)
	if target == "" {
		return false
	}
	for _, item := range hosts {
		if strings.EqualFold(strings.TrimSpace(item), target) {
			return true
		}
	}
	return false
}
