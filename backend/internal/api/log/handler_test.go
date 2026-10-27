package log

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/domain"
	logsvc "auto_performance/internal/service/log"
)

// stubService 日志服务桩：记录收到的查询条件，返回预置分页结果。
type stubService struct {
	Service

	userPage   *logsvc.UserLogPage
	systemPage *logsvc.SystemLogPage
	testPage   *logsvc.TestLogPage
	err        error

	lastUserQuery   domain.UserLogQuery
	lastSystemQuery domain.SystemLogQuery
	lastTestQuery   domain.TestLogQuery
}

func (s *stubService) ListUserLogs(_ context.Context, q domain.UserLogQuery) (*logsvc.UserLogPage, error) {
	s.lastUserQuery = q
	if s.err != nil {
		return nil, s.err
	}
	return s.userPage, nil
}

func (s *stubService) ListSystemLogs(_ context.Context, q domain.SystemLogQuery) (*logsvc.SystemLogPage, error) {
	s.lastSystemQuery = q
	if s.err != nil {
		return nil, s.err
	}
	return s.systemPage, nil
}

func (s *stubService) ListTestLogs(_ context.Context, q domain.TestLogQuery) (*logsvc.TestLogPage, error) {
	s.lastTestQuery = q
	if s.err != nil {
		return nil, s.err
	}
	return s.testPage, nil
}

// newLogRouter 构造被测路由（无鉴权中间件，专注 handler 行为）。
func newLogRouter(svc Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := New(svc)
	r.GET("/api/v1/logs/user", h.ListUserLogs)
	r.GET("/api/v1/logs/system", h.ListSystemLogs)
	r.GET("/api/v1/logs/test", h.ListTestLogs)
	return r
}

// bodyCode 解析统一响应中的业务码。
func bodyCode(t *testing.T, w *httptest.ResponseRecorder) int {
	t.Helper()
	var body struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应失败: %v body=%s", err, w.Body.String())
	}
	return body.Code
}

