package k6

import (
	"strings"
	"testing"
	"time"
)

// TestRequestDetailsFromDurationPoints 由 http_req_duration 点位还原逐请求明细
// （每点位对应一次真实请求，tags 携带 method/url/status/interface_id/error）。
func TestRequestDetailsFromDurationPoints(t *testing.T) {
	const raw = `{"metric":"http_req_duration","type":"Point","data":{"time":"2026-10-28T10:00:00Z","value":12.5,"tags":{"method":"GET","url":"http://10.0.0.1:8080/api/a","status":"200","interface_id":"11"}}}
{"metric":"http_req_duration","type":"Point","data":{"time":"2026-10-28T10:00:01Z","value":5000,"tags":{"method":"POST","url":"http://10.0.0.1:8080/api/b","status":"0","interface_id":"12","error":"dial: connection refused","error_code":"1000"}}}
{"metric":"http_reqs","type":"Point","data":{"time":"2026-10-28T10:00:01Z","value":1}}
{"metric":"http_req_failed","type":"Point","data":{"time":"2026-10-28T10:00:02Z","value":0}}
`
	res, err := Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	details := res.RequestDetails()
	if len(details) != 2 {
		t.Fatalf("明细数 = %d, want 2（按请求一个点位）", len(details))
	}
	first := details[0]
	if first.InterfaceID != 11 || first.Method != "GET" || first.Status != 200 {
		t.Fatalf("首条明细异常: %+v", first)
	}
	if first.URL != "http://10.0.0.1:8080/api/a" || first.DurationMs != 12 {
		t.Fatalf("URL/耗时异常: %+v", first)
	}
	if !first.Time.Equal(time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("发生时间异常: %v", first.Time)
	}

	failed := details[1]
	if failed.Status != 0 || failed.ErrorCode != "1000" || failed.ErrorMessage != "dial: connection refused" {
		t.Fatalf("失败请求明细异常: %+v", failed)
	}
	if failed.DurationMs != 5000 {
		t.Fatalf("失败请求耗时 = %d, want 5000", failed.DurationMs)
	}
}

// TestRequestDetailsFallbackToRequests 无 duration 点位时回退到 http_reqs（耗时记 0）。
func TestRequestDetailsFallbackToRequests(t *testing.T) {
	const raw = `{"metric":"http_reqs","type":"Point","data":{"time":"2026-10-28T10:00:00Z","value":1,"tags":{"method":"DELETE","url":"http://h/api/c","status":"204"}}}`
	res, err := Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	details := res.RequestDetails()
	if len(details) != 1 || details[0].Method != "DELETE" || details[0].Status != 204 {
		t.Fatalf("回退明细异常: %+v", details)
	}
	if details[0].DurationMs != 0 {
		t.Fatalf("回退时耗时应为 0, got %d", details[0].DurationMs)
	}
}

// TestBuildScriptTagsInterfaceID 脚本为每个请求带上 interface_id 标签（请求详情按接口定位）。
func TestBuildScriptTagsInterfaceID(t *testing.T) {
	script, err := BuildScript(sampleSpec(sampleInterface()))
	if err != nil {
		t.Fatalf("生成脚本失败: %v", err)
	}
	if !strings.Contains(script, `interfaceId: "11"`) {
		t.Fatalf("脚本缺少 interfaceId 字面量:\n%s", script)
	}
	if !strings.Contains(script, `tags: { interface_id: req.interfaceId }`) {
		t.Fatalf("脚本未给请求打 interface_id 标签:\n%s", script)
	}
}

// TestRequestDetailsEmpty 无 HTTP 点位 → 返回空切片（不返回 nil，便于调用方判空）。
func TestRequestDetailsEmpty(t *testing.T) {
	res := &Result{}
	details := res.RequestDetails()
	if details == nil || len(details) != 0 {
		t.Fatalf("无点位应返回空切片, got %v", details)
	}
}
