package setting

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/auth"
	"auto_performance/internal/domain"
	settingsvc "auto_performance/internal/service/setting"
)

// stubService 仅实现被测试端点所需方法（其余通过嵌入接口提供方法集）。
type stubService struct {
	Service

	createAdminIn  settingsvc.CreateAdminInput
	createAdminOut *settingsvc.AdminView
	boundRoleIDs   []int64
	roleIn         settingsvc.RoleInput
}

func (s *stubService) CreateAdmin(_ context.Context, in settingsvc.CreateAdminInput) (*settingsvc.AdminView, error) {
	s.createAdminIn = in
	if s.createAdminOut != nil {
		return s.createAdminOut, nil
	}
	return &settingsvc.AdminView{
		ID: 9, Username: in.Username, Name: in.Name, Phone: in.Phone, Email: in.Email,
		Description: in.Description, Status: domain.AdminStatusEnabled,
		IsBuiltin: false, MustChangePassword: true, Version: 1,
	}, nil
}

func (s *stubService) BindAdminRoles(_ context.Context, _ int64, roleIDs []int64) error {
	s.boundRoleIDs = roleIDs
	return nil
}

func (s *stubService) CreateRole(_ context.Context, in settingsvc.RoleInput) (*settingsvc.RoleView, error) {
	s.roleIn = in
	return &settingsvc.RoleView{ID: 3, Name: in.Name, Permissions: in.Permissions, Version: 1}, nil
}

func (s *stubService) PermissionModules() []auth.PermissionModule {
	return auth.PermissionModules()
}

func newTestRouter(svc Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := New(svc)
	r.POST("/api/v1/admins", h.CreateAdmin)
	r.GET("/api/v1/admins/:id", h.GetAdmin)
	r.POST("/api/v1/admins/:id/roles", h.BindAdminRoles)
	r.POST("/api/v1/roles", h.CreateRole)
	r.GET("/api/v1/permissions", h.ListPermissions)
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

// TestCreateAdminDropsPrivilegedFields 端到端验证防 Mass Assignment（DES-E-002）：
// 请求体夹带 role/status/is_super/role_ids/password_hash 等字段被丢弃，不进入业务输入。
func TestCreateAdminDropsPrivilegedFields(t *testing.T) {
	stub := &stubService{}
	r := newTestRouter(stub)

	body := `{
		"username": "operator",
		"name": "测试操作员",
		"phone": "13800000000",
		"email": "op@example.com",
		"description": "新建账号",
		"password": "Operator@123",
		"role": "super",
		"role_ids": ["1"],
		"status": "enabled",
		"is_super": true,
		"is_builtin": true,
		"must_change_password": false,
		"password_hash": "$2a$10$attacker"
	}`
	w := doJSON(r, http.MethodPost, "/api/v1/admins", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}

	got := stub.createAdminIn
	if got.Username != "operator" || got.Name != "测试操作员" || got.Password != "Operator@123" {
		t.Fatalf("白名单字段未正确绑定: %+v", got)
	}
	// 业务输入结构不含任何提权字段：此处断言绑定结果与白名单字段完全一致。
	want := settingsvc.CreateAdminInput{
		Username: "operator", Name: "测试操作员", Phone: "13800000000",
		Email: "op@example.com", Description: "新建账号", Password: "Operator@123",
	}
	if got != want {
		t.Fatalf("CreateAdminInput = %+v, want %+v", got, want)
	}

	// 响应不得包含口令哈希。
	resp := decodeBody(t, w)
	data, _ := resp["data"].(map[string]any)
	if _, ok := data["password_hash"]; ok {
		t.Fatal("响应不应包含 password_hash")
	}
	if data["is_builtin"] != false {
		t.Fatalf("新建账号不得为内置账号: %v", data["is_builtin"])
	}
}

// TestCreateAdminRequiresWhitelistFields 缺少必填字段 → 10002。
func TestCreateAdminRequiresWhitelistFields(t *testing.T) {
	r := newTestRouter(&stubService{})
	w := doJSON(r, http.MethodPost, "/api/v1/admins", `{"username":"x"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if code := decodeBody(t, w)["code"]; code != float64(10002) {
		t.Fatalf("code = %v, want 10002", code)
	}
}

// TestBindRolesAcceptsStringAndNumberIDs role_ids 兼容字符串/数字，且按序下发。
func TestBindRolesAcceptsStringAndNumberIDs(t *testing.T) {
	stub := &stubService{}
	r := newTestRouter(stub)

	w := doJSON(r, http.MethodPost, "/api/v1/admins/7/roles", `{"role_ids":["1",2]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if len(stub.boundRoleIDs) != 2 || stub.boundRoleIDs[0] != 1 || stub.boundRoleIDs[1] != 2 {
		t.Fatalf("role_ids 解析异常: %v", stub.boundRoleIDs)
	}
}

// TestBindRolesRejectsInvalidID 非法 ID → 10003。
func TestBindRolesRejectsInvalidID(t *testing.T) {
	r := newTestRouter(&stubService{})
	w := doJSON(r, http.MethodPost, "/api/v1/admins/7/roles", `{"role_ids":["abc"]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if code := decodeBody(t, w)["code"]; code != float64(10003) {
		t.Fatalf("code = %v, want 10003", code)
	}
}

// TestInvalidPathID 路径 ID 非法 → 10003（不进入业务层）。
func TestInvalidPathID(t *testing.T) {
	r := newTestRouter(&stubService{})
	w := doJSON(r, http.MethodGet, "/api/v1/admins/abc", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if code := decodeBody(t, w)["code"]; code != float64(10003) {
		t.Fatalf("code = %v, want 10003", code)
	}
}

// TestCreateRoleParsesPermissionMatrix 权限矩阵按 {module, permission} 解析并下发业务层。
func TestCreateRoleParsesPermissionMatrix(t *testing.T) {
	stub := &stubService{}
	r := newTestRouter(stub)

	body := `{
		"name": "只读观察员",
		"description": "全部子模块只读",
		"permissions": [
			{"module": "env.list", "permission": "read"},
			{"module": "report.list", "permission": "read"}
		]
	}`
	w := doJSON(r, http.MethodPost, "/api/v1/roles", body)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
	if len(stub.roleIn.Permissions) != 2 {
		t.Fatalf("权限矩阵解析异常: %+v", stub.roleIn.Permissions)
	}
	if stub.roleIn.Permissions[0].Module != "env.list" || stub.roleIn.Permissions[0].Permission != "read" {
		t.Fatalf("权限项异常: %+v", stub.roleIn.Permissions[0])
	}
}

// TestListPermissionsReturnsMatrix 权限矩阵含敏感标记与可授权限。
func TestListPermissionsReturnsMatrix(t *testing.T) {
	r := newTestRouter(&stubService{})
	w := doJSON(r, http.MethodGet, "/api/v1/permissions", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var body struct {
		Data struct {
			List []auth.PermissionModule `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if len(body.Data.List) != len(auth.PermissionModules()) {
		t.Fatalf("权限矩阵条数 = %d, want %d", len(body.Data.List), len(auth.PermissionModules()))
	}
	var found bool
	for _, m := range body.Data.List {
		if m.ModuleCode == "setting.admin" {
			found = true
			if !m.Sensitive {
				t.Fatal("setting.admin 应标记为敏感")
			}
			if len(m.Permissions) != 1 || m.Permissions[0] != "write" {
				t.Fatalf("setting.admin 可授权限异常: %v", m.Permissions)
			}
		}
	}
	if !found {
		t.Fatal("权限矩阵缺少 setting.admin")
	}
}
