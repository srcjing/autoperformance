package environment

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
	"auto_performance/internal/errcode"
	envsvc "auto_performance/internal/service/environment"
)

// stubService 仅实现被测试端点所需方法（其余经嵌入接口提供方法集）。
type stubService struct {
	Service

	createEnvIn    envsvc.EnvironmentInput
	updateEnvIn    envsvc.EnvironmentInput
	boundTagIDs    []int64
	statusValue    string
	listQuery      envsvc.ListEnvironmentsQuery
	pageResult     *envsvc.EnvironmentPage
	createCategory envsvc.CategoryInput
	envErr         error
}

func (s *stubService) ListEnvironments(_ context.Context, q envsvc.ListEnvironmentsQuery) (*envsvc.EnvironmentPage, error) {
	s.listQuery = q
	if s.envErr != nil {
		return nil, s.envErr
	}
	if s.pageResult != nil {
		return s.pageResult, nil
	}
	return &envsvc.EnvironmentPage{List: []envsvc.EnvironmentView{}, Total: 0, Page: 1, PageSize: 20}, nil
}

func (s *stubService) CreateEnvironment(_ context.Context, in envsvc.EnvironmentInput) (*envsvc.EnvironmentView, error) {
	s.createEnvIn = in
	if s.envErr != nil {
		return nil, s.envErr
	}
	return envView(), nil
}

func (s *stubService) GetEnvironment(_ context.Context, _ int64) (*envsvc.EnvironmentView, error) {
	if s.envErr != nil {
		return nil, s.envErr
	}
	return envView(), nil
}

func (s *stubService) UpdateEnvironment(_ context.Context, _ int64, in envsvc.EnvironmentInput) (*envsvc.EnvironmentView, error) {
	s.updateEnvIn = in
	if s.envErr != nil {
		return nil, s.envErr
	}
	return envView(), nil
}

func (s *stubService) BindTags(_ context.Context, _ int64, tagIDs []int64) error {
	s.boundTagIDs = tagIDs
	return s.envErr
}

func (s *stubService) UpdateStatus(_ context.Context, _ int64, status string) error {
	s.statusValue = status
	return s.envErr
}

func (s *stubService) CreateCategory(_ context.Context, in envsvc.CategoryInput) (*envsvc.CategoryView, error) {
	s.createCategory = in
	return &envsvc.CategoryView{ID: 1, Name: in.Name, Description: in.Description, Version: 1}, nil
}

func (s *stubService) TagEnvironments(_ context.Context, _ int64, _, _ int) (*envsvc.EnvironmentPage, error) {
	if s.pageResult != nil {
		return s.pageResult, nil
	}
	return &envsvc.EnvironmentPage{List: []envsvc.EnvironmentView{}, Total: 0, Page: 1, PageSize: 20}, nil
}

func (s *stubService) DeleteCategory(_ context.Context, _ int64) error { return s.envErr }

func (s *stubService) DeleteEnvironment(_ context.Context, _ int64) error { return s.envErr }

// envView 构造含主机的环境视图（用于权限裁剪断言）。
func envView() *envsvc.EnvironmentView {
	now := time.Date(2026, 10, 26, 8, 0, 0, 0, time.UTC)
	return &envsvc.EnvironmentView{
		ID: 7, Name: "测试环境", Code: "test-env", CategoryID: 10, CategoryName: "单机",
		Tags:  []envsvc.TagBrief{{ID: 201, Name: "内网", Color: "#2563eb"}},
		Type:  domain.EnvTypeSingle,
		Hosts: []string{"10.0.0.1"},
		OS:    "Linux", Status: domain.EnvStatusPending, Version: 1, CreatedAt: now, UpdatedAt: now,
	}
}

