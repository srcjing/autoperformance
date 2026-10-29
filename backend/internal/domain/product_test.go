package domain

import (
	"strings"
	"testing"
)

// TestValidateThresholdRejectsInvalidCombination 阈值方向与指标类型必须匹配（INV-2 / PRD §9.3）。
// 响应时间类与错误率只能「越小越好」（le）；吞吐量/成功率/并发数只能「越大越好」（ge）。
func TestValidateThresholdRejectsInvalidCombination(t *testing.T) {
	if err := ValidateThreshold("success_rate", "le"); err == nil {
		t.Fatal("expected error for success_rate + le")
	}
	if err := ValidateThreshold("p95", "ge"); err == nil {
		t.Fatal("expected error for p95 + ge")
	}
	if err := ValidateThreshold("p95", "le"); err != nil {
		t.Fatalf("p95 + le should be valid, got %v", err)
	}
	if err := ValidateThreshold("throughput", "ge"); err != nil {
		t.Fatalf("throughput + ge should be valid, got %v", err)
	}
}

// TestValidateThresholdWhitelist 白名单表完整性：8 个指标全部可校验，未知指标/方向一律拒绝。
func TestValidateThresholdWhitelist(t *testing.T) {
	if len(Metrics()) != 8 {
		t.Fatalf("指标应为 8 个（PRD §9.1），got %d", len(Metrics()))
	}
	for _, metric := range Metrics() {
		ops := AllowedOperators(metric)
		if len(ops) == 0 {
			t.Fatalf("指标 %s 未配置允许方向", metric)
		}
		if err := ValidateThreshold(metric, ops[0]); err != nil {
			t.Fatalf("指标 %s 合法方向 %s 被拒绝: %v", metric, ops[0], err)
		}
		if DefaultThresholdUnit(metric) == "" {
			t.Fatalf("指标 %s 未配置默认单位", metric)
		}
	}

	for _, tc := range []struct{ metric, operator string }{
		{"rt", OperatorLE},              // 未知指标
		{MetricP99, "lt"},               // 未知方向
		{"", OperatorLE},                // 空指标
		{MetricConcurrency, OperatorLE}, // 并发数只允许 ge
		{MetricErrorRate, OperatorGE},   // 错误率只允许 le
		{MetricSuccessRate, OperatorGE}, // 合法组合（对照组）
	} {
		err := ValidateThreshold(tc.metric, tc.operator)
		wantOK := tc.metric == MetricSuccessRate
		if wantOK && err != nil {
			t.Errorf("%s+%s 应合法, got %v", tc.metric, tc.operator, err)
		}
		if !wantOK && err == nil {
			t.Errorf("%s+%s 应被拒绝", tc.metric, tc.operator)
		}
	}
}

// TestIsSensitiveHeader 认证头识别：大小写/空白不敏感，普通头不误判（DES-I-001）。
func TestIsSensitiveHeader(t *testing.T) {
	for _, name := range []string{
		"Authorization", "authorization", "  Token ", "X-Api-Key", "api-key",
		"Cookie", "Proxy-Authorization", "X-Auth-Token", "Auth-Token", "X-Access-Token",
	} {
		if !IsSensitiveHeader(name) {
			t.Errorf("%q 应识别为认证头", name)
		}
	}
	for _, name := range []string{"Content-Type", "Accept", "X-Trace-Id", "", "  "} {
		if IsSensitiveHeader(name) {
			t.Errorf("%q 不应识别为认证头", name)
		}
	}
}

// TestSplitHeaders 认证头与普通头分流：认证头进入敏感集合（加密后落 headers_enc），
// 普通头留在 headers；同时保持原值不被截断，且入参不被修改。
func TestSplitHeaders(t *testing.T) {
	headers := map[string]string{
		"Authorization": "Bearer secret-token",
		"content-type":  "application/json",
		"Token":         "tk-123",
		"Accept":        "*/*",
	}
	safe, sensitive := SplitHeaders(headers)

	if len(safe) != 2 || safe["content-type"] != "application/json" || safe["Accept"] != "*/*" {
		t.Fatalf("普通头分流异常: %v", safe)
	}
	if len(sensitive) != 2 || sensitive["Authorization"] != "Bearer secret-token" || sensitive["Token"] != "tk-123" {
		t.Fatalf("认证头分流异常: %v", sensitive)
	}
	if len(headers) != 4 {
		t.Fatalf("入参不应被修改，got %v", headers)
	}

	safe, sensitive = SplitHeaders(nil)
	if len(safe) != 0 || len(sensitive) != 0 {
		t.Fatalf("空输入应返回空集合: %v %v", safe, sensitive)
	}
}

// TestRedactHeaderValues 脱敏后的头值不得包含原值（仅保留掩码）。
func TestRedactHeaderValues(t *testing.T) {
	masked := RedactHeaderValues(map[string]string{"Authorization": "Bearer secret-token"})
	if masked["Authorization"] == "Bearer secret-token" {
		t.Fatal("脱敏后不应保留明文认证头")
	}
	if strings.Contains(masked["Authorization"], "secret-token") {
		t.Fatalf("脱敏值泄露原值: %s", masked["Authorization"])
	}
}
