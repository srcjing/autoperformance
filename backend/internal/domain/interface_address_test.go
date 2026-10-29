package domain

import (
	"errors"
	"testing"
)

// TestInterfaceURLComposesSchemeHostPortPath 完整压测地址 = {scheme}://{host}:{port}{path}（需求 1）。
func TestInterfaceURLComposesSchemeHostPortPath(t *testing.T) {
	cases := []struct {
		name   string
		scheme string
		host   string
		port   int
		path   string
		want   string
	}{
		{"https 自定义端口", "https", "10.52.78.246", 8443, "/manage/api/v2/man/admin/login",
			"https://10.52.78.246:8443/manage/api/v2/man/admin/login"},
		{"http 域名", "http", "api.example.com", 80, "/api/order/create",
			"http://api.example.com:80/api/order/create"},
		{"协议大写归一化", "HTTPS", "env.internal", 443, "/", "https://env.internal:443/"},
		{"带查询串的路径", "http", "10.0.0.1", 8080, "/api/list?page=1",
			"http://10.0.0.1:8080/api/list?page=1"},
		{"IPv6 字面量自动加方括号", "http", "::1", 8080, "/health",
			"http://[::1]:8080/health"},
		{"IPv6 已带方括号", "https", "[2001:db8::1]", 443, "/health",
			"https://[2001:db8::1]:443/health"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := InterfaceURL(c.scheme, c.host, c.port, c.path)
			if err != nil {
				t.Fatalf("拼接失败: %v", err)
			}
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

// TestInterfaceURLRejectsInvalidParts 协议/端口/路径/host 白名单校验。
func TestInterfaceURLRejectsInvalidParts(t *testing.T) {
	cases := []struct {
		name   string
		scheme string
		host   string
		port   int
		path   string
	}{
		{"协议缺失", "", "10.0.0.1", 80, "/x"},
		{"协议非 http(s)", "ftp", "10.0.0.1", 80, "/x"},
		{"协议为 javascript", "javascript", "10.0.0.1", 80, "/x"},
		{"端口为 0", "http", "10.0.0.1", 0, "/x"},
		{"端口为负", "http", "10.0.0.1", -1, "/x"},
		{"端口越界", "http", "10.0.0.1", 65536, "/x"},
		{"路径为空", "http", "10.0.0.1", 80, ""},
		{"路径不以 / 开头", "http", "10.0.0.1", 80, "api/x"},
		{"路径含换行", "http", "10.0.0.1", 80, "/api\nx"},
		{"路径含空格", "http", "10.0.0.1", 80, "/api x"},
		{"host 为空", "http", "  ", 80, "/x"},
		{"host 含斜杠", "http", "10.0.0.1/evil", 80, "/x"},
		{"host 含空白", "http", "10.0.0.1 evil", 80, "/x"},
		{"host 含查询串", "http", "10.0.0.1?x=1", 80, "/x"},
		{"host 携带端口", "http", "10.0.0.1:8080", 80, "/x"},
		{"host 以冒号结尾", "http", "10.0.0.1:", 80, "/x"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := InterfaceURL(c.scheme, c.host, c.port, c.path); !errors.Is(err, ErrInvalidInterfaceAddress) {
				t.Fatalf("want ErrInvalidInterfaceAddress, got %v", err)
			}
		})
	}
}

// TestAddressPartValidators 单项校验器（语义与 service/k6 共用）。
func TestAddressPartValidators(t *testing.T) {
	for _, scheme := range []string{"http", "https", "HTTPS", " Http "} {
		if !IsValidScheme(scheme) {
			t.Errorf("scheme %q 应合法", scheme)
		}
	}
	for _, scheme := range []string{"", "httpss", "ftp", "ws", "file"} {
		if IsValidScheme(scheme) {
			t.Errorf("scheme %q 应非法", scheme)
		}
	}
	for _, port := range []int{1, 80, 443, 8443, 65535} {
		if !IsValidPort(port) {
			t.Errorf("port %d 应合法", port)
		}
	}
	for _, port := range []int{0, -1, 65536, 100000} {
		if IsValidPort(port) {
			t.Errorf("port %d 应非法", port)
		}
	}
	for _, path := range []string{"/", "/api/order/create", "/a?b=1"} {
		if !IsValidInterfacePath(path) {
			t.Errorf("path %q 应合法", path)
		}
	}
	for _, path := range []string{"", "api", " /api", "/api\n", "/api b"} {
		if IsValidInterfacePath(path) {
			t.Errorf("path %q 应非法", path)
		}
	}
}

// TestIsValidHostAddress 主机地址白名单（用于环境 hosts / 任务 target_host）。
func TestIsValidHostAddress(t *testing.T) {
	for _, host := range []string{"10.52.78.246", "env.internal", "Env.Internal.COM", "::1", "[2001:db8::1]"} {
		if !IsValidHostAddress(host) {
			t.Errorf("host %q 应合法", host)
		}
	}
	for _, host := range []string{"", "  ", "10.0.0.1:8080", "10.0.0.1:", "host/path", "host name", "host?x"} {
		if IsValidHostAddress(host) {
			t.Errorf("host %q 应非法", host)
		}
	}
}

// TestHostInList target_host 必须属于环境 hosts（大小写/首尾空白不敏感）。
func TestHostInList(t *testing.T) {
	hosts := []string{"10.52.78.246", " Env-Internal.Example.COM "}
	if !HostInList("10.52.78.246", hosts) {
		t.Fatal("精确匹配应命中")
	}
	if !HostInList(" env-internal.example.com", hosts) {
		t.Fatal("域名应大小写不敏感匹配")
	}
	if HostInList("10.52.78.247", hosts) {
		t.Fatal("不在列表内的 host 不应命中")
	}
	if HostInList("", hosts) {
		t.Fatal("空 host 不应命中")
	}
	if HostInList("10.52.78.246", nil) {
		t.Fatal("空 hosts 不应命中")
	}
}
