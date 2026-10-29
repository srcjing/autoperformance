package reportgen

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"auto_performance/internal/domain"
	"auto_performance/internal/evaluator"
)

// sampleEvaluation 两条接口：一个达标、一个不达标（覆盖高亮分支）。
func sampleEvaluation() evaluator.Evaluation {
	return evaluator.Evaluation{
		State: evaluator.StateFail,
		Pass:  false,
		Items: []evaluator.Item{
			{
				InterfaceID:   11,
				InterfaceName: "查询订单",
				Metric:        evaluator.MetricP95,
				Operator:      domain.OperatorLE,
				Threshold:     500,
				Actual:        210,
				HasActual:     true,
				Pass:          true,
			},
			{
				InterfaceID:   12,
				InterfaceName: "创建订单",
				Metric:        evaluator.MetricErrorRate,
				Operator:      domain.OperatorLE,
				Threshold:     1,
				Actual:        2.5,
				HasActual:     true,
				Pass:          false,
				Reason:        "error_rate=2.5 未满足 le 1",
			},
		},
		Missing: []evaluator.Metric{evaluator.MetricP99},
	}
}

func sampleBuildInput() BuildInput {
	return BuildInput{
		ReportID:        101,
		TaskID:          9,
		Title:           "订单服务 性能测试报告",
		TaskName:        "订单服务压测",
		Environment:     "预发环境",
		Source:          domain.ReportSourceInternal,
		GeneratedAt:     time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC),
		DurationSeconds: 65,
		Metrics: map[evaluator.Metric]float64{
			evaluator.MetricP95:        210,
			evaluator.MetricErrorRate:  2.5,
			evaluator.MetricThroughput: 128.456,
		},
		Evaluation: sampleEvaluation(),
		Interfaces: []domain.InterfaceSnapshot{
			{
				InterfaceID: 11,
				Name:        "查询订单",
				Method:      "GET",
				URL:         "https://api.example.test/orders?token=secret-token#frag",
			},
			{
				InterfaceID: 12,
				Name:        "创建订单",
				Method:      "POST",
				URL:         "https://api.example.test/orders",
			},
		},
	}
}

// TestGenerateHTMLContainsPassFail 校验 HTML 报告包含达标/不达标高亮与关键统计。
func TestGenerateHTMLContainsPassFail(t *testing.T) {
	data := BuildData(sampleBuildInput())

	var buf bytes.Buffer
	if err := RenderHTML(&buf, data); err != nil {
		t.Fatalf("渲染 HTML 报告失败: %v", err)
	}
	html := buf.String()

	for _, want := range []string{
		"<!DOCTYPE html>",
		// 达标/不达标文案与高亮样式（行级 + 徽标级）。
		"达标", "不达标",
		"row-pass", "row-fail",
		"badge-pass", "badge-fail",
		// 接口与统计。
		"查询订单", "创建订单",
		"50%", // 1/2 接口达标
		"订单服务压测",
		"1 分 05 秒",
		// 指标与阈值快照。
		"P95 响应时间", "210",
		"error_rate=2.5 未满足 le 1",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("报告缺少内容 %q", want)
		}
	}

	// 接口地址脱敏：保留 scheme/host/path，剥离查询串与片段（DES-I-001）。
	if strings.Contains(html, "secret-token") || strings.Contains(html, "#frag") {
		t.Errorf("报告泄露接口地址查询串/片段: %s", html)
	}
	if !strings.Contains(html, "https://api.example.test/orders") {
		t.Errorf("报告应保留脱敏后的接口地址")
	}

	// 离线可读：不得引用任何外部样式/脚本资源。
	if strings.Contains(html, "<link") || strings.Contains(html, "src=\"http") || strings.Contains(html, "src='http") {
		t.Errorf("报告存在外部资源引用，无法离线读取")
	}
	if !strings.Contains(html, "<style>") || !strings.Contains(html, "<script>") {
		t.Errorf("报告应内联 CSS/JS")
	}
}

// TestGenerateHTMLUnconfigured 校验未配置阈值时给出明确口径提示。
func TestGenerateHTMLUnconfigured(t *testing.T) {
	in := sampleBuildInput()
	in.Evaluation = evaluator.Evaluation{State: evaluator.StateUnconfigured}
	in.Interfaces = nil
	in.Metrics = nil

	var buf bytes.Buffer
	if err := RenderHTML(&buf, BuildData(in)); err != nil {
		t.Fatalf("渲染 HTML 报告失败: %v", err)
	}
	html := buf.String()
	if !strings.Contains(html, "未配置阈值") {
		t.Errorf("报告应提示未配置阈值: %s", html)
	}
	if !strings.Contains(html, "0%") {
		t.Errorf("未配置阈值时接口达标率应为 0%%")
	}
}

// TestMaskURL 校验接口地址脱敏不泄露查询串/用户信息。
func TestMaskURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://api.example.test/orders?token=abc", "https://api.example.test/orders"},
		{"https://user:pass@api.example.test:8443/a/b?x=1#f", "https://api.example.test:8443/a/b"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := MaskURL(tc.in); got != tc.want {
			t.Errorf("MaskURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := MaskURL("not-a-url"); strings.Contains(got, "not-a-url") {
		t.Errorf("非法地址不应原样输出: %q", got)
	}
}