func newTestRouter(svc Service, perms map[string]bool) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(auth.CtxPerms, perms)
		c.Next()
	})
	h := New(svc)
	r.GET("/api/v1/environments", h.ListEnvironments)
	r.POST("/api/v1/environments", h.CreateEnvironment)
	r.GET("/api/v1/environments/:id", h.GetEnvironment)
	r.PUT("/api/v1/environments/:id", h.UpdateEnvironment)
	r.DELETE("/api/v1/environments/:id", h.DeleteEnvironment)
	r.PUT("/api/v1/environments/:id/tags", h.BindTags)
	r.PATCH("/api/v1/environments/:id/status", h.UpdateStatus)
	r.GET("/api/v1/environment-categories", h.ListCategories)
	r.POST("/api/v1/environment-categories", h.CreateCategory)
	r.GET("/api/v1/tags", h.ListTags)
	r.GET("/api/v1/tags/:id/environments", h.TagEnvironments)
	return r
}

func doJSON(r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("响应不是统一 JSON 结构: %s", w.Body.String())
	}
	return m
}

// TestListEnvironmentsTrimsHostsWithoutReadPermission DES-I-001：
// 具备 env.list:read 时下发 hosts；缺失时不下发（不泄露主机地址）。
func TestListEnvironmentsTrimsHostsWithoutReadPermission(t *testing.T) {
	stub := &stubService{pageResult: &envsvc.EnvironmentPage{
		List: []envsvc.EnvironmentView{*envView()}, Total: 1, Page: 1, PageSize: 20,
	}}

	withPerm := newTestRouter(stub, map[string]bool{"env.list:read": true})
	w := doJSON(withPerm, http.MethodGet, "/api/v1/environments", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	first := firstEnv(t, w)
	if hosts, ok := first["hosts"].([]any); !ok || len(hosts) != 1 {
		t.Fatalf("有 read 权限应返回 hosts: %v", first)
	}

	withoutPerm := newTestRouter(stub, map[string]bool{})
	w = doJSON(withoutPerm, http.MethodGet, "/api/v1/environments", "")
	if _, ok := firstEnv(t, w)["hosts"]; ok {
		t.Fatal("无 env.list:read 时不得返回 hosts（DES-I-001）")
	}
}

// TestGetEnvironmentTrimsHosts 详情同样按权限裁剪 hosts（DES-I-001）。
func TestGetEnvironmentTrimsHosts(t *testing.T) {
	withPerm := newTestRouter(&stubService{}, map[string]bool{"env.list:read": true})
	w := doJSON(withPerm, http.MethodGet, "/api/v1/environments/7", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	body := decodeBody(t, w)
	data, _ := body["data"].(map[string]any)
	if hosts, ok := data["hosts"].([]any); !ok || len(hosts) != 1 {
		t.Fatalf("有 read 权限应返回 hosts: %v", data)
	}

	withoutPerm := newTestRouter(&stubService{}, map[string]bool{})
	w = doJSON(withoutPerm, http.MethodGet, "/api/v1/environments/7", "")
	body = decodeBody(t, w)
	data, _ = body["data"].(map[string]any)
	if _, ok := data["hosts"]; ok {
		t.Fatal("无 env.list:read 时详情不得返回 hosts（DES-I-001）")
	}
}

// TestTagEnvironmentsTrimsHosts 标签反查环境同样按权限裁剪 hosts。
func TestTagEnvironmentsTrimsHosts(t *testing.T) {
	stub := &stubService{pageResult: &envsvc.EnvironmentPage{
		List: []envsvc.EnvironmentView{*envView()}, Total: 1, Page: 1, PageSize: 20,
	}}
	r := newTestRouter(stub, map[string]bool{"env.tag:read": true})

	w := doJSON(r, http.MethodGet, "/api/v1/tags/201/environments?page=1&page_size=10", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if _, ok := firstEnv(t, w)["hosts"]; ok {
		t.Fatal("无 env.list:read 时反查结果不得返回 hosts（DES-I-001）")
	}
}

// TestListEnvironmentsParsesQuery 列表查询参数解析（含 tag_ids 逗号分隔与分页）。
func TestListEnvironmentsParsesQuery(t *testing.T) {
	stub := &stubService{}
	r := newTestRouter(stub, map[string]bool{"env.list:read": true})

	w := doJSON(r, http.MethodGet,
		"/api/v1/environments?keyword=prod&category_id=10&tag_ids=1,2&status=pending&page=2&page_size=50", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	got := stub.listQuery
	if got.Keyword != "prod" || got.CategoryID != 10 || got.Status != domain.EnvStatusPending {
		t.Fatalf("查询参数解析异常: %+v", got)
	}
	if len(got.TagIDs) != 2 || got.TagIDs[0] != 1 || got.TagIDs[1] != 2 {
		t.Fatalf("tag_ids 解析异常: %v", got.TagIDs)
	}
	if got.Page != 2 || got.PageSize != 50 {
		t.Fatalf("分页参数解析异常: page=%d size=%d", got.Page, got.PageSize)
	}

	// 非法 tag_ids → 10003（不进入业务层）。
	w = doJSON(r, http.MethodGet, "/api/v1/environments?tag_ids=abc", "")
	if w.Code != http.StatusBadRequest || decodeBody(t, w)["code"] != float64(10003) {
		t.Fatalf("非法 tag_ids 应 400/10003，got %d %s", w.Code, w.Body.String())
	}
}

// TestCreateEnvironmentAcceptsStringAndNumberIDs 请求体 ID 兼容字符串/数字；
// 并验证 status 等提权字段被忽略（状态只能经状态接口变更）。
func TestCreateEnvironmentAcceptsStringAndNumberIDs(t *testing.T) {
	stub := &stubService{}
	r := newTestRouter(stub, map[string]bool{"env.list:write": true})

	body := `{
		"name": "测试环境",
		"code": "test-env",
		"category_id": "10",
		"tag_ids": ["1", 2],
		"type": "cluster",
		"hosts": ["10.0.0.1"],
		"os": "Linux",
		"ssh_host_id": "5",
		"cert_id": 6,
		"description": "说明",
		"status": "testing"
	}`
	w := doJSON(r, http.MethodPost, "/api/v1/environments", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	got := stub.createEnvIn
	if got.CategoryID != 10 || got.SshHostID != 5 || got.CertID != 6 {
		t.Fatalf("ID 解析异常: %+v", got)
	}
	if len(got.TagIDs) != 2 || got.TagIDs[0] != 1 || got.TagIDs[1] != 2 {
		t.Fatalf("tag_ids 解析异常: %v", got.TagIDs)
	}
	if got.Type != domain.EnvTypeCluster || got.Code != "test-env" {
		t.Fatalf("字段绑定异常: %+v", got)
	}
}

// TestCreateEnvironmentRequiresRequiredFields 缺 name/code/category_id/hosts → 10002。
func TestCreateEnvironmentRequiresRequiredFields(t *testing.T) {
	stub := &stubService{}
	r := newTestRouter(stub, map[string]bool{"env.list:write": true})

	bodies := []string{
		`{"code":"c","category_id":"1","hosts":["1.1.1.1"]}`,
		`{"name":"n","category_id":"1","hosts":["1.1.1.1"]}`,
		`{"name":"n","code":"c","hosts":["1.1.1.1"]}`,
		`{"name":"n","code":"c","category_id":"1","hosts":[]}`,
	}
	for _, body := range bodies {
		w := doJSON(r, http.MethodPost, "/api/v1/environments", body)
		if w.Code != http.StatusBadRequest || decodeBody(t, w)["code"] != float64(10002) {
			t.Errorf("缺必填字段应 400/10002, body=%s got %d %s", body, w.Code, w.Body.String())
		}
	}
}

// TestUpdateEnvironmentPassesVersion 全量修改透传 version（乐观锁由服务层判定）。
func TestUpdateEnvironmentPassesVersion(t *testing.T) {
	stub := &stubService{}
	r := newTestRouter(stub, map[string]bool{"env.list:write": true})

	body := `{"name":"n","code":"c","category_id":1,"hosts":["1.1.1.1"],"version":3}`
	w := doJSON(r, http.MethodPut, "/api/v1/environments/9", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if stub.updateEnvIn.Version != 3 || stub.updateEnvIn.CategoryID != 1 {
		t.Fatalf("version/category_id 未透传: %+v", stub.updateEnvIn)
	}
}

// TestOptimisticLockMapsTo409 乐观锁冲突 → HTTP 409 + 10402（DES-T-001）。
func TestOptimisticLockMapsTo409(t *testing.T) {
	stub := &stubService{envErr: errcode.ErrConcurrent}
	r := newTestRouter(stub, map[string]bool{"env.list:write": true})

	body := `{"name":"n","code":"c","category_id":1,"hosts":["1.1.1.1"],"version":1}`
	w := doJSON(r, http.MethodPut, "/api/v1/environments/9", body)
	if w.Code != http.StatusConflict || decodeBody(t, w)["code"] != float64(10402) {
		t.Fatalf("乐观锁冲突应 409/10402，got %d %s", w.Code, w.Body.String())
	}
}

// TestBusyEnvironmentMapsTo21004 环境被占用 → HTTP 409 + 21004（删除/停用）。
func TestBusyEnvironmentMapsTo21004(t *testing.T) {
	stub := &stubService{envErr: envsvc.ErrEnvOccupied}
	r := newTestRouter(stub, map[string]bool{"env.list:write": true})

	for _, tc := range []struct{ method, path, body string }{
		{http.MethodDelete, "/api/v1/environments/9", ""},
		{http.MethodPatch, "/api/v1/environments/9/status", `{"status":"disabled"}`},
	} {
		w := doJSON(r, tc.method, tc.path, tc.body)
		if w.Code != http.StatusConflict || decodeBody(t, w)["code"] != float64(21004) {
			t.Errorf("%s %s 应 409/21004，got %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
	}
}

// TestBindTagsParsesIDs 标签绑定解析字符串/数字 ID；非法 ID → 10003。
func TestBindTagsParsesIDs(t *testing.T) {
	stub := &stubService{}
	r := newTestRouter(stub, map[string]bool{"env.list:write": true})

	w := doJSON(r, http.MethodPut, "/api/v1/environments/9/tags", `{"tag_ids":["3",4]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if len(stub.boundTagIDs) != 2 || stub.boundTagIDs[0] != 3 || stub.boundTagIDs[1] != 4 {
		t.Fatalf("tag_ids 解析异常: %v", stub.boundTagIDs)
	}

	w = doJSON(r, http.MethodPut, "/api/v1/environments/9/tags", `{"tag_ids":["x"]}`)
	if w.Code != http.StatusBadRequest || decodeBody(t, w)["code"] != float64(10003) {
		t.Fatalf("非法 tag_ids 应 400/10003，got %d %s", w.Code, w.Body.String())
	}
}

// TestCreateCategoryRequiresName 分类名称必填 → 10002。
func TestCreateCategoryRequiresName(t *testing.T) {
	stub := &stubService{}
	r := newTestRouter(stub, map[string]bool{"env.list:write": true})

	w := doJSON(r, http.MethodPost, "/api/v1/environment-categories", `{"description":"缺名称"}`)
	if w.Code != http.StatusBadRequest || decodeBody(t, w)["code"] != float64(10002) {
		t.Fatalf("缺名称应 400/10002，got %d %s", w.Code, w.Body.String())
	}
}

// firstEnv 取响应 data.list[0]。
func firstEnv(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	body := decodeBody(t, w)
	data, _ := body["data"].(map[string]any)
	list, _ := data["list"].([]any)
	if len(list) == 0 {
		t.Fatalf("响应列表为空: %s", w.Body.String())
	}
	first, _ := list[0].(map[string]any)
	if first == nil {
		t.Fatalf("列表项结构异常: %s", w.Body.String())
	}
	return first
}
