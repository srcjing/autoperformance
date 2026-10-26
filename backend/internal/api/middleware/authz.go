package middleware

import (
	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
	"auto_performance/internal/auth"
	"auto_performance/internal/errcode"
)

// AuthZ RBAC 鉴权中间件（骨架占位实现）。
//
// 校验当前请求是否具备 module_code 的 read/write 权限（集合包含判断，write 蕴含 read）。
// 用法：group.GET("/path", AuthZ("env.list", "read"), handler)。
func AuthZ(module, perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		v, _ := c.Get(auth.CtxPerms)
		perms, _ := v.(map[string]bool)
		if !auth.HasPerm(perms, module, perm) {
			response.Error(c, errcode.ErrForbidden)
			c.Abort()
			return
		}
		c.Next()
	}
}
