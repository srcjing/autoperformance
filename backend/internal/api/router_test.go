package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	envapi "auto_performance/internal/api/environment"
	logapi "auto_performance/internal/api/log"
	productapi "auto_performance/internal/api/product"
	"auto_performance/internal/api/setting"
	taskapi "auto_performance/internal/api/task"
	"auto_performance/internal/auth"
	"auto_performance/internal/config"
	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
	authsvc "auto_performance/internal/service/auth"
	envsvc "auto_performance/internal/service/environment"
	logsvc "auto_performance/internal/service/log"
	productsvc "auto_performance/internal/service/product"
	settingsvc "auto_performance/internal/service/setting"
	testsvc "auto_performance/internal/service/testing"
	"auto_performance/internal/ws"
)

// expectedRoutePlan 期望注册的路由清单（独立于 router.moduleRoutes 书写，
// 用于校验 specs/api-contract.md §2 的 78 个 REST 接口 + 1 条实时通道（/ws）一条不漏、无多余）。
var expectedRoutePlan = []string{
	// 认证（4）
	"POST /api/v1/auth/login",
	"POST /api/v1/auth/logout",
	"POST /api/v1/auth/change-password",
	"GET /api/v1/auth/me",
	// 环境管理（16）
	"GET /api/v1/environment-categories",
	"POST /api/v1/environment-categories",
	"PUT /api/v1/environment-categories/:id",
	"DELETE /api/v1/environment-categories/:id",
	"GET /api/v1/tags",
	"POST /api/v1/tags",
	"PUT /api/v1/tags/:id",
	"DELETE /api/v1/tags/:id",
	"GET /api/v1/tags/:id/environments",
	"GET /api/v1/environments",
	"POST /api/v1/environments",
	"GET /api/v1/environments/:id",
	"PUT /api/v1/environments/:id",
	"DELETE /api/v1/environments/:id",
	"PUT /api/v1/environments/:id/tags",
	"PATCH /api/v1/environments/:id/status",
	// 产品管理（13）
	"GET /api/v1/products",
	"POST /api/v1/products",
	"GET /api/v1/products/:id",
	"PUT /api/v1/products/:id",
	"DELETE /api/v1/products/:id",
	"PATCH /api/v1/products/:id/status",
	"GET /api/v1/products/:id/interfaces",
	"POST /api/v1/products/:id/interfaces",
	"PUT /api/v1/interfaces/:id",
	"DELETE /api/v1/interfaces/:id",
	"PATCH /api/v1/interfaces/:id/status",
	"GET /api/v1/interfaces/:id/thresholds",
	"PUT /api/v1/interfaces/:id/thresholds",
	// 测试管理（12）
	"GET /api/v1/templates",
	"POST /api/v1/templates",
	"GET /api/v1/templates/:id",
	"PUT /api/v1/templates/:id",
	"DELETE /api/v1/templates/:id",
	"POST /api/v1/tasks",
	"GET /api/v1/tasks",
	"GET /api/v1/tasks/:id",
	"POST /api/v1/tasks/:id/start",
	"POST /api/v1/tasks/:id/cancel",
	"POST /api/v1/tasks/:id/retry",
	"POST /api/v1/tasks/:id/cleanup",
	// 报告管理（6）
	"GET /api/v1/reports",
	"GET /api/v1/reports/:id",
	"POST /api/v1/reports/:id/generate",
	"POST /api/v1/reports/import",
	"GET /api/v1/reports/:id/export",
	"GET /api/v1/reports/:id/artifact",
	// 日志管理（4）
	"GET /api/v1/logs/user",
	"GET /api/v1/logs/system",
	"GET /api/v1/logs/test",
	"GET /api/v1/logs/request",
	// 日志设置（2）
	"GET /api/v1/log-settings",
	"PUT /api/v1/log-settings",
	// 系统设置 · 管理员与角色（15）
	"GET /api/v1/admins",
	"POST /api/v1/admins",
	"GET /api/v1/admins/:id",
	"PUT /api/v1/admins/:id",
	"DELETE /api/v1/admins/:id",
	"PATCH /api/v1/admins/:id/status",
	"POST /api/v1/admins/:id/reset-password",
	"GET /api/v1/admins/:id/roles",
	"POST /api/v1/admins/:id/roles",
	"GET /api/v1/roles",
	"POST /api/v1/roles",
	"GET /api/v1/roles/:id",
	"PUT /api/v1/roles/:id",
	"DELETE /api/v1/roles/:id",
	"GET /api/v1/permissions",
	// 系统设置 · 证书（4）
	"GET /api/v1/certificates",
	"POST /api/v1/certificates",
	"DELETE /api/v1/certificates/:id",
	"POST /api/v1/certificates/:id/reinit",
	// 系统设置 · SSH/Host（5）
	"GET /api/v1/ssh-hosts",
	"POST /api/v1/ssh-hosts",
	"POST /api/v1/ssh-hosts/import",
	"PUT /api/v1/ssh-hosts/:id",
	"DELETE /api/v1/ssh-hosts/:id",
	// 实时通道（1，非 REST 接口，api-contract §1.1）
	"GET /api/v1/ws",
}

func testConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{Host: "127.0.0.1", Port: 8080, Mode: gin.TestMode},
		JWT:    config.JWTConfig{Secret: "router-test-secret", ExpireSeconds: 3600},
	}
}

// fakeAdminRepo 供路由级集成测试使用的假仓储。
type fakeAdminRepo struct {
	admin *domain.Admin
	perms map[string]bool
	err   error // 非空时模拟 DB 不可用
}

func (f *fakeAdminRepo) GetAdminByID(_ context.Context, _ int64) (*domain.Admin, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.admin, nil
}

func (f *fakeAdminRepo) ListPermissionsByAdmin(_ context.Context, _ int64) (map[string]bool, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.perms, nil
}

func (f *fakeAdminRepo) GetAdminByUsername(_ context.Context, _ string) (*domain.Admin, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.admin, nil
}

func (f *fakeAdminRepo) UpdateLastLogin(_ context.Context, _ int64, _ time.Time) error {
	return f.err
}

func (f *fakeAdminRepo) UpdatePassword(_ context.Context, _ int64, _ string, _ bool) error {
	return f.err
}

func newTestRouter(t *testing.T, perms map[string]bool) (*gin.Engine, string) {
	t.Helper()
	cfg := testConfig()
	svc := authsvc.New(&fakeAdminRepo{
		admin: &domain.Admin{ID: 7, Username: "alice", Status: domain.AdminStatusEnabled},
		perms: perms,
	}, time.Minute)

	r := NewRouter(Deps{Config: cfg, Auth: svc, Audit: nil, AuditKey: []byte("0123456789abcdef0123456789abcdef")})

	token, err := auth.GenerateToken("7", "alice", cfg.JWT.Secret, time.Hour)
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}
	return r, token
}

// newTestRouterWithSettings 构造注入了系统设置服务的路由（验证 Phase 1 handler 接管）。
func newTestRouterWithSettings(t *testing.T, perms map[string]bool) (*gin.Engine, string) {
	t.Helper()
	cfg := testConfig()
	svc := authsvc.New(&fakeAdminRepo{
		admin: &domain.Admin{ID: 7, Username: "alice", Status: domain.AdminStatusEnabled},
		perms: perms,
	}, time.Minute)

	r := NewRouter(Deps{
		Config: cfg, Auth: svc, Settings: &stubSettings{},
		AuditKey: []byte("0123456789abcdef0123456789abcdef"),
	})

	token, err := auth.GenerateToken("7", "alice", cfg.JWT.Secret, time.Hour)
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}
	return r, token
}

