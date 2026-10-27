// Package evaluator 阈值评估器。
//
// 对齐 architecture.md §5.4 与 INV-2：把 k6 原始样本归一化为 p95/p99/错误率/吞吐量/
// 成功率等口径，再逐条对比任务创建时固化的阈值快照（AC-TEST-4）。
package evaluator

import (
	"math"
	"sort"
	"time"
)

// Metric 指标类型（取值与 PRD §9.1 / domain 阈值指标一致）。
type Metric string

// 归一化指标（PRD §9.1，决策 #12：响应时间 P95+P99，错误率=失败/总请求）。
const (
	// MetricAvgRT 平均响应时间（ms）。
	MetricAvgRT Metric = "avg_rt"
	// MetricP95 95 分位响应时间（ms）。
	MetricP95 Metric = "p95"
	// MetricP99 99 分位响应时间（ms）。
	MetricP99 Metric = "p99"
	// MetricMaxRT 最大响应时间（ms）。
	MetricMaxRT Metric = "max_rt"
	// MetricThroughput 吞吐量（req/s）。
	MetricThroughput Metric = "throughput"
	// MetricErrorRate 错误率（%，失败请求/总请求 ×100）。
	MetricErrorRate Metric = "error_rate"
	// MetricSuccessRate 成功率（%）。
	MetricSuccessRate Metric = "success_rate"
	// MetricConcurrency 并发数（个）。
	MetricConcurrency Metric = "concurrency"
)

// k6 结果中的指标名（--out json 输出的 metric 字段）。
const (
	k6MetricDuration = "http_req_duration"
	k6MetricRequests = "http_reqs"
	k6MetricFailed   = "http_req_failed"
)

// Sample 引擎结果的指标样本（最小投影，避免评估层依赖具体引擎实现）。
type Sample struct {
	// Metric 原始指标名（k6: http_req_duration/http_reqs/http_req_failed 等）。
	Metric string
	// Value 采样值（http_req_duration 为毫秒；http_req_failed 为 0/1）。
	Value float64
	// Time 采样时间（用于推断执行窗口；可为零值）。
	Time time.Time
}

// Options 归一化参数。
type Options struct {
	// Wall 实际执行窗口；<=0 时由样本时间跨度推断。
	Wall time.Duration
	// MaxVUs 配置的目标并发（threshold 指标 concurrency 使用）。
	MaxVUs int
}

// Normalize 将 k6 样本归一化为可评估指标集合（INV-2）。
//
// 口径：
//   - p95/p99：对响应时间样本排序后取分位（nearest-rank）；
//   - error_rate = 失败请求 / 总请求 × 100（%），成功率 = 100 - 错误率；
//   - throughput = 总请求 / 执行窗口秒数。
func Normalize(samples []Sample, opts Options) map[Metric]float64 {
	out := make(map[Metric]float64, 8)

	var (
		durations []float64
		total     int
		failed    float64
		minTime   time.Time
		maxTime   time.Time
	)
	for _, s := range samples {
		if !s.Time.IsZero() {
			if minTime.IsZero() || s.Time.Before(minTime) {
				minTime = s.Time
			}
			if maxTime.IsZero() || s.Time.After(maxTime) {
				maxTime = s.Time
			}
		}
		switch s.Metric {
		case k6MetricDuration:
			durations = append(durations, s.Value)
		case k6MetricRequests:
			total++
		case k6MetricFailed:
			failed += s.Value
		}
	}
	if total == 0 {
		total = len(durations)
	}

	if len(durations) > 0 {
		sorted := append([]float64(nil), durations...)
		sort.Float64s(sorted)
		sum := 0.0
		for _, v := range sorted {
			sum += v
		}
		out[MetricAvgRT] = sum / float64(len(sorted))
		out[MetricP95] = percentile(sorted, 0.95)
		out[MetricP99] = percentile(sorted, 0.99)
		out[MetricMaxRT] = sorted[len(sorted)-1]
	}

	if total > 0 {
		if failed < 0 {
			failed = 0
		}
		if failed > float64(total) {
			failed = float64(total)
		}
		errRate := failed / float64(total) * 100
		out[MetricErrorRate] = errRate
		out[MetricSuccessRate] = 100 - errRate
	}

	wall := opts.Wall
	if wall <= 0 && !minTime.IsZero() && maxTime.After(minTime) {
		wall = maxTime.Sub(minTime)
	}
	if wall > 0 && total > 0 {
		out[MetricThroughput] = float64(total) / wall.Seconds()
	}
	if opts.MaxVUs > 0 {
		out[MetricConcurrency] = float64(opts.MaxVUs)
	}
	return out
}

// percentile 有序样本的 nearest-rank 分位（p ∈ (0,1]）。
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
