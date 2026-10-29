package evaluator

import (
	"math"
	"testing"
	"time"
)

// TestNormalizeMetrics 校验 k6 样本归一化口径（INV-2 / 决策 #12）。
func TestNormalizeMetrics(t *testing.T) {
	// 100 个请求：95 个 10ms，5 个 100ms；其中 2 个失败；执行窗口 10s。
	samples := make([]Sample, 0, 205)
	for i := 0; i < 95; i++ {
		samples = append(samples, Sample{Metric: "http_req_duration", Value: 10})
	}
	for i := 0; i < 5; i++ {
		samples = append(samples, Sample{Metric: "http_req_duration", Value: 100})
	}
	for i := 0; i < 100; i++ {
		samples = append(samples, Sample{Metric: "http_reqs", Value: 1})
	}
	samples = append(samples,
		Sample{Metric: "http_req_failed", Value: 1},
		Sample{Metric: "http_req_failed", Value: 1},
	)

	got := Normalize(samples, Options{Wall: 10 * time.Second, MaxVUs: 100})

	if got[MetricAvgRT] != 14.5 {
		t.Fatalf("avg_rt = %v, want 14.5", got[MetricAvgRT])
	}
	if got[MetricP95] != 10 {
		t.Fatalf("p95 = %v, want 10（nearest-rank）", got[MetricP95])
	}
	if got[MetricP99] != 100 {
		t.Fatalf("p99 = %v, want 100", got[MetricP99])
	}
	if got[MetricMaxRT] != 100 {
		t.Fatalf("max_rt = %v, want 100", got[MetricMaxRT])
	}
	if math.Abs(got[MetricErrorRate]-2) > 1e-9 {
		t.Fatalf("error_rate = %v, want 2(%%)", got[MetricErrorRate])
	}
	if math.Abs(got[MetricSuccessRate]-98) > 1e-9 {
		t.Fatalf("success_rate = %v, want 98(%%)", got[MetricSuccessRate])
	}
	if math.Abs(got[MetricThroughput]-10) > 1e-9 {
		t.Fatalf("throughput = %v, want 10", got[MetricThroughput])
	}
	if got[MetricConcurrency] != 100 {
		t.Fatalf("concurrency = %v, want 100", got[MetricConcurrency])
	}

	// 空样本不产出分位/吞吐指标（由评估器判 Missing）。
	if empty := Normalize(nil, Options{}); len(empty) != 0 {
		t.Fatalf("空样本应无指标, got %v", empty)
	}
}

// TestPercentileOrdering 分位依赖排序而非输入顺序。
func TestPercentileOrdering(t *testing.T) {
	samples := []Sample{
		{Metric: "http_req_duration", Value: 300},
		{Metric: "http_req_duration", Value: 1},
		{Metric: "http_req_duration", Value: 200},
		{Metric: "http_req_duration", Value: 100},
	}
	got := Normalize(samples, Options{})
	if got[MetricP95] != 300 || got[MetricP99] != 300 {
		t.Fatalf("p95/p99 应取排序后最大值, got %v/%v", got[MetricP95], got[MetricP99])
	}
	if got[MetricAvgRT] != 150.25 {
		t.Fatalf("avg_rt = %v, want 150.25", got[MetricAvgRT])
	}
}
