package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/auth"
	"auto_performance/internal/errcode"
	authsvc "auto_performance/internal/service/auth"
)

const testSecret = "test-secret-for-authn"

// fakeAuthenticator 认证服务假实现，按用例返回主体或错误。
type fakeAuthenticator struct {
	principal *authsvc.Principal
	err       error
	revoked   map[string]bool
}

func (f *fakeAuthenticator) Principal(_ context.Context, _ int64) (*authsvc.Principal, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.principal, nil
}

func (f *fakeAuthenticator) IsRevoked(jti string) bool { return f.revoked[jti] }

func newAuthNEngine(svc Authenticator, routes ...string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api/v1")
	g.Use(AuthN(testSecret, svc))
	for _, p := range routes {
		g.GET(p, func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"code": 0}) })
	}
	return r
}

func doRequest(r *gin.Engine, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeCode(t *testing.T, w *httptest.ResponseRecorder) int {
	t.Helper()
	var body struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("响应不是统一 JSON 结构: %s", w.Body.String())
	}
	return body.Code
}

func validPrincipal() *authsvc.Principal {
	return &authsvc.Principal{
		AdminID:     42,
		Username:    "alice",
		Status:      "enabled",
		Permissions: map[string]bool{"env.list:read": true},
	}
}

// TestAuthNRejectsExpiredToken 过期 token 应返回 401/10101。
func TestAuthNRejectsExpiredToken(t *testing.T) {
	r := newAuthNEngine(&fakeAuthenticator{principal: validPrincipal()}, "/environments")

	token, err := auth.GenerateToken("42", "alice", testSecret, -time.Minute)
	if err != nil {
		t.Fatalf("签发过期 token 失败: %v", err)
	}

	w := doRequest(r, http.MethodGet, "/api/v1/environments", token)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if code := decodeCode(t, w); code != errcode.ErrUnauth.Code {
		t.Fatalf("code = %d, want %d", code, errcode.ErrUnauth.Code)
	}
}

// TestAuthNRejectsMissingOrForgedToken 缺失/伪造签名 token 应返回 401。
func TestAuthNRejectsMissingOrForgedToken(t *testing.T) {
	r := newAuthNEngine(&fakeAuthenticator{principal: validPrincipal()}, "/environments")

	if w := doRequest(r, http.MethodGet, "/api/v1/environments", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("无 token status = %d, want 401", w.Code)
	}

	forged, err := auth.GenerateToken("42", "alice", "another-secret", time.Minute)
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}
	if w := doRequest(r, http.MethodGet, "/api/v1/environments", forged); w.Code != http.StatusUnauthorized {
		t.Fatalf("伪造签名 status = %d, want 401", w.Code)
	}
}

// TestAuthNBlocksDisabledAdmin 停用账号应返回 403/10202。
func TestAuthNBlocksDisabledAdmin(t *testing.T) {
	svc := &fakeAuthenticator{err: errcode.ErrDisabled}
	r := newAuthNEngine(svc, "/environments")

	token, err := auth.GenerateToken("42", "alice", testSecret, time.Minute)
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}

	w := doRequest(r, http.MethodGet, "/api/v1/environments", token)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if code := decodeCode(t, w); code != errcode.ErrDisabled.Code {
		t.Fatalf("code = %d, want %d", code, errcode.ErrDisabled.Code)
	}
}

// TestAuthNRejectsRevokedToken 登出失效后的 token 重放应返回 401/10102。
func TestAuthNRejectsRevokedToken(t *testing.T) {
	token, err := auth.GenerateToken("42", "alice", testSecret, time.Minute)
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}
	claims, err := auth.ParseToken(token, testSecret)
	if err != nil {
		t.Fatalf("解析 token 失败: %v", err)
	}
	if claims.KeyID() == "" {
		t.Fatal("token 应携带 jti")
	}

	r := newAuthNEngine(&fakeAuthenticator{
		principal: validPrincipal(),
		revoked:   map[string]bool{claims.KeyID(): true},
	}, "/environments")

	w := doRequest(r, http.MethodGet, "/api/v1/environments", token)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if code := decodeCode(t, w); code != errcode.ErrSession.Code {
		t.Fatalf("code = %d, want %d", code, errcode.ErrSession.Code)
	}
}

// TestAuthNBlocksMustChangePassword 首登强制改密期间仅放行改密/登出/当前用户。
func TestAuthNBlocksMustChangePassword(t *testing.T) {
	p := validPrincipal()
	p.MustChangePassword = true
	r := newAuthNEngine(&fakeAuthenticator{principal: p},
		"/environments", "/tasks",
	)
	authGroup := r.Group("/api/v1/auth")
	authGroup.Use(AuthN(testSecret, &fakeAuthenticator{principal: p}))
	for _, path := range []string{"/change-password", "/logout", "/me"} {
		authGroup.GET(path, func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"code": 0}) })
	}

	token, err := auth.GenerateToken("42", "alice", testSecret, time.Minute)
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}

	w := doRequest(r, http.MethodGet, "/api/v1/environments", token)
	if w.Code != http.StatusForbidden || decodeCode(t, w) != errcode.ErrMustPwd.Code {
		t.Fatalf("业务接口应返回 403/%d，got %d/%d",
			errcode.ErrMustPwd.Code, w.Code, decodeCode(t, w))
	}

	for _, path := range []string{"/api/v1/auth/me", "/api/v1/auth/logout"} {
		if w := doRequest(r, http.MethodGet, path, token); w.Code != http.StatusOK {
			t.Fatalf("%s 应放行，got %d", path, w.Code)
		}
	}
}

// TestAuthNInjectsContext 验证上下文注入（admin_id/username/权限/jti）。
func TestAuthNInjectsContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	var gotID, gotUser, gotJTI string
	var gotPerms map[string]bool
	g := r.Group("/api/v1")
	g.Use(AuthN(testSecret, &fakeAuthenticator{principal: validPrincipal()}))
	g.GET("/environments", func(c *gin.Context) {
		idVal, _ := c.Get(auth.CtxAdminID)
		gotID, _ = idVal.(string)
		userVal, _ := c.Get(auth.CtxUsername)
		gotUser, _ = userVal.(string)
		jtiVal, _ := c.Get(auth.CtxTokenID)
		gotJTI, _ = jtiVal.(string)
		v, _ := c.Get(auth.CtxPerms)
		gotPerms, _ = v.(map[string]bool)
		c.Status(http.StatusOK)
	})

	token, err := auth.GenerateToken("42", "alice", testSecret, time.Minute)
	if err != nil {
		t.Fatalf("签发 token 失败: %v", err)
	}
	if w := doRequest(r, http.MethodGet, "/api/v1/environments", token); w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if gotID != "42" || gotUser != "alice" {
		t.Fatalf("上下文注入异常: id=%q user=%q", gotID, gotUser)
	}
	if gotJTI == "" {
		t.Fatal("上下文应注入 jti")
	}
	if !gotPerms["env.list:read"] {
		t.Fatalf("上下文应注入权限并集: %v", gotPerms)
	}
}
