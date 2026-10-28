package api

// 安全红线专项集成测试（Phase 9 任务 9.3）。
//
// 本文件在 HTTP 接入层补齐「服务端最终鉴权 / 分级分权 / 敏感子模块 write-only /
// 日志只读 / 防 Mass Assignment」的红线证据；其余红线（密钥加密、注入、审计链、
// 脱敏、上传校验、路径穿越、低权限）由对应模块单测出证，清单见
// `.project/security/安全专项测试证据.md`。

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/setting"
	"auto_performance/internal/auth"
	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
	authsvc "auto_performance/internal/service/auth"
	settingsvc "auto_performance/internal/service/setting"
)

// securedRoute 受 RBAC 保护的业务路由（统一描述，供鉴权矩阵用例遍历）。
type securedRoute struct {
	Method string
	Path   string
	Module string
	Perm   string
}

// securedRoutesForTest 汇总全部业务路由及其权限点（唯一事实源来自 router.go 的路由表）。
//
// 共 77 条（81 REST - 4 条认证接口；认证接口只要求登录态，不做子模块鉴权）。
func securedRoutesForTest() []securedRoute {
	out := make([]securedRoute, 0, 77)
	for _, rt := range settingAdminRoutes() {
		out = append(out, securedRoute{rt.Method, rt.Path, "setting.admin", "write"})
	}
	for _, rt := range environmentRoutes() {
		out = append(out, securedRoute{rt.Method, rt.Path, rt.Module, rt.Perm})
	}
	for _, rt := range productRoutes() {
		out = append(out, securedRoute{rt.Method, rt.Path, rt.Module, rt.Perm})
	}
	for _, rt := range taskRoutes() {
		out = append(out, securedRoute{rt.Method, rt.Path, rt.Module, rt.Perm})
	}
	for _, rt := range reportRoutes() {
		out = append(out, securedRoute{rt.Method, rt.Path, rt.Module, rt.Perm})
	}
	for _, rt := range logRoutes() {
		out = append(out, securedRoute{rt.Method, rt.Path, rt.Module, rt.Perm})
	}
	for _, rt := range logSettingRoutes() {
		out = append(out, securedRoute{rt.Method, rt.Path, rt.Module, rt.Perm})
	}
	for _, rt := range settingSecretRoutes() {
		out = append(out, securedRoute{rt.Method, rt.Path, rt.Module, rt.Perm})
	}
	return out
}

// concretePath 拼接 API 前缀并把路由模板中的 :id 替换为具体值
// （鉴权在 handler 之前完成，ID 无需真实存在）。
func concretePath(p string) string {
	return "/api/v1" + strings.ReplaceAll(p, ":id", "1")
}

// TestSecurityAllBusinessRoutesRequireAuthN 无凭据访问任一业务接口 → 401（DES-S-001 / TEST-S-001）。
func TestSecurityAllBusinessRoutesRequireAuthN(t *testing.T) {
	r, _ := newTestRouter(t, map[string]bool{})
	routes := securedRoutesForTest()
	if len(routes) != 77 {
		t.Fatalf("受保护业务路由数 = %d, want 77（81 REST - 4 认证）", len(routes))
	}

	for _, rt := range routes {
		w := call(r, rt.Method, concretePath(rt.Path), "")
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s 无 token status = %d, want 401", rt.Method, rt.Path, w.Code)
			continue
		}
		if code := bodyCode(t, w); code != errcode.ErrUnauth.Code {
			t.Errorf("%s %s code = %d, want %d", rt.Method, rt.Path, code, errcode.ErrUnauth.Code)
		}
	}
}

// TestSecurityEmptyPermissionsDeniedEverywhere 已登录但无任何权限 → 所有业务接口 403（INV-1）。
func TestSecurityEmptyPermissionsDeniedEverywhere(t *testing.T) {
	r, token := newTestRouter(t, map[string]bool{})

	for _, rt := range securedRoutesForTest() {
		w := call(r, rt.Method, concretePath(rt.Path), token)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s 空权限 status = %d, want 403", rt.Method, rt.Path, w.Code)
			continue
		}
		if code := bodyCode(t, w); code != errcode.ErrForbidden.Code {
			t.Errorf("%s %s code = %d, want %d", rt.Method, rt.Path, code, errcode.ErrForbidden.Code)
		}
	}
}