func call(r *gin.Engine, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// callJSON 发起带 JSON 请求体的调用。
func callJSON(r *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func bodyCode(t *testing.T, w *httptest.ResponseRecorder) int {
	t.Helper()
	var body struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是统一 JSON 结构: %s", w.Body.String())
	}
	return body.Code
}

// TestRouterRegistersAllSeventyEightEndpoints 校验全部接口（81 REST + /ws）全部注册且无多余。
func TestRouterRegistersAllSeventyEightEndpoints(t *testing.T) {
	r, _ := newTestRouter(t, map[string]bool{})

	got := make(map[string]bool)
	for _, rt := range r.Routes() {
		got[rt.Method+" "+rt.Path] = true
	}

	if len(expectedRoutePlan) != 82 {
		t.Fatalf("期望清单应为 82 条（81 REST + WS），实际 %d 条", len(expectedRoutePlan))
	}
	for _, want := range expectedRoutePlan {
		if !got[want] {
			t.Errorf("缺少路由: %s", want)
		}
	}

	// 业务路由总数：81 REST + /ws + /healthz；NoRoute 不计入 Routes()。
	if len(r.Routes()) != 83 {
		var paths []string
		for _, rt := range r.Routes() {
			paths = append(paths, rt.Method+" "+rt.Path)
		}
		sort.Strings(paths)
		t.Fatalf("路由总数 = %d, want 83（81 REST + /ws + /healthz）:\n%s", len(r.Routes()), paths)
	}
}

// TestRouterRequiresAuthN 未携带 token 的业务接口一律 401（服务端最终鉴权）。
func TestRouterRequiresAuthN(t *testing.T) {
	r, _ := newTestRouter(t, map[string]bool{})

	for _, path := range []string{
		"/api/v1/environments", "/api/v1/products", "/api/v1/tasks",
		"/api/v1/reports", "/api/v1/logs/user", "/api/v1/roles", "/api/v1/ssh-hosts",
	} {
		w := call(r, http.MethodGet, path, "")
		if w.Code != http.StatusUnauthorized {
			t.Errorf("GET %s 无 token status = %d, want 401", path, w.Code)
			continue
		}
		if code := bodyCode(t, w); code != errcode.ErrUnauth.Code {
			t.Errorf("GET %s code = %d, want %d", path, code, errcode.ErrUnauth.Code)
		}
	}
}

// TestRouterEnforcesAuthZPermissions 校验权限点映射生效（read/write 分离、敏感模块 write-only）。
func TestRouterEnforcesAuthZPermissions(t *testing.T) {
	r, token := newTestRouter(t, map[string]bool{"env.list:read": true})

	// 有 read：路由骨架返回 501（说明已通过 AuthN + AuthZ）。
	if w := call(r, http.MethodGet, "/api/v1/environments", token); w.Code != http.StatusNotImplemented {
		t.Fatalf("GET /environments status = %d, want 501（骨架）", w.Code)
	}
	// 无 write：写请求被拒。
	if w := call(r, http.MethodPost, "/api/v1/environments", token); w.Code != http.StatusForbidden {
		t.Fatalf("POST /environments status = %d, want 403", w.Code)
	}
	// 未授权模块：直接 403。
	if w := call(r, http.MethodGet, "/api/v1/products", token); w.Code != http.StatusForbidden {
		t.Fatalf("GET /products status = %d, want 403", w.Code)
	}
	// 敏感子模块为 write-only：仅有 read 不足以访问（也不存在 read 授权）。
	if w := call(r, http.MethodGet, "/api/v1/admins", token); w.Code != http.StatusForbidden {
		t.Fatalf("GET /admins status = %d, want 403", w.Code)
	}
}

// TestRouterAuthEndpoints 校验认证模块：/auth/me 返回真实权限并集，登出后 token 失效。
func TestRouterAuthEndpoints(t *testing.T) {
	r, token := newTestRouter(t, map[string]bool{"env.list:read": true})

	w := call(r, http.MethodGet, "/api/v1/auth/me", token)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /auth/me status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			Admin       map[string]any `json:"admin"`
			Permissions []string       `json:"permissions"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析 /auth/me 响应失败: %v", err)
	}
	if len(body.Data.Permissions) != 1 || body.Data.Permissions[0] != "env.list:read" {
		t.Fatalf("权限集合应来自 DB 并集, got %v", body.Data.Permissions)
	}
	if body.Data.Admin["username"] != "alice" {
		t.Fatalf("admin.username = %v, want alice", body.Data.Admin["username"])
	}

	// 登出后同一 token 重放 → 10102（会话失效）。
	if w := call(r, http.MethodPost, "/api/v1/auth/logout", token); w.Code != http.StatusOK {
		t.Fatalf("POST /auth/logout status = %d, want 200", w.Code)
	}
	w = call(r, http.MethodGet, "/api/v1/auth/me", token)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("登出后重放 status = %d, want 401", w.Code)
	}
	if code := bodyCode(t, w); code != errcode.ErrSession.Code {
		t.Fatalf("登出后重放 code = %d, want %d", code, errcode.ErrSession.Code)
	}
}

// TestRouterLoginFlow 端到端覆盖 AC-AUTH-1：统一错误提示 → 登录 → 首登拦截 → 改密 → 旧 token 失效。
func TestRouterLoginFlow(t *testing.T) {
	cfg := testConfig()
	const initialPwd = "Cqmyg@123"
	hash, err := authsvc.HashPassword(initialPwd)
	if err != nil {
		t.Fatalf("生成哈希失败: %v", err)
	}
	repo := &fakeAdminRepo{
		admin: &domain.Admin{
			ID: 7, Username: "alice", Name: "Alice", Status: domain.AdminStatusEnabled,
			PasswordHash: hash, MustChangePassword: true,
		},
		perms: map[string]bool{"env.list:read": true},
	}
	svc := authsvc.New(repo, time.Minute)
	r := NewRouter(Deps{Config: cfg, Auth: svc, AuditKey: []byte("0123456789abcdef0123456789abcdef")})

	// 1) 口令错误：统一 20001，不泄露账号是否存在。
	w := callJSON(r, http.MethodPost, "/api/v1/auth/login", "", `{"username":"alice","password":"wrong"}`)
	if w.Code != http.StatusUnauthorized || bodyCode(t, w) != 20001 {
		t.Fatalf("错误口令应 401/20001，got %d %s", w.Code, w.Body.String())
	}

	// 2) 登录成功：返回 token / 权限并集 / 首登改密标记。
	w = callJSON(r, http.MethodPost, "/api/v1/auth/login", "",
		`{"username":"alice","password":"`+initialPwd+`"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("登录失败: %d %s", w.Code, w.Body.String())
	}
	var login struct {
		Data struct {
			Token              string   `json:"token"`
			Permissions        []string `json:"permissions"`
			MustChangePassword bool     `json:"must_change_password"`
			Admin              struct {
				Username string `json:"username"`
			} `json:"admin"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &login); err != nil {
		t.Fatalf("解析登录响应失败: %v", err)
	}
	if login.Data.Token == "" || !login.Data.MustChangePassword {
		t.Fatalf("登录响应异常: %s", w.Body.String())
	}
	if login.Data.Admin.Username != "alice" {
		t.Fatalf("admin.username = %q, want alice", login.Data.Admin.Username)
	}
	token := login.Data.Token

	// 3) 首登未改密：业务接口被拦（403/10103），/auth/me 放行。
	w = call(r, http.MethodGet, "/api/v1/environments", token)
	if w.Code != http.StatusForbidden || bodyCode(t, w) != errcode.ErrMustPwd.Code {
		t.Fatalf("首登未改密应 403/10103，got %d %s", w.Code, w.Body.String())
	}
	if w = call(r, http.MethodGet, "/api/v1/auth/me", token); w.Code != http.StatusOK {
		t.Fatalf("/auth/me 应放行，got %d %s", w.Code, w.Body.String())
	}

	// 4) 旧口令错误 → 20003。
	w = callJSON(r, http.MethodPost, "/api/v1/auth/change-password", token,
		`{"old_password":"wrong","new_password":"NewSecret@456"}`)
	if w.Code != http.StatusBadRequest || bodyCode(t, w) != 20003 {
		t.Fatalf("旧口令错误应 400/20003，got %d %s", w.Code, w.Body.String())
	}

	// 5) 改密成功。
	w = callJSON(r, http.MethodPost, "/api/v1/auth/change-password", token,
		`{"old_password":"`+initialPwd+`","new_password":"NewSecret@456"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("改密失败: %d %s", w.Code, w.Body.String())
	}

	// 6) 改密后旧 token 失效（需重新登录）。
	w = call(r, http.MethodGet, "/api/v1/auth/me", token)
	if w.Code != http.StatusUnauthorized || bodyCode(t, w) != errcode.ErrSession.Code {
		t.Fatalf("旧 token 应 401/10102，got %d %s", w.Code, w.Body.String())
	}
}

// stubSettings 仅实现 /permissions 所需方法（其余方法不应被调用）。
// 以嵌入式接口提供方法集，避免为路由测试重复实现全量桩。
type stubSettings struct {
	setting.Service
}

func (s *stubSettings) PermissionModules() []auth.PermissionModule {
	return []auth.PermissionModule{{
		ModuleCode: "env.list", Name: "环境管理 · 环境列表",
		Permissions: []string{"read", "write"},
	}}
}

// TestRouterWiresSettingHandlers 系统设置真实 handler 已接管路由（不再返回 501）。
func TestRouterWiresSettingHandlers(t *testing.T) {
	r, token := newTestRouterWithSettings(t, map[string]bool{"setting.admin:write": true})

	w := call(r, http.MethodGet, "/api/v1/permissions", token)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /permissions status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			List []struct {
				ModuleCode string `json:"module_code"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析 /permissions 响应失败: %v", err)
	}
	if len(body.Data.List) != 1 || body.Data.List[0].ModuleCode != "env.list" {
		t.Fatalf("权限矩阵响应异常: %s", w.Body.String())
	}
}

// stubEnvironment 仅实现环境分类列表所需方法（其余方法由嵌入接口提供）。
type stubEnvironment struct {
	envapi.Service
}

func (s *stubEnvironment) ListCategories(_ context.Context) ([]envsvc.CategoryView, error) {
	return []envsvc.CategoryView{{ID: 1, Name: "单机", Version: 1}}, nil
}

// newTestRouterWithEnvironment 构造注入了环境管理服务的路由（验证 Phase 2 handler 接管）。
func newTestRouterWithEnvironment(t *testing.T, perms map[string]bool) (*gin.Engine, string) {
	t.Helper()
	cfg := testConfig()
	svc := authsvc.New(&fakeAdminRepo{
		admin: &domain.Admin{ID: 7, Username: "alice", Status: domain.AdminStatusEnabled},
		perms: perms,
	}, time.Minute)

	r := NewRouter(Deps{
		Config: cfg, Auth: svc, Environments: &stubEnvironment{},
		AuditKey: []byte("0123456789abcdef0123456789abcdef"),
	})

	token, err := auth.GenerateToken("7", "alice", cfg.JWT.Secret, time.Hour)
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}
	return r, token
}

// TestRouterWiresEnvironmentHandlers 环境管理真实 handler 已接管路由（不再返回 501），
// 且沿用 env.list/env.tag 读写分离的权限点。
func TestRouterWiresEnvironmentHandlers(t *testing.T) {
	r, token := newTestRouterWithEnvironment(t, map[string]bool{"env.list:read": true})

	w := call(r, http.MethodGet, "/api/v1/environment-categories", token)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /environment-categories status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			List []struct {
				Name string `json:"name"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析分类响应失败: %v", err)
	}
	if len(body.Data.List) != 1 || body.Data.List[0].Name != "单机" {
		t.Fatalf("分类响应异常: %s", w.Body.String())
	}

	// 仅有 read：写接口被 AuthZ 拒绝。
	if w := call(r, http.MethodPost, "/api/v1/environments", token); w.Code != http.StatusForbidden {
		t.Fatalf("POST /environments status = %d, want 403", w.Code)
	}
	// 标签模块未授权：直接 403。
	if w := call(r, http.MethodGet, "/api/v1/tags", token); w.Code != http.StatusForbidden {
		t.Fatalf("GET /tags status = %d, want 403", w.Code)
	}
}

// stubProduct 仅实现产品列表所需方法（其余方法由嵌入接口提供）。
type stubProduct struct {
	productapi.Service
}

func (s *stubProduct) ListProducts(_ context.Context, _ productsvc.ListProductsQuery) (*productsvc.ProductPage, error) {
	return &productsvc.ProductPage{
		List:  []productsvc.ProductView{{ID: 3, Name: "网关服务", Code: "gateway", Status: domain.ProductStatusEnabled, Version: 1}},
		Total: 1, Page: 1, PageSize: 20,
	}, nil
}

// newTestRouterWithProducts 构造注入了产品管理服务的路由（验证 Phase 3 handler 接管）。
func newTestRouterWithProducts(t *testing.T, perms map[string]bool) (*gin.Engine, string) {
	t.Helper()
	cfg := testConfig()
	svc := authsvc.New(&fakeAdminRepo{
		admin: &domain.Admin{ID: 7, Username: "alice", Status: domain.AdminStatusEnabled},
		perms: perms,
	}, time.Minute)

	r := NewRouter(Deps{
		Config: cfg, Auth: svc, Products: &stubProduct{},
		AuditKey: []byte("0123456789abcdef0123456789abcdef"),
	})

	token, err := auth.GenerateToken("7", "alice", cfg.JWT.Secret, time.Hour)
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}
	return r, token
}

// TestRouterWiresProductHandlers 产品管理真实 handler 已接管路由（不再返回 501），
// 且沿用 product.list / product.interface 读写分离的权限点。
func TestRouterWiresProductHandlers(t *testing.T) {
	r, token := newTestRouterWithProducts(t, map[string]bool{"product.list:read": true})

	w := call(r, http.MethodGet, "/api/v1/products", token)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /products status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			List []struct {
				Code string `json:"code"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析产品响应失败: %v", err)
	}
	if len(body.Data.List) != 1 || body.Data.List[0].Code != "gateway" {
		t.Fatalf("产品响应异常: %s", w.Body.String())
	}

	// 仅有 read：写接口被 AuthZ 拒绝。
	if w := call(r, http.MethodPost, "/api/v1/products", token); w.Code != http.StatusForbidden {
		t.Fatalf("POST /products status = %d, want 403", w.Code)
	}
	// 接口子模块未授权：直接 403（product.list:read 不蕴含 product.interface:read）。
	if w := call(r, http.MethodGet, "/api/v1/products/3/interfaces", token); w.Code != http.StatusForbidden {
		t.Fatalf("GET /products/3/interfaces status = %d, want 403", w.Code)
	}
}

// stubSecretSettings 仅实现证书列表所需方法（其余方法由嵌入接口提供）。
type stubSecretSettings struct {
	setting.Service
}

func (s *stubSecretSettings) ListCertificates(_ context.Context, page, pageSize int) (*settingsvc.CertificatePage, error) {
	return &settingsvc.CertificatePage{
		List: []settingsvc.CertificateView{{
			ID: 31, Name: "内置自签证书", IsBuiltin: true,
			Status: domain.CertStatusValid, HasKey: true, Version: 1,
		}},
		Total: 1, Page: page, PageSize: pageSize,
	}, nil
}

// newTestRouterWithSecrets 构造注入了系统设置证书/SSH 服务的路由（验证 Phase 7 handler 接管）。
func newTestRouterWithSecrets(t *testing.T, perms map[string]bool) (*gin.Engine, string) {
	t.Helper()
	cfg := testConfig()
	svc := authsvc.New(&fakeAdminRepo{
		admin: &domain.Admin{ID: 7, Username: "alice", Status: domain.AdminStatusEnabled},
		perms: perms,
	}, time.Minute)

	r := NewRouter(Deps{
		Config: cfg, Auth: svc, Settings: &stubSecretSettings{},
		AuditKey: []byte("0123456789abcdef0123456789abcdef"),
	})

	token, err := auth.GenerateToken("7", "alice", cfg.JWT.Secret, time.Hour)
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}
	return r, token
}

// TestRouterWiresSecretSettingHandlers 证书/SSH-Host 真实 handler 已接管路由（不再返回 501），
// 且两个敏感子模块权限点相互独立（setting.cert 不蕴含 setting.ssh）。
func TestRouterWiresSecretSettingHandlers(t *testing.T) {
	r, token := newTestRouterWithSecrets(t, map[string]bool{"setting.cert:write": true})

	w := call(r, http.MethodGet, "/api/v1/certificates", token)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /certificates status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			List []struct {
				Name      string `json:"name"`
				IsBuiltin bool   `json:"is_builtin"`
				Status    string `json:"status"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析证书响应失败: %v", err)
	}
	if len(body.Data.List) != 1 || !body.Data.List[0].IsBuiltin || body.Data.List[0].Status != domain.CertStatusValid {
		t.Fatalf("证书响应异常: %s", w.Body.String())
	}

	// SSH 子模块未授权：403（两个敏感子模块权限点相互独立）。
	if w := call(r, http.MethodGet, "/api/v1/ssh-hosts", token); w.Code != http.StatusForbidden {
		t.Fatalf("GET /ssh-hosts status = %d, want 403", w.Code)
	}
}

// TestRouterAuthServiceUnavailable 鉴权依赖异常（如 DB 不可用）返回 500/10602 而非 panic。
func TestRouterAuthServiceUnavailable(t *testing.T) {
	cfg := testConfig()
	svc := authsvc.New(&fakeAdminRepo{err: errors.New("db down")}, time.Minute)
	r := NewRouter(Deps{Config: cfg, Auth: svc, AuditKey: []byte("0123456789abcdef0123456789abcdef")})

	token, err := auth.GenerateToken("7", "alice", cfg.JWT.Secret, time.Hour)
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}

	w := call(r, http.MethodGet, "/api/v1/environments", token)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if code := bodyCode(t, w); code != errcode.ErrDependency.Code {
		t.Fatalf("code = %d, want %d", code, errcode.ErrDependency.Code)
	}

	// 健康检查不因 DB 不可用而变脸（仍返回 200 + db=down）。
	if w := call(r, http.MethodGet, "/healthz", ""); w.Code != http.StatusOK {
		t.Fatalf("/healthz status = %d, want 200", w.Code)
	}
}

// TestRouterHealthz 健康检查公开可访问且返回 200。
func TestRouterHealthz(t *testing.T) {
	r, _ := newTestRouter(t, map[string]bool{})
	w := call(r, http.MethodGet, "/healthz", "")
	if w.Code != http.StatusOK {
		t.Fatalf("/healthz status = %d, want 200", w.Code)
	}
}

// TestRouterNotFound 未注册路径返回统一 404 结构。
func TestRouterNotFound(t *testing.T) {
	r, _ := newTestRouter(t, map[string]bool{})
	w := call(r, http.MethodGet, "/api/v1/unknown", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("unknown path status = %d, want 404", w.Code)
	}
	if code := bodyCode(t, w); code != errcode.ErrNotFound.Code {
		t.Fatalf("unknown path code = %d, want %d", code, errcode.ErrNotFound.Code)
	}
}

// stubTasks 仅实现模板列表所需方法（其余方法由嵌入接口提供）。
type stubTasks struct {
	taskapi.Service
}

func (s *stubTasks) ListTemplates(_ context.Context, _ testsvc.ListTemplatesQuery) (*testsvc.TemplatePage, error) {
	return &testsvc.TemplatePage{
		List:  []testsvc.TemplateView{{ID: 5, Name: "基线模板", Version: 1}},
		Total: 1, Page: 1, PageSize: 20,
	}, nil
}

// newTestRouterWithTasks 构造注入了测试管理服务的路由（验证 Phase 4 handler 接管）。
func newTestRouterWithTasks(t *testing.T, perms map[string]bool) (*gin.Engine, string) {
	t.Helper()
	cfg := testConfig()
	svc := authsvc.New(&fakeAdminRepo{
		admin: &domain.Admin{ID: 7, Username: "alice", Status: domain.AdminStatusEnabled},
		perms: perms,
	}, time.Minute)

	r := NewRouter(Deps{
		Config: cfg, Auth: svc, Tasks: &stubTasks{},
		Hub:      ws.NewHub(cfg.JWT.Secret, nil),
		AuditKey: []byte("0123456789abcdef0123456789abcdef"),
	})

	token, err := auth.GenerateToken("7", "alice", cfg.JWT.Secret, time.Hour)
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}
	return r, token
}

