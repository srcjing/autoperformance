package k6

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"auto_performance/internal/domain"
)

// sampleInterface 合法接口快照（供脚本生成测试复用）。
func sampleInterface() domain.InterfaceSnapshot {
	return domain.InterfaceSnapshot{
		InterfaceID: 11,
		Name:        "查询订单",
		Method:      "GET",
		URL:         "https://api.example.test/orders?page=1",
		Headers:     map[string]string{"X-Trace-Id": "abc"},
		Status:      domain.InterfaceStatusEnabled,
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
	if _, err := BuildScript(ScriptSpec{Flow: flow, Interfaces: []domain.InterfaceSnapshot{sampleInterface()}, MaxVUs: 100}); !errors.Is(err, domain.ErrInvalidConcurrency) {
		t.Fatalf("超过 max_vus 应拒绝, got %v", err)
	}
}

// TestBuildK6ScriptEscapesUserInput 脚本只以转义字面量承载用户输入（无原始字符串拼接）。
func TestBuildK6ScriptEscapesUserInput(t *testing.T) {
	snap := sampleInterface()
	// URL 中的引号/分号在 JS 字面量中必须被转义，不能跳出字符串上下文。
	snap.URL = `https://api.example.test/o"'orders";id;`
	snap.Body = `{"note":"it's \"quoted\"}"}`

	flow := domain.FlowConfig{
		ConcurrencyMode: domain.ConcurrencyModeFixed,
		TargetVUs:       json.RawMessage(`100`),
		Duration:        "60s",
		Timeout:         "5s",
		ThinkTime:       "0.5s",
	}
	script, err := BuildScript(ScriptSpec{Flow: flow, Interfaces: []domain.InterfaceSnapshot{snap}, MaxVUs: 1000})
	if err != nil {
		t.Fatalf("生成脚本失败: %v", err)
	}
	if !strings.Contains(script, "vus: 100") {
		t.Fatalf("脚本缺少并发数: %s", script)
	}
	if strings.Contains(script, `url: https://`) {
		t.Fatal("URL 必须作为转义字符串字面量出现")
	}
	if !strings.Contains(script, `url: "https://api.example.test/o\"'orders\";id;"`) {
		t.Fatalf("URL 未正确转义: %s", script)
	}
	if strings.Contains(script, "child_process") {
		t.Fatal("脚本不应包含用户可控代码片段")
	}
}

// TestBuildK6ScriptValidatesURLAndMethod 接口地址/方法白名单（DES-T-001）。
func TestBuildK6ScriptValidatesURLAndMethod(t *testing.T) {
	flow := domain.FlowConfig{
		ConcurrencyMode: domain.ConcurrencyModeFixed,
		TargetVUs:       json.RawMessage(`10`),
		Duration:        "10s",
	}
	base := sampleInterface()

	for _, u := range []string{"", "ftp://example.test/x", "file:///etc/passwd", "https://exa mple.test/x", "https://x.test/a\nb"} {
		snap := base
		snap.URL = u
		if _, err := BuildScript(ScriptSpec{Flow: flow, Interfaces: []domain.InterfaceSnapshot{snap}, MaxVUs: 100}); err == nil {
			t.Fatalf("URL %q 应被拒绝", u)
		}
	}

	snap := base
	snap.Method = "TRACE"
	if _, err := BuildScript(ScriptSpec{Flow: flow, Interfaces: []domain.InterfaceSnapshot{snap}, MaxVUs: 100}); err == nil {
		t.Fatal("非法 HTTP 方法应被拒绝")
	}

	// 空接口范围 → 拒绝。
	if _, err := BuildScript(ScriptSpec{Flow: flow, Interfaces: nil, MaxVUs: 100}); err == nil {
		t.Fatal("空接口范围应被拒绝")
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
	script, err := BuildScript(ScriptSpec{Flow: ramp, Interfaces: []domain.InterfaceSnapshot{sampleInterface()}, MaxVUs: 1000})
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
	script, err = BuildScript(ScriptSpec{Flow: pulse, Interfaces: []domain.InterfaceSnapshot{sampleInterface()}, MaxVUs: 1000})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "executor: 'ramping-vus'") || !strings.Contains(script, "target: 50") || !strings.Contains(script, "target: 0") {
		t.Fatalf("脉冲脚本档位异常:\n%s", script)
	}
}
