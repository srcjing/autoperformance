package k6

import (
	"encoding/json"
	"strings"
	"testing"

	"auto_performance/internal/domain"
)

// flowSpec 构造流程测试脚本入参（登录 + 主流程 + 每请求前置）。
func flowSpec() ScriptSpec {
	login := sampleInterface()
	login.InterfaceID = 1
	login.Method = "POST"
	login.Path = "/login"
	login.Body = `{"user":"{{username}}"}`

	order := sampleInterface()
	order.InterfaceID = 2
	order.Method = "GET"
	order.Path = "/orders?token={{token}}"

	legacy := sampleInterface()
	legacy.InterfaceID = 3
	legacy.Method = "POST"
	legacy.Path = "/nonce"

	return ScriptSpec{
		Flow: domain.FlowConfig{
			ConcurrencyMode: domain.ConcurrencyModeFixed,
			TargetVUs:       json.RawMessage(`10`),
			Duration:        "30s",
			Timeout:         "5s",
			TestMode:        domain.TestModeFlow,
			Steps: []domain.FlowStep{
				{Seq: 1, Name: "登录", InterfaceID: 1, Scope: domain.ScopeOncePerTest, Extract: []domain.ExtractRule{
					{Name: "token", Source: domain.ExtractSourceJSONPath, Expr: "$.data.token"},
				}},
				{Seq: 2, InterfaceID: 3, Scope: domain.ScopeEveryRequest, Extract: []domain.ExtractRule{
					{Name: "nonce", Source: domain.ExtractSourceRegex, Expr: `nonce":"([^"]+)`, Group: 1},
				}},
				{Seq: 3, InterfaceID: 2, Scope: domain.ScopeOncePerIteration, Headers: map[string]string{"X-Token": "{{token}}"}},
			},
		},
		Interfaces: []domain.InterfaceSnapshot{login, order, legacy},
		MaxVUs:     1000,
		TargetHost: sampleTargetHost,
	}
}

// TestBuildFlowScriptBucketsStepsByScope 步骤按作用域分桶，生成 setup/MAIN/PRE 三组。
func TestBuildFlowScriptBucketsStepsByScope(t *testing.T) {
	script, err := BuildScript(flowSpec())
	if err != nil {
		t.Fatalf("生成流程脚本失败: %v", err)
	}
	for _, want := range []string{
		"export function setup()",
		"const SETUP_STEPS = [",
		"const MAIN_STEPS = [",
		"const PRE_STEPS = [",
		"runSteps(SETUP_STEPS, vars, cookies)",
		"jar.set(COOKIE_ORIGIN, name, c.value, opts);",
		`const COOKIE_ORIGIN = "https://10.52.78.246:8443"`,
		"url: \"https://10.52.78.246:8443/login\"",
		"url: \"https://10.52.78.246:8443/orders?token={{token}}\"",
		`"X-Token":"{{token}}"`,
		`{"name":"token","source":"jsonpath","expr":"$.data.token"}`,
		`"name":"nonce","source":"regex"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("流程脚本缺少 %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "const REQUESTS = [") {
		t.Fatalf("流程模式不应生成单接口 REQUESTS 数组")
	}
}

// TestBuildFlowScriptRejectsUnknownInterface 步骤引用接口必须在任务接口范围内。
func TestBuildFlowScriptRejectsUnknownInterface(t *testing.T) {
	spec := flowSpec()
	spec.Interfaces = spec.Interfaces[:1]
	if _, err := BuildScript(spec); err == nil {
		t.Fatalf("引用范围外接口应报错")
	}
}

// TestBuildSingleScriptUnchanged 单接口模式保持原 REQUESTS 结构。
func TestBuildSingleScriptUnchanged(t *testing.T) {
	script, err := BuildScript(sampleSpec(sampleInterface()))
	if err != nil {
		t.Fatalf("生成单接口脚本失败: %v", err)
	}
	if !strings.Contains(script, "const REQUESTS = [") {
		t.Fatalf("单接口模式应保留 REQUESTS 数组")
	}
	if strings.Contains(script, "export function setup()") {
		t.Fatalf("单接口模式不应生成 setup")
	}
}

// TestFlowScriptEmitsVariableWarnings 流程脚本带变量告警与步骤标签（排查变量未定义/提取失败）。
func TestFlowScriptEmitsVariableWarnings(t *testing.T) {
	script, err := BuildScript(flowSpec())
	if err != nil {
		t.Fatalf("生成流程脚本失败: %v", err)
	}
	for _, want := range []string{
		"AFWARN|",
		`label: "第 1 步（登录）"`,
		`label: "第 3 步"`,
		"未定义，已按空值发送",
		"提取失败（",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("流程脚本缺少 %q", want)
		}
	}
}

// TestFlowScriptCookieInjectionPerIteration 每迭代注入会话 Cookie 且保留属性。
func TestFlowScriptCookieInjectionOncePerVU(t *testing.T) {
	script, err := BuildScript(flowSpec())
	if err != nil {
		t.Fatalf("生成流程脚本失败: %v", err)
	}
	for _, want := range []string{
		"if (cookies) {",
		"__af_cookies",
		"opts.secure = true;",
		"opts.httpOnly = true;",
		"jar.set(COOKIE_ORIGIN, name, c.value, opts);",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("流程脚本缺少 %q", want)
		}
	}
}
