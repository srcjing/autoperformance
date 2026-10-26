package middleware

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"
)

// AuditLog 审计日志中间件（占位）：记录请求方法/路径/状态码/耗时。
//
// 正式实现：写入用户日志表（user_log），采用 HMAC 链式防篡改；此处仅打印到日志。
func AuditLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		log.Printf("[audit] %s %s status=%d cost=%s",
			c.Request.Method, c.Request.URL.Path, c.Writer.Status(), time.Since(start))
	}
}