// TestRouterWiresTaskHandlers 测试管理真实 handler 已接管路由，且沿用 testing.template / testing.task 读写分离。
func TestRouterWiresTaskHandlers(t *testing.T) {
	r, token := newTestRouterWithTasks(t, map[string]bool{"testing.template:read": true})

	w := call(r, http.MethodGet, "/api/v1/templates", token)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /templates status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			List []struct {
				Name string `json:"name"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析模板响应失败: %v", err)
	}
	if len(body.Data.List) != 1 || body.Data.List[0].Name != "基线模板" {
		t.Fatalf("模板响应异常: %s", w.Body.String())
	}

	// 仅有 template:read：任务模块未授权 → 403。
	if w := call(r, http.MethodGet, "/api/v1/tasks", token); w.Code != http.StatusForbidden {
		t.Fatalf("GET /tasks status = %d, want 403", w.Code)
	}
	// 模板写接口同样 403。
	if w := callJSON(r, http.MethodPost, "/api/v1/templates", token, `{"name":"x"}`); w.Code != http.StatusForbidden {
		t.Fatalf("POST /templates status = %d, want 403", w.Code)
	}
}

// stubLogs 仅实现用户日志查询所需方法（其余方法由嵌入接口提供）。
type stubLogs struct {
	logapi.Service
}

func (s *stubLogs) ListUserLogs(_ context.Context, _ domain.UserLogQuery) (*logsvc.UserLogPage, error) {
	adminID := int64(7)
	return &logsvc.UserLogPage{
		List: []domain.AuditLog{{
			ID: 11, AdminID: &adminID, AdminUsername: "alice", Action: "login",
			Result: domain.AuditResultOK,
		}},
		Total: 1, Page: 1, PageSize: 20,
	}, nil
}

// newTestRouterWithLogs 构造注入了日志服务的路由（验证 Phase 6 handler 接管）。
func newTestRouterWithLogs(t *testing.T, perms map[string]bool) (*gin.Engine, string) {
	t.Helper()
	cfg := testConfig()
	svc := authsvc.New(&fakeAdminRepo{
		admin: &domain.Admin{ID: 7, Username: "alice", Status: domain.AdminStatusEnabled},
		perms: perms,
	}, time.Minute)

	r := NewRouter(Deps{
		Config: cfg, Auth: svc, Logs: &stubLogs{},
		AuditKey: []byte("0123456789abcdef0123456789abcdef"),
	})

	token, err := auth.GenerateToken("7", "alice", cfg.JWT.Secret, time.Hour)
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}
	return r, token
}

// TestRouterWiresLogHandlers 日志 handler 已接管路由（不再返回 501），
// 且日志子模块仅授权 read（无 write 路由，DES-T-006）。
func TestRouterWiresLogHandlers(t *testing.T) {
	r, token := newTestRouterWithLogs(t, map[string]bool{"log.user:read": true})

	w := call(r, http.MethodGet, "/api/v1/logs/user", token)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /logs/user status = %d, want 200: %s", w.Code, w.Body.String())
	}
	var body struct {
		Data struct {
			List []struct {
				ID     string `json:"id"`
				Action string `json:"action"`
				Level  string `json:"level"`
			} `json:"list"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析日志响应失败: %v", err)
	}
	if len(body.Data.List) != 1 || body.Data.List[0].ID != "11" || body.Data.List[0].Level != domain.LogLevelInfo {
		t.Fatalf("日志响应异常: %s", w.Body.String())
	}

	// 其他日志子模块未授权：403（三个日志子模块权限点相互独立）。
	if w := call(r, http.MethodGet, "/api/v1/logs/system", token); w.Code != http.StatusForbidden {
		t.Fatalf("GET /logs/system status = %d, want 403", w.Code)
	}
	// 未授权用户日志子模块的服务也拒绝写方法（无写路由 → 405/404，不会落到业务 handler）。
	if w := call(r, http.MethodDelete, "/api/v1/logs/user", token); w.Code == http.StatusOK {
		t.Fatalf("日志为只读，DELETE 不得成功: %s", w.Body.String())
	}
}

// TestRouterWSRequiresToken WebSocket 握手无 token → 401/10101（DES-S-001）。
func TestRouterWSRequiresToken(t *testing.T) {
	r, token := newTestRouterWithTasks(t, map[string]bool{})

	w := call(r, http.MethodGet, "/api/v1/ws", "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("GET /ws 无 token status = %d, want 401", w.Code)
	}
	if code := bodyCode(t, w); code != errcode.ErrUnauth.Code {
		t.Fatalf("GET /ws code = %d, want %d", code, errcode.ErrUnauth.Code)
	}

	// 带上有效 token 但缺少 upgrade 头 → gorilla 返回 400（已通过握手鉴权阶段）。
	if w := call(r, http.MethodGet, "/api/v1/ws?token="+token, ""); w.Code == http.StatusUnauthorized {
		t.Fatalf("有效 token 不应被判未认证: %s", w.Body.String())
	}
}
