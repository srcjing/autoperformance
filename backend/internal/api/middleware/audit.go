package middleware

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/auth"
	"auto_performance/internal/domain"
	logsvc "auto_performance/internal/service/log"
)

// auditWriteTimeout 审计落库超时：审计失败不得拖垮业务请求（仅记录告警）。
const auditWriteTimeout = 3 * time.Second

// AuditLog 审计日志中间件（INV-8 / DES-T-006 / DES-R-001）。
//
// 位于 AuthN 之前、响应之后（c.Next() 返回后）执行，因此可读取 AuthN/handler 写入上下文的
// 操作人（admin_id）与最终状态码，将其作为 5W1H 事件经 service/log.AppendAudit 追加到 user_log。
//
// 记录范围：
//   - 全部写请求（POST/PUT/PATCH/DELETE）——覆盖登录/增删改/权限变更/执行/停止；
//   - 失败的读请求（status>=400）——覆盖越权探测与无效访问。
//
// GET 成功请求不落审计表，避免日志查询类读接口自放大（容量/限流见 DES-D-001）。
// svc 为 nil（未初始化 DB）时降级为本地打印。
func AuditLog(svc *logsvc.Service, hmacKey []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		if !shouldAudit(c.Request.Method, c.Request.URL.Path, status) {
			return
		}

		event := buildAuditEvent(c, status, time.Since(start))
		if svc == nil {
			log.Printf("[audit] %s %s status=%d admin=%v（未初始化审计仓储，未落库）",
				event.Action, c.Request.URL.Path, status, event.AdminID)
			return
		}

		// 请求结束后原 ctx 可能已被取消（客户端断开），审计写入需独立于其生命周期。
		ctx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), auditWriteTimeout)
		defer cancel()
		if _, err := svc.AppendAudit(ctx, nil, event, hmacKey); err != nil {
			log.Printf("[audit] 写入审计日志失败: %v path=%s status=%d", err, c.Request.URL.Path, status)
		}
	}
}

// buildAuditEvent 由请求上下文与响应结果构造审计事件（5W1H）。
func buildAuditEvent(c *gin.Context, status int, cost time.Duration) logsvc.AuditEvent {
	result := domain.AuditResultOK
	if status >= 400 {
		result = domain.AuditResultFail
	}
	return logsvc.AuditEvent{
		AdminID:      adminIDFromContext(c),
		Action:       auditAction(c.Request.Method, c.Request.URL.Path),
		ResourceType: auditResourceType(c.Request.URL.Path),
		ResourceID:   c.Param("id"),
		Detail: fmt.Sprintf("%s %s status=%d cost_ms=%d",
			c.Request.Method, c.Request.URL.Path, status, cost.Milliseconds()),
		IP:     c.ClientIP(),
		Result: result,
	}
}

// shouldAudit 判定请求是否进入审计链。
func shouldAudit(method, path string, status int) bool {
	if path == "" || method == "" {
		return false
	}
	if method == "OPTIONS" || path == "/healthz" {
		return false
	}
	if !strings.HasPrefix(path, "/api/v1/") {
		return false
	}
	if method == "GET" || method == "HEAD" {
		// 读请求仅记录失败（越权/探测），避免日志表读放大。
		return status >= 400
	}
	return true
}

// adminIDFromContext 从 AuthN 注入的上下文读取操作人 ID（未认证请求为 nil）。
func adminIDFromContext(c *gin.Context) *int64 {
	v, ok := c.Get(auth.CtxAdminID)
	if !ok {
		return nil
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil
	}
	return &id
}

// auditResourceType 由路径首段推导资源类型（singular snake_case）。
func auditResourceType(path string) string {
	seg := firstPathSegment(path)
	switch seg {
	case "environment-categories":
		return "environment_category"
	case "environments":
		return "environment"
	case "tags":
		return "tag"
	case "products":
		return "product"
	case "interfaces":
		return "interface"
	case "templates":
		return "template"
	case "tasks":
		return "task"
	case "reports":
		return "report"
	case "logs":
		return "log"
	case "admins":
		return "admin"
	case "roles":
		return "role"
	case "permissions":
		return "permission"
	case "certificates":
		return "certificate"
	case "ssh-hosts":
		return "ssh_host"
	case "auth":
		return "auth"
	default:
		return seg
	}
}

// auditAction 由方法 + 路径推导动作枚举（便于按动作检索）。
func auditAction(method, path string) string {
	if strings.HasPrefix(path, "/api/v1/auth/") {
		return strings.TrimPrefix(path, "/api/v1/auth/")
	}
	if action := pathSuffixAction(path); action != "" {
		return action
	}
	switch method {
	case "POST":
		return "create"
	case "PUT", "PATCH":
		return "update"
	case "DELETE":
		return "delete"
	case "GET", "HEAD":
		return "read"
	default:
		return strings.ToLower(method)
	}
}

// pathSuffixAction 识别子资源动作（执行/停止/授权等），优先于通用 CRUD 动作。
func pathSuffixAction(path string) string {
	actions := []struct{ suffix, action string }{
		{"/start", "start"},
		{"/cancel", "stop"},
		{"/retry", "retry"},
		{"/cleanup", "cleanup"},
		{"/reset-password", "reset_password"},
		{"/generate", "generate"},
		{"/export", "export"},
		{"/artifact", "view_artifact"},
		{"/import", "import"},
		{"/reinit", "reinit"},
		{"/roles", "grant_roles"},
		{"/tags", "bind_tags"},
		{"/status", "update_status"},
	}
	for _, a := range actions {
		if strings.HasSuffix(path, a.suffix) {
			return a.action
		}
	}
	return ""
}

// firstPathSegment 返回 /api/v1/<seg>/... 中的 <seg>。
func firstPathSegment(path string) string {
	trimmed := strings.TrimPrefix(path, "/api/v1/")
	if i := strings.IndexByte(trimmed, '/'); i >= 0 {
		return trimmed[:i]
	}
	return trimmed
}
