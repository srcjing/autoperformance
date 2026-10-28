package log

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
	logsvc "auto_performance/internal/service/log"
)

// TestRequestLogDTOAndFilters 请求详情 DTO（ID 字符串化 + 明细字段）与筛选透传。
func TestRequestLogDTOAndFilters(t *testing.T) {
	occurred := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	svc := &stubService{requestPage: &logsvc.RequestLogPage{
		List: []domain.RequestLog{{
			ID:             101,
			TaskID:         42,
			InterfaceID:    11,
			Seq:            3,
			OccurredAt:     occurred,
			Method:         "POST",
			URL:            "http://10.0.0.1:8080/api/orders",
			RequestBody:    `{"page":1}`,
			ResponseStatus: 502,
			ResponseBody:   `{"msg":"bad gateway"}`,
			ErrorCode:      "1000",
			ErrorMessage:   "connection refused",
			DurationMs:     1234,
		}},
		Total: 1, Page: 2, PageSize: 50,
	}}
	w := httptest.NewRecorder()
	newLogRouter(svc).ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		"/api/v1/logs/request?task_id=42&interface_id=11&keyword=orders&"+
			"start_time=2026-10-28T00:00:00Z&end_time=2026-10-28T23:59:59Z&page=2&page_size=50", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}

	q := svc.lastRequestQuery
	if q.TaskID != 42 || q.InterfaceID != 11 || q.Keyword != "orders" || q.Page != 2 || q.PageSize != 50 {
		t.Fatalf("请求详情筛选未透传: %+v", q)
	}
	if q.StartTime == nil || q.EndTime == nil {
		t.Fatalf("时间范围未透传: %+v", q)
	}

	var body struct {
		Data struct {
			Total    int `json:"total"`
			Page     int `json:"page"`
			PageSize int `json:"page_size"`
			List     []struct {
				ID             string `json:"id"`
				TaskID         string `json:"task_id"`
				InterfaceID    string `json:"interface_id"`
				Seq            int64  `json:"seq"`
				OccurredAt     string `json:"occurred_at"`
				Method         string `json:"method"`
				URL            string `json:"url"`
				RequestBody    string `json:"request_body"`
				ResponseStatus int    `json:"response_status"`
				ResponseBody   string `json:"response_body"`
				ErrorCode      string `json:"error_code"`
				ErrorMessage   string `json:"error_message"`
				DurationMs     int64  `json:"duration_ms"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应失败: %v body=%s", err, w.Body.String())
	}
	if body.Data.Total != 1 || body.Data.Page != 2 || body.Data.PageSize != 50 || len(body.Data.List) != 1 {
		t.Fatalf("分页字段异常: %s", w.Body.String())
	}
	item := body.Data.List[0]
	if item.ID != "101" || item.TaskID != "42" || item.InterfaceID != "11" {
		t.Fatalf("ID 应为字符串: %+v", item)
	}
	if item.OccurredAt != "2026-10-28T10:00:00Z" {
		t.Fatalf("occurred_at 应为 UTC RFC3339, got %q", item.OccurredAt)
	}
	if item.Method != "POST" || item.ResponseStatus != 502 || item.ErrorCode != "1000" || item.DurationMs != 1234 {
		t.Fatalf("明细字段异常: %+v", item)
	}
	if item.RequestBody != `{"page":1}` || item.ResponseBody != `{"msg":"bad gateway"}` {
		t.Fatalf("入参/响应内容异常: %+v", item)
	}
}

// TestRequestLogInvalidParams ID 非数字/时间格式非法 → 10003；时间范围非法 → 25001。
func TestRequestLogInvalidParams(t *testing.T) {
	router := newLogRouter(&stubService{})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/logs/request?interface_id=abc", nil))
	if w.Code != http.StatusBadRequest || bodyCode(t, w) != 10003 {
		t.Fatalf("interface_id 非法 status/code = %d/%d", w.Code, bodyCode(t, w))
	}

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/logs/request?start_time=2026-10-28", nil))
	if w.Code != http.StatusBadRequest || bodyCode(t, w) != 10003 {
		t.Fatalf("时间格式非法 status/code = %d/%d", w.Code, bodyCode(t, w))
	}

	w = httptest.NewRecorder()
	newLogRouter(&stubService{err: logsvc.ErrInvalidTimeRange}).ServeHTTP(w,
		httptest.NewRequest(http.MethodGet,
			"/api/v1/logs/request?start_time=2026-10-29T00:00:00Z&end_time=2026-10-28T00:00:00Z", nil))
	if w.Code != http.StatusBadRequest || bodyCode(t, w) != 25001 {
		t.Fatalf("时间范围非法 status/code = %d/%d", w.Code, bodyCode(t, w))
	}
}

// TestGetLogSetting 读取日志设置：字段完整、更新时间 RFC3339。
func TestGetLogSetting(t *testing.T) {
	updated := time.Date(2026, 10, 28, 9, 30, 0, 0, time.UTC)
	svc := &stubService{setting: &domain.LogSetting{
		Enabled: true, MaxCount: 5000, RetentionDays: 7,
		LogTypes: []string{domain.LogTypeUser, domain.LogTypeRequest}, UpdatedAt: updated,
	}}

	w := httptest.NewRecorder()
	newLogRouter(svc).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/log-settings", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			Enabled       bool     `json:"enabled"`
			MaxCount      int64    `json:"max_count"`
			RetentionDays int      `json:"retention_days"`
			LogTypes      []string `json:"log_types"`
			UpdatedAt     string   `json:"updated_at"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if !body.Data.Enabled || body.Data.MaxCount != 5000 || body.Data.RetentionDays != 7 {
		t.Fatalf("设置字段异常: %+v", body.Data)
	}
	if len(body.Data.LogTypes) != 2 || body.Data.LogTypes[1] != domain.LogTypeRequest {
		t.Fatalf("日志类型异常: %v", body.Data.LogTypes)
	}
	if body.Data.UpdatedAt != "2026-10-28T09:30:00Z" {
		t.Fatalf("更新时间格式异常: %q", body.Data.UpdatedAt)
	}
}

// TestUpdateLogSetting 保存设置：请求体白名单字段透传服务层并回显结果。
func TestUpdateLogSetting(t *testing.T) {
	current := domain.LogSetting{Enabled: true, MaxCount: 100000, RetentionDays: 30,
		LogTypes: []string{domain.LogTypeUser, domain.LogTypeRequest}}
	svc := &stubService{setting: &current}

	w := httptest.NewRecorder()
	newLogRouter(svc).ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/v1/log-settings",
		strings.NewReader(`{"enabled":false,"max_count":2000,"retention_days":3,"log_types":["request"]}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	got := svc.lastSettingUpdate
	if got.Enabled || got.MaxCount != 2000 || got.RetentionDays != 3 {
		t.Fatalf("设置未透传: %+v", got)
	}
	if len(got.LogTypes) != 1 || got.LogTypes[0] != domain.LogTypeRequest {
		t.Fatalf("日志类型未透传: %v", got.LogTypes)
	}
}

// TestUpdateLogSettingPartial 部分字段更新：未提供的字段沿用当前设置（不清零）。
func TestUpdateLogSettingPartial(t *testing.T) {
	current := domain.LogSetting{Enabled: true, MaxCount: 100000, RetentionDays: 30,
		LogTypes: []string{domain.LogTypeUser, domain.LogTypeRequest}}
	svc := &stubService{setting: &current}

	w := httptest.NewRecorder()
	newLogRouter(svc).ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/v1/log-settings",
		strings.NewReader(`{"max_count":5000}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	got := svc.lastSettingUpdate
	if got.MaxCount != 5000 || got.RetentionDays != 30 || !got.Enabled || len(got.LogTypes) != 2 {
		t.Fatalf("部分更新应保留其余字段: %+v", got)
	}
}

// TestUpdateLogSettingInvalidBody 请求体非法 JSON → 10003；服务层校验失败原样映射。
func TestUpdateLogSettingInvalidBody(t *testing.T) {
	router := newLogRouter(&stubService{setting: &domain.LogSetting{Enabled: true, MaxCount: 100000, RetentionDays: 30,
		LogTypes: []string{domain.LogTypeRequest}}})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/v1/log-settings",
		strings.NewReader(`{"max_count":"abc"}`)))
	if w.Code != http.StatusBadRequest || bodyCode(t, w) != 10003 {
		t.Fatalf("非法请求体 status/code = %d/%d", w.Code, bodyCode(t, w))
	}

	// 服务层校验失败（如 max_count 越界）→ 10003。
	w = httptest.NewRecorder()
	newLogRouter(&stubService{
		setting: &domain.LogSetting{Enabled: true, MaxCount: 100000, RetentionDays: 30,
			LogTypes: []string{domain.LogTypeRequest}},
		updateErr: errcode.ErrParamFmt,
	}).ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/api/v1/log-settings",
		strings.NewReader(`{"max_count":10}`)))
	if w.Code != http.StatusBadRequest || bodyCode(t, w) != 10003 {
		t.Fatalf("服务层校验失败 status/code = %d/%d", w.Code, bodyCode(t, w))
	}
}
