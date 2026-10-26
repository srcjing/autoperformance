package middleware

import (
	"log"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
	"auto_performance/internal/errcode"
)

// Recovery 恢复中间件：捕获 panic，返回统一 500 响应（errcode.ErrInternal）。
// 位于中间件链首位。
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("panic recovered: %v", r)
				if !c.Writer.Written() {
					response.Error(c, errcode.ErrInternal)
				}
				c.Abort()
			}
		}()
		c.Next()
	}
}
