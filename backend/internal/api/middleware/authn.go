package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
	"auto_performance/internal/auth"
	"auto_performance/internal/errcode"
)

// AuthN JWT 认证中间件（骨架占位实现）。
//
// 校验 Authorization: Bearer <token>，解析 admin_id/username 并注入上下文；
// 权限集合不从 token 读取，而是注入占位全量权限（正式从 DB 实时/缓存读取）。
func AuthN(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := extractToken(c)
		if token == "" {
			response.Error(c, errcode.ErrUnauth)
			c.Abort()
			return
		}

		claims, err := auth.ParseToken(token, secret)
		if err != nil {
			response.Error(c, errcode.ErrUnauth)
			c.Abort()
			return
		}

		c.Set(auth.CtxAdminID, claims.Subject)
		c.Set(auth.CtxUsername, claims.Username)
		c.Set(auth.CtxPerms, auth.SkeletonPermissions())
		c.Next()
	}
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
