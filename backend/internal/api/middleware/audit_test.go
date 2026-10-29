package middleware

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/auth"
	"auto_performance/internal/domain"
	logsvc "auto_performance/internal/service/log"
)

// captureStore 捕获审计落库条目。
type captureStore struct {
	entries []domain.AuditLog
	err     error
}

func (c *captureStore) LastAuditHash(_ context.Context) ([]byte, error) {
	if len(c.entries) == 0 {
		return nil, nil
	}
	return c.entries[len(c.entries)-1].Hash, nil
}

func (c *captureStore) AppendUserLog(_ context.Context, entry *domain.AuditLog) error {
	if c.err != nil {
		return c.err
	}
	c.entries = append(c.entries, *entry)
	return nil
}

func newAuditEngine(store *captureStore) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(AuditLog(logsvc.NewService(store), []byte("0123456789abcdef0123456789abcdef")))
	r.POST("/api/v1/environments", func(c *gin.Context) {
		c.Set(auth.CtxAdminID, "42")
		c.Status(http.StatusCreated)
	})
	r.GET("/api/v1/environments", func(c *gin.Context) { c.Status(http.StatusOK) })
	r.GET("/api/v1/logs/user", func(c *gin.Context) { c.Status(http.StatusForbidden) })
	r.GET("/healthz", func(c *gin.Context) { c.Status(http.StatusOK) })
	return r
}

// TestAuditLogAppendsWriteRequests 写请求落审计链，读成功请求不落库，读失败请求落库。
func TestAuditLogAppendsWriteRequests(t *testing.T) {
	store := &captureStore{}
	r := newAuditEngine(store)

	if w := doRequest(r, http.MethodPost, "/api/v1/environments", ""); w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", w.Code)
	}
	if len(store.entries) != 1 {
		t.Fatalf("写请求应落 1 条审计，got %d", len(store.entries))
	}
	entry := store.entries[0]
	if entry.AdminID == nil || *entry.AdminID != 42 {
		t.Fatalf("应记录操作人 admin_id=42: %+v", entry.AdminID)
	}
	if entry.Action != "create" || entry.ResourceType != "environment" {
		t.Fatalf("动作/资源推导异常: action=%q resource=%q", entry.Action, entry.ResourceType)
	}
	if entry.Result != domain.AuditResultOK {
		t.Fatalf("result = %q, want ok", entry.Result)
	}
	if len(entry.Hash) != 32 {
		t.Fatalf("应写入 32 字节 HMAC-SHA256, got %d", len(entry.Hash))
	}

	// 读成功：不落库（避免日志查询自放大）。
	doRequest(r, http.MethodGet, "/api/v1/environments", "")
	if len(store.entries) != 1 {
		t.Fatalf("读成功请求不应落审计，got %d 条", len(store.entries))
	}

	// 读失败：落库（越权探测留痕）。
	doRequest(r, http.MethodGet, "/api/v1/logs/user", "")
	if len(store.entries) != 2 {
		t.Fatalf("读失败请求应落审计，got %d 条", len(store.entries))
	}
	if store.entries[1].Result != domain.AuditResultFail || store.entries[1].Action != "read" {
		t.Fatalf("失败读请求审计异常: %+v", store.entries[1])
	}
	if string(store.entries[1].PrevHash) != string(store.entries[0].Hash) {
		t.Fatal("审计链应逐条链接 prev_hash")
	}

	// 健康检查：不落库。
	doRequest(r, http.MethodGet, "/healthz", "")
	if len(store.entries) != 2 {
		t.Fatalf("健康检查不应落审计，got %d 条", len(store.entries))
	}
}

// TestAuditLogDoesNotBreakRequest 审计写入失败不影响业务响应。
func TestAuditLogDoesNotBreakRequest(t *testing.T) {
	store := &captureStore{err: errors.New("audit store down")}
	r := newAuditEngine(store)

	w := doRequest(r, http.MethodPost, "/api/v1/environments", "")
	if w.Code != http.StatusCreated {
		t.Fatalf("审计失败不应改变业务状态码, got %d", w.Code)
	}
}

// TestAuditActionMapping 验证动作/资源推导（子资源动作优先于 CRUD）。
func TestAuditActionMapping(t *testing.T) {
	cases := []struct{ method, path, action, resource string }{
		{"POST", "/api/v1/auth/login", "login", "auth"},
		{"POST", "/api/v1/auth/change-password", "change-password", "auth"},
		{"GET", "/api/v1/auth/me", "me", "auth"},
		{"POST", "/api/v1/tasks/3/start", "start", "task"},
		{"POST", "/api/v1/tasks/3/cancel", "stop", "task"},
		{"POST", "/api/v1/tasks/3/cleanup", "cleanup", "task"},
		{"POST", "/api/v1/admins/3/roles", "grant_roles", "admin"},
		{"POST", "/api/v1/reports/import", "import", "report"},
		{"PUT", "/api/v1/environments/3/tags", "bind_tags", "environment"},
		{"PATCH", "/api/v1/environments/3/status", "update_status", "environment"},
		{"DELETE", "/api/v1/ssh-hosts/9", "delete", "ssh_host"},
		{"GET", "/api/v1/environment-categories", "read", "environment_category"},
	}
	for _, tc := range cases {
		if got := auditAction(tc.method, tc.path); got != tc.action {
			t.Errorf("auditAction(%s %s) = %q, want %q", tc.method, tc.path, got, tc.action)
		}
		if got := auditResourceType(tc.path); got != tc.resource {
			t.Errorf("auditResourceType(%s) = %q, want %q", tc.path, got, tc.resource)
		}
	}
}

// TestShouldAudit 验证审计范围判定。
func TestShouldAudit(t *testing.T) {
	cases := []struct {
		method, path string
		status       int
		want         bool
	}{
		{"POST", "/api/v1/environments", 201, true},
		{"PATCH", "/api/v1/environments/1/status", 200, true},
		{"GET", "/api/v1/environments", 200, false},
		{"GET", "/api/v1/environments", 403, true},
		{"OPTIONS", "/api/v1/environments", 204, false},
		{"GET", "/healthz", 200, false},
		{"GET", "/static/app.js", 200, false},
	}
	for _, tc := range cases {
		if got := shouldAudit(tc.method, tc.path, tc.status); got != tc.want {
			t.Errorf("shouldAudit(%s %s %d) = %v, want %v", tc.method, tc.path, tc.status, got, tc.want)
		}
	}
}
