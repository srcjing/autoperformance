package middleware

import (
	"context"
	"errors"
	"log"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
	"auto_performance/internal/auth"
	"auto_performance/internal/errcode"
	authsvc "auto_performance/internal/service/auth"
)

// Authenticator 认证服务能力（由 service/auth.Service 实现；测试可注入假实现）。
type Authenticator interface {
	// Principal 加载请求主体的鉴权快照（账号状态实时校验 + 权限并集缓存）。
	Principal(ctx context.Context, adminID int64) (*authsvc.Principal, error)
	// IsRevoked 判断 jti 是否已失效（登出/改密后的旧 token 重放）。
	IsRevoked(jti string) bool
}

// mustChangePasswordAllowlist 首登强制改密期间允许访问的接口（其余返回 10103）。
var mustChangePasswordAllowlist = map[string]bool{
	"/api/v1/auth/change-password": true,
	"/api/v1/auth/logout":          true,
	"/api/v1/auth/me":              true,
}

// AuthN JWT 认证中间件（DES-S-001 / DES-S-006 / INV-1）。
//
// 校验 Authorization: Bearer <token> 后：
//  1. 解析并校验 jti / exp（过期或签名非法 → 10101）；
//  2. jti 在失效名单内 → 10102（登出后重放）；
//  3. 从 service/auth 加载主体：账号停用 → 10202；账号不存在/已删除 → 10101；
//  4. must_change_password=true 时仅放行改密/登出/当前用户接口，其余 → 10103；
//  5. 注入上下文：admin_id/username/权限并集/jti/exp，供 AuthZ、审计与 handler 使用。
//
// 权限不写入 JWT，每次请求由服务端从 DB（带短 TTL 缓存）读取，前端隐藏不可绕过。
func AuthN(secret string, svc Authenticator) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractToken(c)
		if token == "" {
			abort(c, errcode.ErrUnauth)
			return
		}

		claims, err := auth.ParseToken(token, secret)
		if err != nil {
			// 过期/格式非法/签名不匹配统一返回未认证，避免泄露 token 校验细节。
			abort(c, errcode.ErrUnauth)
			return
		}

		if svc == nil {
			log.Printf("[authn] 认证服务未初始化，拒绝请求 path=%s", c.Request.URL.Path)
			abort(c, errcode.ErrDependency)
			return
		}

		if svc.IsRevoked(claims.KeyID()) {
			abort(c, errcode.ErrSession)
			return
		}

		adminID, err := strconv.ParseInt(claims.Subject, 10, 64)
		if err != nil {
			abort(c, errcode.ErrUnauth)
			return
		}

		principal, err := svc.Principal(c.Request.Context(), adminID)
		if err != nil {
			switch {
			case errors.Is(err, errcode.ErrDisabled):
				abort(c, errcode.ErrDisabled)
			case errors.Is(err, errcode.ErrNotFound):
				// 账号已删除/不存在：视为未认证（不暴露账号是否曾存在）。
				abort(c, errcode.ErrUnauth)
			default:
				log.Printf("[authn] 加载鉴权快照失败 admin_id=%d: %v", adminID, err)
				abort(c, errcode.ErrDependency)
			}
			return
		}

		if principal.MustChangePassword && !mustChangePasswordAllowlist[c.Request.URL.Path] {
			abort(c, errcode.ErrMustPwd)
			return
		}

		c.Set(auth.CtxAdminID, strconv.FormatInt(principal.AdminID, 10))
		c.Set(auth.CtxUsername, principal.Username)
		c.Set(auth.CtxPerms, principal.Permissions)
		c.Set(auth.CtxMustPwd, principal.MustChangePassword)
		c.Set(auth.CtxTokenID, claims.KeyID())
		if claims.ExpiresAt != nil {
			c.Set(auth.CtxTokenExp, claims.ExpiresAt.Time)
		}
		c.Next()
	}
}

// abort 输出统一错误响应并终止链路。
func abort(c *gin.Context, e *errcode.Error) {
	response.Error(c, e)
	c.Abort()
}

// extractToken 从 Authorization 头提取 Bearer token。
func extractToken(c *gin.Context) string {
	h := c.GetHeader("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return strings.TrimSpace(parts[1])
	}
	return ""
}

// 编译期断言：service/auth.Service 满足中间件依赖（避免签名漂移）。
var _ Authenticator = (*authsvc.Service)(nil)
