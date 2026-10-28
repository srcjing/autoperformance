package k6

import (
	"strconv"
	"strings"
	"time"
)

// 请求明细来源指标：一次 HTTP 请求在 k6 `--out json` 中对应一个 duration 点位；
// 无 duration 点位（极少数中断场景）时回退到 http_reqs（无耗时）。
const (
	metricRequestDuration = "http_req_duration"
	metricRequests        = "http_reqs"
)

// RequestDetail k6 单请求明细（请求详情日志的采集来源）。
//
// 说明：k6 JSON 输出不含响应体，ResponseBody 由上层按需留空；
// error/error_code 仅在请求失败时由 k6 写入 tags。
type RequestDetail struct {
	InterfaceID  int64
	Method       string
	URL          string
	Status       int
	ErrorCode    string
	ErrorMessage string
	DurationMs   int64
	Time         time.Time
}

// RequestDetails 还原逐请求明细（保序）。
//
// 耗时取 http_req_duration 点位值（毫秒，向下取整）；无该指标时回退 http_reqs（耗时 0）。
// 结果恒为非 nil 切片，便于调用方直接判空。
func (r *Result) RequestDetails() []RequestDetail {
	out := make([]RequestDetail, 0)
	if r == nil {
		return out
	}
	durations := make([]RequestDetail, 0)
	fallback := make([]RequestDetail, 0)
	for i := range r.Samples {
		s := &r.Samples[i]
		switch s.Metric {
		case metricRequestDuration:
			durations = append(durations, requestDetailOf(s))
		case metricRequests:
			fallback = append(fallback, requestDetailOf(s))
		}
	}
	if len(durations) > 0 {
		return durations
	}
	return fallback
}

// requestDetailOf 单个点位 → 请求明细（标签缺失时降级为零值，不丢弃明细）。
func requestDetailOf(s *Sample) RequestDetail {
	d := RequestDetail{
		InterfaceID:  int64(tagInt(s.Tags["interface_id"])),
		Method:       strings.ToUpper(strings.TrimSpace(s.Tags["method"])),
		URL:          s.Tags["url"],
		Status:       tagInt(s.Tags["status"]),
		ErrorCode:    s.Tags["error_code"],
		ErrorMessage: s.Tags["error"],
		Time:         s.Time,
	}
	if s.Metric == metricRequestDuration && s.Value > 0 {
		d.DurationMs = int64(s.Value)
	}
	return d
}

// tagInt 解析整型标签（缺失/非法返回 0）。
func tagInt(raw string) int {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return v
}
