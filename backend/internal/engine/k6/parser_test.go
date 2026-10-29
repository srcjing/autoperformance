package k6

import (
	"strings"
	"testing"
	"time"
)

// TestParseK6Output 解析 k6 `--out json` 逐行输出（仅取 Point 行）。
func TestParseK6Output(t *testing.T) {
	const raw = `{"metric":"http_req_duration","type":"Point","data":{"time":"2026-10-26T08:00:00Z","value":12.5,"tags":{"status":"200"}}}
{"metric":"http_req_duration","type":"Point","data":{"time":"2026-10-26T08:00:01Z","value":20}}
{"metric":"http_reqs","type":"Point","data":{"time":"2026-10-26T08:00:01Z","value":1}}
{"metric":"http_req_failed","type":"Point","data":{"time":"2026-10-26T08:00:01Z","value":0}}
{"metric":"http_req_failed","type":"Point","data":{"time":"2026-10-26T08:00:02Z","value":1}}
{"metric":"http_req_duration","type":"Metric","data":{"time":"0001-01-01T00:00:00Z","value":0}}
not-json-line
`
	res, err := Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if len(res.Samples) != 5 {
		t.Fatalf("样本数 = %d, want 5", len(res.Samples))
	}
	if !res.StartedAt.Equal(time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("StartedAt = %v", res.StartedAt)
	}
	if res.Samples[0].Metric != "http_req_duration" || res.Samples[0].Value != 12.5 {
		t.Fatalf("首条样本异常: %+v", res.Samples[0])
	}

	if _, err := Parse(nil); err == nil {
		t.Fatal("空输入应返回错误")
	}
}