// TestSecurityReadOnlyRoleCannotWrite 仅 read 权限 → 读接口放行、写接口 403（DES-E-001 / TEST-E-001）。
func TestSecurityReadOnlyRoleCannotWrite(t *testing.T) {
	routes := securedRoutesForTest()

	// 按模块聚合：授予 read 后逐个路由验证读写分离。
	modules := map[string]bool{}
	for _, rt := range routes {
		modules[rt.Module] = true
	}

	for module := range modules {
		router, tok := newTestRouter(t, map[string]bool{module + ":read": true})

		for _, rt := range routes {
			if rt.Module != module {
				continue
			}
			w := call(router, rt.Method, concretePath(rt.Path), tok)
			switch rt.Perm {
			case "write":
				if w.Code != http.StatusForbidden {
					t.Errorf("%s %s 仅 read 权限 status = %d, want 403", rt.Method, rt.Path, w.Code)
				}
			default:
				if w.Code == http.StatusForbidden || w.Code == http.StatusUnauthorized {
					t.Errorf("%s %s 具备 read 权限却 status = %d", rt.Method, rt.Path, w.Code)
				}
			}
		}
	}
}

// TestSecuritySensitiveModulesAreWriteOnly 敏感子模块（管理员/证书/SSH）write-only：
// 即便被授予 read 也不得访问（DES-E-007 / TEST-E-001）。
func TestSecuritySensitiveModulesAreWriteOnly(t *testing.T) {
	sensitive := []string{"setting.admin", "setting.cert", "setting.ssh"}

	for _, module := range sensitive {
		r, token := newTestRouter(t, map[string]bool{module + ":read": true})
		for _, rt := range securedRoutesForTest() {
			if rt.Module != module {
				continue
			}
			w := call(r, rt.Method, concretePath(rt.Path), token)
			if w.Code != http.StatusForbidden {
				t.Errorf("%s %s 仅 %s:read 应被拒绝, status = %d", rt.Method, rt.Path, module, w.Code)
			}
		}
	}

	// 反向验证：授予 write 后不再被 403（说明拒绝原因确实是权限点而非路由错误）。
	r, token := newTestRouter(t, map[string]bool{"setting.cert:write": true})
	var checked int
	for _, rt := range securedRoutesForTest() {
		if rt.Module != "setting.cert" {
			continue
		}
		checked++
		if w := call(r, rt.Method, concretePath(rt.Path), token); w.Code == http.StatusForbidden {
			t.Errorf("%s %s 具备 setting.cert:write 却 403", rt.Method, rt.Path)
		}
	}
	if checked == 0 {
		t.Fatal("未找到证书管理路由")
	}
}

// TestSecurityLogRoutesAreReadOnly 日志接口只读：不存在任何写/删/改路由（DES-T-006 / INV-8）。
func TestSecurityLogRoutesAreReadOnly(t *testing.T) {
	r, _ := newTestRouter(t, map[string]bool{})

	var logRouteCount int
	for _, rt := range r.Routes() {
		if !strings.HasPrefix(rt.Path, "/api/v1/logs/") {
			continue
		}
		logRouteCount++
		if rt.Method != http.MethodGet {
			t.Fatalf("日志路由出现非 GET 方法: %s %s", rt.Method, rt.Path)
		}
	}
	if logRouteCount != 4 {
		t.Fatalf("日志查询路由数 = %d, want 4（user/system/test/request）", logRouteCount)
	}

	// 任意日志子模块均无 DELETE/PUT/PATCH 端点（路由层兜底）。
	for _, m := range []string{http.MethodDelete, http.MethodPut, http.MethodPatch, http.MethodPost} {
		req := call(r, m, "/api/v1/logs/user", "")
		if req.Code == http.StatusOK || req.Code == http.StatusNoContent {
			t.Fatalf("%s /logs/user 不应成功: %s", m, req.Body.String())
		}
	}
}

// recordingSettings 记录 CreateAdmin 入参，验证 handler 白名单绑定（DES-E-002）。
type recordingSettings struct {
	setting.Service
	mu      sync.Mutex
	created *settingsvc.CreateAdminInput
}

