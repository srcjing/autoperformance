package task

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/auth"
	"auto_performance/internal/domain"
	testsvc "auto_performance/internal/service/testing"
)

// stubService 测试管理服务桩：只实现被测方法，其余由嵌入接口兜底。
type stubService struct {
	Service

	created     *testsvc.CreateTaskInput
	task        *testsvc.TaskView
	templates   *testsvc.TemplatePage
	listQuery   *testsvc.ListTasksQuery
	cleanupCall int
}

func (s *stubService) Create(_ context.Context, in testsvc.CreateTaskInput) (*testsvc.TaskView, error) {
	s.created = &in
	if s.task != nil {
		return s.task, nil
	}
	return &testsvc.TaskView{ID: 1, Name: in.Name, Status: domain.TaskPending}, nil
}

func (s *stubService) Get(_ context.Context, _ int64) (*testsvc.TaskView, error) {
	return s.task, nil
}

func (s *stubService) List(_ context.Context, q testsvc.ListTasksQuery) (*testsvc.TaskPage, error) {
	s.listQuery = &q
	return &testsvc.TaskPage{List: []testsvc.TaskView{*s.task}, Total: 1, Page: q.Page, PageSize: q.PageSize}, nil
}

func (s *stubService) ListTemplates(_ context.Context, q testsvc.ListTemplatesQuery) (*testsvc.TemplatePage, error) {
	if s.templates == nil {
		return &testsvc.TemplatePage{Page: q.Page, PageSize: q.PageSize}, nil
	}
	return s.templates, nil
}

func (s *stubService) Cleanup(_ context.Context, _ int64) (*testsvc.TaskView, error) {
	s.cleanupCall++
	return s.task, nil
}

func newTaskRouter(svc Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := New(svc)
	r.GET("/api/v1/templates", h.ListTemplates)
	r.POST("/api/v1/tasks", func(c *gin.Context) {
		c.Set(auth.CtxAdminID, "7")
		h.CreateTask(c)
	})
	r.GET("/api/v1/tasks", h.ListTasks)
	r.GET("/api/v1/tasks/:id", h.GetTask)
	r.POST("/api/v1/tasks/:id/cleanup", h.CleanupTask)
	return r
}

