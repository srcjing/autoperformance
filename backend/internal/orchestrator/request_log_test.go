package orchestrator

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"auto_performance/internal/domain"
	"auto_performance/internal/engine/k6"
)

// fakeRequestWriter 请求详情写入桩：记录落库条目。
type fakeRequestWriter struct {
	mu      sync.Mutex
	calls   int
	entries []domain.RequestLog
	err     error
}

func (f *fakeRequestWriter) WriteRequestLogs(_ context.Context, _ int64, entries []domain.RequestLog) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.err != nil {
		return f.err
	}
	f.entries = append(f.entries, entries...)
	return nil
}

// requestLogTask 构造带接口快照的任务（快照提供请求入参，k6 结果提供实际方法/状态）。
func requestLogTask() *domain.TestTask {
	return &domain.TestTask{
		ID:         42,
		Name:       "请求详情任务",
		TargetHost: "10.0.0.1",
		InterfaceSnapshots: []domain.InterfaceSnapshot{{
			InterfaceID: 11,
			Name:        "查询订单",
			Method:      "GET",
			Scheme:      "http",
			Port:        8080,
			Path:        "/api/orders",
			Body:        `{"page":1}`,
		}},
	}
}

// sampleResult 构造 k6 解析结果（一次成功 + 一次失败请求）。
func sampleResult() *k6.Result {
	return &k6.Result{Samples: []k6.Sample{
		{Metric: "http_req_duration", Value: 30, Time: time.Unix(1000, 0), Tags: map[string]string{
			"method": "GET", "url": "http://10.0.0.1:8080/api/orders", "status": "200", "interface_id": "11",
		}},
		{Metric: "http_req_duration", Value: 120, Time: time.Unix(1001, 0), Tags: map[string]string{
			"method": "GET", "url": "http://10.0.0.1:8080/api/orders", "status": "0",
			"interface_id": "11", "error": "dial: connection refused", "error_code": "1000",
		}},
	}}
}

// TestRecordRequestLogsWritesDetails k6 结果解析后按请求明细批量写入（补全任务/接口/入参）。
func TestRecordRequestLogsWritesDetails(t *testing.T) {
	writer := &fakeRequestWriter{}
	o := New(Deps{Store: &fakeStore{task: requestLogTask()}, RequestLogs: writer})

	o.recordRequestLogs(context.Background(), requestLogTask(), sampleResult(), nil)

	if writer.calls != 1 {
		t.Fatalf("应批量写入一次, got %d", writer.calls)
	}
	if len(writer.entries) != 2 {
		t.Fatalf("明细数 = %d, want 2", len(writer.entries))
	}
	first, failed := writer.entries[0], writer.entries[1]
	if first.TaskID != 42 || first.InterfaceID != 11 {
		t.Fatalf("任务/接口未补全: %+v", first)
	}
	if first.RequestBody != `{"page":1}` {
		t.Fatalf("请求入参未从接口快照补全: %q", first.RequestBody)
	}
	if first.ResponseStatus != 200 || first.DurationMs != 30 {
		t.Fatalf("状态/耗时异常: %+v", first)
	}
	if failed.ErrorCode != "1000" || !strings.Contains(failed.ErrorMessage, "connection refused") {
		t.Fatalf("失败明细未携带错误码/错误信息: %+v", failed)
	}
}

// TestRecordRequestLogsWithoutWriter 未注入写入能力（或日志关闭）时静默跳过，不影响执行。
func TestRecordRequestLogsWithoutWriter(t *testing.T) {
	o := New(Deps{Store: &fakeStore{task: requestLogTask()}})
	o.recordRequestLogs(context.Background(), requestLogTask(), sampleResult(), nil)
}

// TestRecordRequestLogsSwallowsError 写入失败仅记录测试日志，不阻断任务。
func TestRecordRequestLogsSwallowsError(t *testing.T) {
	store := &fakeStore{task: requestLogTask()}
	writer := &fakeRequestWriter{err: errors.New("db down")}
	o := New(Deps{Store: store, RequestLogs: writer})

	o.recordRequestLogs(context.Background(), requestLogTask(), sampleResult(), nil)

	if writer.calls != 1 {
		t.Fatalf("应尝试写入一次, got %d", writer.calls)
	}
	joined := strings.Join(store.logs, "\n")
	if !strings.Contains(joined, "请求详情") {
		t.Fatalf("写入失败应留下测试日志线索: %v", store.logs)
	}
}
