package domain

import (
	"encoding/json"
	"testing"
)

// flowConfig 构造流程测试配置。
func flowConfig(steps ...FlowStep) *FlowConfig {
	return &FlowConfig{
		ConcurrencyMode: ConcurrencyModeFixed,
		TargetVUs:       json.RawMessage(`10`),
		Duration:        "30s",
		TestMode:        TestModeFlow,
		Steps:           steps,
	}
}

// TestValidateStepsDefaultsToSingle 未声明模式时视为单接口测试（不校验步骤）。
func TestValidateStepsDefaultsToSingle(t *testing.T) {
	f := &FlowConfig{ConcurrencyMode: ConcurrencyModeFixed}
	steps, err := f.ValidateSteps()
	if err != nil {
		t.Fatalf("单接口模式不应报错: %v", err)
	}
	if steps != nil {
		t.Fatalf("单接口模式应返回空步骤: %v", steps)
	}
	if f.TestModeValue() != TestModeSingle {
		t.Fatalf("空模式应归一化为 single，得到 %q", f.TestModeValue())
	}
}

// TestValidateStepsOrdersBySeqAndDefaultsScope 步骤按 Seq 排序，空作用域默认 once_per_iteration。
func TestValidateStepsOrdersBySeqAndDefaultsScope(t *testing.T) {
	f := flowConfig(
		FlowStep{Seq: 2, InterfaceID: 12, Scope: ScopeOncePerTest},
		FlowStep{Seq: 1, InterfaceID: 11},
	)
	steps, err := f.ValidateSteps()
	if err != nil {
		t.Fatalf("校验失败: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("步骤数量错误: %d", len(steps))
	}
	if steps[0].InterfaceID != 11 || steps[0].Scope != ScopeOncePerIteration {
		t.Fatalf("排序/默认作用域错误: %+v", steps[0])
	}
	if steps[1].InterfaceID != 12 || steps[1].Scope != ScopeOncePerTest {
		t.Fatalf("排序错误: %+v", steps[1])
	}
}

// TestValidateStepsRejectsInvalid 非法步骤配置全部拒绝。
func TestValidateStepsRejectsInvalid(t *testing.T) {
	cases := []struct {
		name  string
		steps []FlowStep
	}{
		{"缺少步骤", nil},
		{"缺少接口", []FlowStep{{Seq: 1, Scope: ScopeOncePerIteration}}},
		{"作用域非法", []FlowStep{{Seq: 1, InterfaceID: 1, Scope: "sometimes"}}},
		{"无主流程步骤", []FlowStep{{Seq: 1, InterfaceID: 1, Scope: ScopeOncePerTest}}},
		{"变量名非法", []FlowStep{{Seq: 1, InterfaceID: 1, Extract: []ExtractRule{{Name: "1token", Source: ExtractSourceJSONPath, Expr: "$.token"}}}}},
		{"来源非法", []FlowStep{{Seq: 1, InterfaceID: 1, Extract: []ExtractRule{{Name: "token", Source: "xpath", Expr: "$.token"}}}}},
		{"JSONPath 缺少前缀", []FlowStep{{Seq: 1, InterfaceID: 1, Extract: []ExtractRule{{Name: "token", Source: ExtractSourceJSONPath, Expr: "data.token"}}}}},
		{"正则不可编译", []FlowStep{{Seq: 1, InterfaceID: 1, Extract: []ExtractRule{{Name: "token", Source: ExtractSourceRegex, Expr: "([a-z"}}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := flowConfig(tc.steps...).ValidateSteps(); err == nil {
				t.Fatalf("应拒绝非法步骤: %+v", tc.steps)
			}
		})
	}
}

// TestStepInterfaceIDsDedupes 步骤接口 ID 去重保序。
func TestStepInterfaceIDsDedupes(t *testing.T) {
	f := flowConfig(
		FlowStep{Seq: 1, InterfaceID: 3},
		FlowStep{Seq: 2, InterfaceID: 3},
		FlowStep{Seq: 3, InterfaceID: 7},
	)
	ids := f.StepInterfaceIDs()
	if len(ids) != 2 || ids[0] != 3 || ids[1] != 7 {
		t.Fatalf("接口 ID 去重错误: %v", ids)
	}
}

// TestValidateStepsRejectsReservedVarPrefix 变量名不得使用平台保留前缀 __af_（Cookie 传递用）。
func TestValidateStepsRejectsReservedVarPrefix(t *testing.T) {
	f := flowConfig(FlowStep{
		Seq: 1, InterfaceID: 1,
		Extract: []ExtractRule{{Name: "__af_cookies", Source: ExtractSourceJSONPath, Expr: "$.x"}},
	})
	if _, err := f.ValidateSteps(); err == nil {
		t.Fatal("保留前缀 __af_ 应被拒绝")
	}
}
