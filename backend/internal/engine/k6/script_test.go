package k6

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"auto_performance/internal/domain"
)

// sampleTargetHost 压测目标主机（来自任务 target_host，属于所选环境 hosts）。
const sampleTargetHost = "10.52.78.246"

// sampleInterface 合法接口快照（协议 + 端口 + 路径，不含 host；供脚本生成测试复用）。
func sampleInterface() domain.InterfaceSnapshot {
	return domain.InterfaceSnapshot{
		InterfaceID: 11,
		Name:        "查询订单",
		Method:      "GET",
		Scheme:      domain.SchemeHTTPS,
		Port:        8443,
		Path:        "/orders",
		Headers:     map[string]string{"X-Trace-Id": "abc"},
		Status:      domain.InterfaceStatusEnabled,
	}
}

// sampleSpec 构造完整脚本生成入参。
func sampleSpec(snap domain.InterfaceSnapshot) ScriptSpec {
	return ScriptSpec{
		Flow: domain.FlowConfig{
			ConcurrencyMode: domain.ConcurrencyModeFixed,
			TargetVUs:       json.RawMessage(`100`),
			Duration:        "60s",
			Timeout:         "5s",
			ThinkTime:       "0.5s",
		},
		Interfaces: []domain.InterfaceSnapshot{snap},
		MaxVUs:     1000,
		TargetHost: sampleTargetHost,
	}
}

// TestBuildK6ScriptComposesURLFromParts 完整 URL = {scheme}://{target_host}:{port}{path}（需求 1）。
func TestBuildK6ScriptComposesURLFromParts(t *testing.T) {
	snap := sampleInterface()
	snap.Scheme = "https"
	snap.Port = 8443
	snap.Path = "/manage/api/v2/man/admin/login"

	script, err := BuildScript(sampleSpec(snap))
	if err != nil {
		t.Fatalf("生成脚本失败: %v", err)
	}
	want := `url: "https://10.52.78.246:8443/manage/api/v2/man/admin/login"`
	if !strings.Contains(script, want) {
		t.Fatalf("脚本 URL 拼接错误，缺少 %q:\n%s", want, script)
	}
	if strings.Contains(script, "http://") {
		t.Fatalf("脚本出现错误协议: %s", script)
	}
}

// TestBuildK6ScriptRejectsInvalidAddressParts 协议/端口/路径/目标主机白名单（DES-T-001）。
func TestBuildK6ScriptRejectsInvalidAddressParts(t *testing.T) {
	cases := []struct {
		name       string
		mutate     func(*domain.InterfaceSnapshot)
		targetHost string
	}{
		{"协议非法", func(s *domain.InterfaceSnapshot) { s.Scheme = "ftp" }, sampleTargetHost},
		{"协议为空", func(s *domain.InterfaceSnapshot) { s.Scheme = "" }, sampleTargetHost},
		{"端口为 0", func(s *domain.InterfaceSnapshot) { s.Port = 0 }, sampleTargetHost},
		{"端口越界", func(s *domain.InterfaceSnapshot) { s.Port = 70000 }, sampleTargetHost},
		{"路径不以 / 开头", func(s *domain.InterfaceSnapshot) { s.Path = "orders" }, sampleTargetHost},
		{"路径含换行", func(s *domain.InterfaceSnapshot) { s.Path = "/api\nx" }, sampleTargetHost},
		{"路径含空格", func(s *domain.InterfaceSnapshot) { s.Path = "/api x" }, sampleTargetHost},
		{"路径为空", func(s *domain.InterfaceSnapshot) { s.Path = "" }, sampleTargetHost},
		{"目标主机为空", func(s *domain.InterfaceSnapshot) {}, ""},
		{"目标主机含空白", func(s *domain.InterfaceSnapshot) {}, "10.52.78.246 evil"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			snap := sampleInterface()
			c.mutate(&snap)
			spec := sampleSpec(snap)
			spec.TargetHost = c.targetHost
			if _, err := BuildScript(spec); err == nil {
				t.Fatal("非法接口地址应被拒绝")
			}
		})
	}

	snap := sampleInterface()
	snap.Method = "TRACE"
	if _, err := BuildScript(sampleSpec(snap)); err == nil {
		t.Fatal("非法 HTTP 方法应被拒绝")
	}

	// 空接口范围 → 拒绝。
	spec := sampleSpec(sampleInterface())
	spec.Interfaces = nil
	if _, err := BuildScript(spec); err == nil {
		t.Fatal("空接口范围应被拒绝")
	}
}