// TestUserLogDTO 用户日志 DTO：ID 字符串化、level 由 result 推导、操作人登录名下发。
func TestUserLogDTO(t *testing.T) {
	now := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	adminID := int64(7)
	svc := &stubService{userPage: &logsvc.UserLogPage{
		List: []domain.AuditLog{{
			ID: 11, AdminID: &adminID, AdminUsername: "alice", Action: "create",
			ResourceType: "environment", ResourceID: "12", Detail: "POST /api/v1/environments status=200",
			IP: "10.0.0.8", Result: domain.AuditResultFail, CreatedAt: now,
		}},
		Total: 1, Page: 1, PageSize: 20,
	}}

	w := httptest.NewRecorder()
	newLogRouter(svc).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/logs/user", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}

	var body struct {
		Data struct {
			Total    int `json:"total"`
			Page     int `json:"page"`
			PageSize int `json:"page_size"`
			List     []struct {
				ID            string `json:"id"`
				AdminID       string `json:"admin_id"`
				AdminUsername string `json:"admin_username"`
				Action        string `json:"action"`
				Result        string `json:"result"`
				Level         string `json:"level"`
				CreatedAt     string `json:"created_at"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应失败: %v body=%s", err, w.Body.String())
	}
	if body.Data.Total != 1 || body.Data.Page != 1 || body.Data.PageSize != 20 || len(body.Data.List) != 1 {
		t.Fatalf("分页字段异常: %s", w.Body.String())
	}
	item := body.Data.List[0]
	if item.ID != "11" || item.AdminID != "7" {
		t.Fatalf("ID 应为字符串: id=%s admin_id=%s", item.ID, item.AdminID)
	}
	if item.AdminUsername != "alice" || item.Action != "create" {
		t.Fatalf("操作人/动作字段异常: %+v", item)
	}
	if item.Level != domain.LogLevelError {
		t.Fatalf("fail 结果应映射 level=error, got %q", item.Level)
	}
	if item.CreatedAt != "2026-10-26T10:00:00Z" {
		t.Fatalf("时间应为 UTC RFC3339, got %q", item.CreatedAt)
	}
}

// TestLogQueryPassthrough 校验 query 参数透传（级别/关键字/时间/关联对象/分页）。
func TestLogQueryPassthrough(t *testing.T) {
	svc := &stubService{
		userPage:   &logsvc.UserLogPage{Page: 2, PageSize: 50},
		systemPage: &logsvc.SystemLogPage{Page: 1, PageSize: 20},
		testPage:   &logsvc.TestLogPage{Page: 1, PageSize: 20},
	}
	router := newLogRouter(svc)

	// 用户日志：动作/资源/操作人/级别/关键字/时间范围/分页。
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		"/api/v1/logs/user?action=create&resource_type=environment&resource_id=12&admin_id=7"+
			"&level=error&keyword=env&start_time=2026-10-01T00:00:00Z&end_time=2026-10-31T23:59:59Z&page=2&page_size=50", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	uq := svc.lastUserQuery
	if uq.Action != "create" || uq.ResourceType != "environment" || uq.ResourceID != "12" || uq.AdminID != 7 {
		t.Fatalf("用户日志筛选未透传: %+v", uq)
	}
	if uq.Level != "error" || uq.Keyword != "env" || uq.Page != 2 || uq.PageSize != 50 {
		t.Fatalf("用户日志级别/关键字/分页未透传: %+v", uq)
	}
	if uq.StartTime == nil || uq.EndTime == nil || !uq.StartTime.Before(*uq.EndTime) {
		t.Fatalf("用户日志时间范围未透传: %+v", uq)
	}

	// 系统日志：事件筛选。
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/logs/system?event=restart&level=warn", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if svc.lastSystemQuery.Event != "restart" || svc.lastSystemQuery.Level != "warn" {
		t.Fatalf("系统日志筛选未透传: %+v", svc.lastSystemQuery)
	}

	// 测试日志：task_id 定位。
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/logs/test?task_id=9&level=error", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if svc.lastTestQuery.TaskID != 9 || svc.lastTestQuery.Level != "error" {
		t.Fatalf("测试日志筛选未透传: %+v", svc.lastTestQuery)
	}
}

// TestLogInvalidTimeParam 时间格式非法 → 10003（参数格式非法），不进入服务层。
func TestLogInvalidTimeParam(t *testing.T) {
	svc := &stubService{}
	w := httptest.NewRecorder()
	newLogRouter(svc).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/logs/user?start_time=2026-10-01", nil))

	if w.Code != http.StatusBadRequest || bodyCode(t, w) != 10003 {
		t.Fatalf("status/code = %d/%d, want 400/10003", w.Code, bodyCode(t, w))
	}
	if svc.lastUserQuery.Level != "" || svc.lastUserQuery.Page != 0 {
		t.Fatalf("非法参数不应进入服务层: %+v", svc.lastUserQuery)
	}
}

// TestLogInvalidTimeRangeMapsTo25001 时间范围非法 → 25001（服务层校验，HTTP 400）。
func TestLogInvalidTimeRangeMapsTo25001(t *testing.T) {
	svc := &stubService{err: logsvc.ErrInvalidTimeRange}
	w := httptest.NewRecorder()
	newLogRouter(svc).ServeHTTP(w, httptest.NewRequest(http.MethodGet,
		"/api/v1/logs/test?start_time=2026-10-31T00:00:00Z&end_time=2026-10-01T00:00:00Z", nil))

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if code := bodyCode(t, w); code != 25001 {
		t.Fatalf("code = %d, want 25001", code)
	}
}

// TestLogInvalidIDParam 非数字 admin_id/task_id → 10003。
func TestLogInvalidIDParam(t *testing.T) {
	router := newLogRouter(&stubService{})
	for _, path := range []string{"/api/v1/logs/user?admin_id=abc", "/api/v1/logs/test?task_id=abc"} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		if w.Code != http.StatusBadRequest || bodyCode(t, w) != 10003 {
			t.Fatalf("%s status/code = %d/%d, want 400/10003", path, w.Code, bodyCode(t, w))
		}
	}
}

// TestSystemAndTestLogDTO 系统/测试日志 DTO 字段与时间格式。
func TestSystemAndTestLogDTO(t *testing.T) {
	now := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	svc := &stubService{
		systemPage: &logsvc.SystemLogPage{
			List:  []domain.SystemLog{{ID: 3, Level: domain.LogLevelWarn, Event: domain.SystemEventRestart, Message: "重启", CreatedAt: now}},
			Total: 1, Page: 1, PageSize: 20,
		},
		testPage: &logsvc.TestLogPage{
			List:  []domain.TestLog{{ID: 5, TaskID: 9, Seq: 2, Level: domain.LogLevelError, Message: "timeout", CreatedAt: now}},
			Total: 1, Page: 1, PageSize: 20,
		},
	}
	router := newLogRouter(svc)

	type logList struct {
		Data struct {
			List []map[string]any `json:"list"`
		} `json:"data"`
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/logs/system", nil))
	var sys logList
	if err := json.Unmarshal(w.Body.Bytes(), &sys); err != nil {
		t.Fatalf("解析系统日志响应失败: %v", err)
	}
	if len(sys.Data.List) != 1 {
		t.Fatalf("系统日志为空: %s", w.Body.String())
	}
	if sys.Data.List[0]["id"] != "3" || sys.Data.List[0]["event"] != "restart" || sys.Data.List[0]["level"] != "warn" {
		t.Fatalf("系统日志 DTO 异常: %+v", sys.Data.List[0])
	}

	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/logs/test?task_id=9", nil))
	var tst logList
	if err := json.Unmarshal(w.Body.Bytes(), &tst); err != nil {
		t.Fatalf("解析测试日志响应失败: %v", err)
	}
	if len(tst.Data.List) != 1 {
		t.Fatalf("测试日志为空: %s", w.Body.String())
	}
	item := tst.Data.List[0]
	if item["id"] != "5" || item["task_id"] != "9" || item["seq"] != float64(2) || item["level"] != "error" {
		t.Fatalf("测试日志 DTO 异常: %+v", item)
	}
}