func (s *recordingSettings) CreateAdmin(_ context.Context, in settingsvc.CreateAdminInput) (*settingsvc.AdminView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := in
	s.created = &cp
	return &settingsvc.AdminView{
		ID: 9, Username: in.Username, Name: in.Name, Phone: in.Phone, Email: in.Email,
		Description: in.Description, Status: domain.AdminStatusEnabled, Version: 1,
	}, nil
}

func (s *recordingSettings) input() *settingsvc.CreateAdminInput {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.created
}

// TestSecurityMassAssignmentDroppedAtHandler 请求体夹带提权字段（role/status/is_builtin/
// must_change_password/role_ids）不会进入服务入参，也不可能改变新建账号的默认权限与状态
// （DES-E-002 / TEST-E-002）。
func TestSecurityMassAssignmentDroppedAtHandler(t *testing.T) {
	cfg := testConfig()
	recorder := &recordingSettings{}
	svc := authsvc.New(&fakeAdminRepo{
		admin: &domain.Admin{ID: 7, Username: "alice", Status: domain.AdminStatusEnabled},
		perms: map[string]bool{"setting.admin:write": true},
	}, time.Minute)
	r := NewRouter(Deps{
		Config: cfg, Auth: svc, Settings: recorder,
		AuditKey: []byte("0123456789abcdef0123456789abcdef"),
	})
	token, err := auth.GenerateToken("7", "alice", cfg.JWT.Secret, time.Hour)
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}

	body := `{
		"username":"mallory","name":"Mallory","phone":"13800000000","email":"m@example.test",
		"password":"Passw0rd!23","description":"normal user",
		"role":"super","role_ids":[1,2],"status":"disabled","is_builtin":true,
		"must_change_password":false,"is_super":true
	}`
	w := callJSON(r, http.MethodPost, "/api/v1/admins", token, body)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /admins status = %d, want 200: %s", w.Code, w.Body.String())
	}

	in := recorder.input()
	if in == nil {
		t.Fatal("服务未收到创建请求")
	}
	if in.Username != "mallory" || in.Name != "Mallory" || in.Password != "Passw0rd!23" {
		t.Fatalf("白名单字段未正确传递: %+v", in)
	}

	// 响应体不含提权痕迹：服务端默认启用、非内置、角色为空。
	var resp struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if status, _ := resp.Data["status"].(string); status == domain.AdminStatusDisabled {
		t.Fatalf("请求体 status 字段影响了新建账号状态: %v", resp.Data)
	}
	if builtin, _ := resp.Data["is_builtin"].(bool); builtin {
		t.Fatalf("请求体 is_builtin 字段影响了新建账号: %v", resp.Data)
	}
	for key := range resp.Data {
		switch key {
		case "role", "role_ids", "is_super":
			t.Fatalf("响应出现提权字段 %q: %v", key, resp.Data)
		}
	}
}

// TestSecuritySensitiveResponsesHideSecrets 权限矩阵/证书响应不返回口令哈希、私钥等敏感字段
// （DES-I-001 / DES-I-004）。
func TestSecuritySensitiveResponsesHideSecrets(t *testing.T) {
	cases := []struct {
		name  string
		build func(*testing.T) (*gin.Engine, string)
		path  string
	}{
		{
			name: "权限矩阵",
			build: func(t *testing.T) (*gin.Engine, string) {
				return newTestRouterWithSettings(t, map[string]bool{"setting.admin:write": true})
			},
			path: "/api/v1/permissions",
		},
		{
			name: "证书列表",
			build: func(t *testing.T) (*gin.Engine, string) {
				return newTestRouterWithSecrets(t, map[string]bool{"setting.cert:write": true})
			},
			path: "/api/v1/certificates",
		},
	}

	for _, tc := range cases {
		r, token := tc.build(t)
		w := call(r, http.MethodGet, tc.path, token)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want 200: %s", tc.path, w.Code, w.Body.String())
		}
		body := strings.ToLower(w.Body.String())
		for _, leaked := range []string{"password_hash", "passwordhash", "secret", "private_key", "key_pem", "-----begin"} {
			if strings.Contains(body, leaked) {
				t.Fatalf("%s 响应泄露敏感字段 %q: %s", tc.name, leaked, w.Body.String())
			}
		}
	}
}