// TestBuildK6ScriptLegacySnapshotFallsBackToURL 迁移前创建的任务快照（仅有 url）仍可执行。
func TestBuildK6ScriptLegacySnapshotFallsBackToURL(t *testing.T) {
	snap := domain.InterfaceSnapshot{
		InterfaceID: 12,
		Name:        "历史接口",
		Method:      "GET",
		URL:         "https://legacy.example.test/orders?page=1",
		Status:      domain.InterfaceStatusEnabled,
	}
	script, err := BuildScript(sampleSpec(snap))
	if err != nil {
		t.Fatalf("历史快照应可生成脚本: %v", err)
	}
	if !strings.Contains(script, `url: "https://legacy.example.test/orders?page=1"`) {
		t.Fatalf("历史快照 URL 未透传:\n%s", script)
	}

	// 历史 URL 仍走白名单校验。
	for _, u := range []string{"ftp://example.test/x", "file:///etc/passwd", "https://exa mple.test/x", "https://x.test/a\nb"} {
		bad := snap
		bad.URL = u
		if _, err := BuildScript(sampleSpec(bad)); err == nil {
			t.Fatalf("URL %q 应被拒绝", u)
		}
	}
}

// TestBuildK6ScriptEscapesUserInput 脚本只以转义字面量承载用户输入（无原始字符串拼接）。
func TestBuildK6ScriptEscapesUserInput(t *testing.T) {
	snap := sampleInterface()
	// 路径中的引号/分号在 JS 字面量中必须被转义，不能跳出字符串上下文。
	snap.Path = `/o"'orders";id;`
	snap.Body = `{"note":"it's \"quoted\"}"}`

	script, err := BuildScript(sampleSpec(snap))
	if err != nil {
		t.Fatalf("生成脚本失败: %v", err)
	}
	if !strings.Contains(script, "vus: 100") {
		t.Fatalf("脚本缺少并发数: %s", script)
	}
	if strings.Contains(script, `url: https://`) {
		t.Fatal("URL 必须作为转义字符串字面量出现")
	}
	if !strings.Contains(script, `url: "https://10.52.78.246:8443/o\"'orders\";id;"`) {
		t.Fatalf("URL 未正确转义: %s", script)
	}
	if strings.Contains(script, "child_process") {
		t.Fatal("脚本不应包含用户可控代码片段")
	}
}

// TestBuildK6ScriptRejectsInjection 并发数注入一律拒绝（DES-T-001 / TEST-T-001）。
func TestBuildK6ScriptRejectsInjection(t *testing.T) {
	cases := []string{
		`"1;require('child_process').exec('id')"`,
		`"1;exec('id')"`,
		`"1\n"`,
		`"0"`,
		`"-1"`,
	}
	for _, vus := range cases {
		flow := domain.FlowConfig{
			ConcurrencyMode: domain.ConcurrencyModeFixed,
			TargetVUs:       json.RawMessage(vus),
			Duration:        "10s",
		}
		_, err := BuildScript(ScriptSpec{
			Flow:       flow,
			Interfaces: []domain.InterfaceSnapshot{sampleInterface()},
			MaxVUs:     100,
			TargetHost: sampleTargetHost,
		})
		if !errors.Is(err, domain.ErrInvalidConcurrency) {
			t.Fatalf("target_vus=%s 应返回并发数非法, got %v", vus, err)
		}
	}

	// 超过 max_vus 同样拒绝。
	flow := domain.FlowConfig{
		ConcurrencyMode: domain.ConcurrencyModeFixed,
		TargetVUs:       json.RawMessage(`500`),
		Duration:        "10s",
	}
	if _, err := BuildScript(ScriptSpec{
		Flow: flow, Interfaces: []domain.InterfaceSnapshot{sampleInterface()},
		MaxVUs: 100, TargetHost: sampleTargetHost,
	}); !errors.Is(err, domain.ErrInvalidConcurrency) {
		t.Fatalf("超过 max_vus 应拒绝, got %v", err)
	}
}

// TestBuildK6ScriptRampAndPulse 阶梯/脉冲模式生成 ramping-vus 档位。
func TestBuildK6ScriptRampAndPulse(t *testing.T) {
	ramp := domain.FlowConfig{
		ConcurrencyMode: domain.ConcurrencyModeRamp,
		RampStages: []domain.RampStage{
			{VUs: json.RawMessage(`100`), Duration: "60s"},
			{VUs: json.RawMessage(`300`), Duration: "120s"},
		},
	}
	script, err := BuildScript(ScriptSpec{
		Flow: ramp, Interfaces: []domain.InterfaceSnapshot{sampleInterface()},
		MaxVUs: 1000, TargetHost: sampleTargetHost,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"executor: 'ramping-vus'", "target: 100", "target: 300", `duration: "1m0s"`} {
		if !strings.Contains(script, want) {
			t.Fatalf("阶梯脚本缺少 %q:\n%s", want, script)
		}
	}

	pulse := domain.FlowConfig{
		ConcurrencyMode: domain.ConcurrencyModePulse,
		TargetVUs:       json.RawMessage(`50`),
		Duration:        "40s",
	}
	script, err = BuildScript(ScriptSpec{
		Flow: pulse, Interfaces: []domain.InterfaceSnapshot{sampleInterface()},
		MaxVUs: 1000, TargetHost: sampleTargetHost,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "executor: 'ramping-vus'") || !strings.Contains(script, "target: 50") || !strings.Contains(script, "target: 0") {
		t.Fatalf("脉冲脚本档位异常:\n%s", script)
	}
}