// TestTaskDTOHidesAuthHeadersCiphertext 任务详情绝不输出认证头密文（INV-7）。
func TestTaskDTOHidesAuthHeadersCiphertext(t *testing.T) {
	now := time.Now()
	svc := &stubService{task: &testsvc.TaskView{
		ID:         9,
		Name:       "任务",
		EnvID:      3,
		TargetHost: "10.20.30.40",
		Status:     domain.TaskRunning,
		InterfaceSnapshots: []domain.InterfaceSnapshot{{
			InterfaceID:    11,
			ProductID:      4,
			Name:           "查询订单",
			Method:         "GET",
			Scheme:         domain.SchemeHTTPS,
			Port:           8443,
			Path:           "/orders",
			URL:            "https://10.20.30.40:8443/orders",
			Headers:        map[string]string{"X-Trace-Id": "abc"},
			AuthHeadersEnc: []byte("super-cipher-text"),
			Status:         domain.InterfaceStatusEnabled,
			CapturedAt:     now,
		}},
		ThresholdSnapshots: []domain.ThresholdSnapshot{{InterfaceID: 11, Metric: "p95", Operator: "le", Value: 500, Enabled: true}},
		Flow:               json.RawMessage(`{"concurrency_mode":"fixed"}`),
		CreatedAt:          now,
		UpdatedAt:          now,
	}}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tasks/9", nil)
	newTaskRouter(svc).ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, "super-cipher-text") || strings.Contains(body, "auth_headers_enc") {
		t.Fatalf("响应泄露认证头密文: %s", body)
	}
	var resp struct {
		Data struct {
			ID         string `json:"id"`
			Status     string `json:"status"`
			TargetHost string `json:"target_host"`
			Snapshot   []struct {
				InterfaceID string `json:"interface_id"`
				Scheme      string `json:"scheme"`
				Port        int    `json:"port"`
				Path        string `json:"path"`
				URL         string `json:"url"`
			} `json:"interface_range_snapshot"`
			Thresholds []struct {
				InterfaceID string  `json:"interface_id"`
				Value       float64 `json:"value"`
			} `json:"threshold_snapshot"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp.Data.ID != "9" || resp.Data.Status != "running" || resp.Data.TargetHost != "10.20.30.40" {
		t.Fatalf("任务响应异常: %+v", resp.Data)
	}
	if len(resp.Data.Snapshot) != 1 || resp.Data.Snapshot[0].InterfaceID != "11" {
		t.Fatalf("快照 ID 应字符串化: %+v", resp.Data.Snapshot)
	}
	snap := resp.Data.Snapshot[0]
	if snap.Scheme != "https" || snap.Port != 8443 || snap.Path != "/orders" {
		t.Fatalf("快照应回传 scheme/port/path: %+v", snap)
	}
	if snap.URL != "https://10.20.30.40:8443/orders" {
		t.Fatalf("快照 url 应为完整压测地址: %q", snap.URL)
	}
	if len(resp.Data.Thresholds) != 1 || resp.Data.Thresholds[0].Value != 500 {
		t.Fatalf("阈值快照异常: %+v", resp.Data.Thresholds)
	}
}

// TestCreateTaskBindsIDsAndOperator 请求体兼容字符串/数字 ID，操作人取自 AuthN 上下文。
func TestCreateTaskBindsIDsAndOperator(t *testing.T) {
	svc := &stubService{}
	router := newTaskRouter(svc)

	body := `{
		"name":"峰值压测",
		"environment_id":"3",
		"target_host":"10.20.30.40",
		"interface_ids":["11",12],
		"product_ids":[4],
		"flow":{"concurrency_mode":"fixed","target_vus":100,"duration":"60s"},
		"report_config":{"formats":["html"]},
		"clean_config":{"actions":["clean_data"]}
	}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	if svc.created == nil {
		t.Fatal("服务未被调用")
	}
	if svc.created.EnvID != 3 || svc.created.CreatedBy != 7 || svc.created.TargetHost != "10.20.30.40" {
		t.Fatalf("环境/目标主机/操作人解析异常: %+v", svc.created)
	}
	if len(svc.created.InterfaceIDs) != 2 || svc.created.InterfaceIDs[1] != 12 {
		t.Fatalf("接口范围解析异常: %+v", svc.created.InterfaceIDs)
	}

	// 非法 ID → 10003。
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/tasks", strings.NewReader(`{"name":"x","environment_id":"abc"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 ID 应 400, got %d", w.Code)
	}
}

// TestListTasksParsesFilters 列表筛选参数（状态/环境/产品/时间/分页）。
func TestListTasksParsesFilters(t *testing.T) {
	svc := &stubService{task: &testsvc.TaskView{ID: 1, Name: "任务", Status: domain.TaskPending}}
	router := newTaskRouter(svc)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/tasks?keyword=peak&status=pending&env_id=2&product_id=4&page=2&page_size=50&created_from=2026-10-01T00:00:00Z", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", w.Code, w.Body.String())
	}
	q := svc.listQuery
	if q == nil || q.Keyword != "peak" || q.Status != "pending" || q.EnvID != 2 || q.ProductID != 4 ||
		q.Page != 2 || q.PageSize != 50 || q.CreatedFrom == nil {
		t.Fatalf("查询参数解析异常: %+v", q)
	}

}

// TestCleanupTaskRequiresID 路径 ID 非法 → 10003。
func TestCleanupTaskRequiresID(t *testing.T) {
	svc := &stubService{task: &testsvc.TaskView{ID: 1, Status: domain.TaskCleaning}}
	router := newTaskRouter(svc)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/v1/tasks/abc/cleanup", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 ID 应 400, got %d", w.Code)
	}
	if svc.cleanupCall != 0 {
		t.Fatal("非法 ID 不得调用服务")
	}
}
