// Package api 负责 REST 路由与 handler 装配（接入层）。
//
// 中间件链（对齐 architecture.md §5.3.2）：
//
//	Recovery → CORS → AuditLog → AuthN(JWT) → AuthZ(RBAC) → Handler
package api

import (
	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/handler"
	"auto_performance/internal/api/middleware"
	"auto_performance/internal/api/response"
	"auto_performance/internal/config"
	"auto_performance/internal/errcode"
)

// NewRouter 装配路由与中间件链。
func NewRouter(cfg *config.Config, db handler.Pinger) *gin.Engine {
	if cfg.Server.Mode == gin.ReleaseMode {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()

	// 全局中间件链
	r.Use(middleware.Recovery())
	r.Use(middleware.CORS())
	r.Use(middleware.AuditLog())

	h := handler.New(db, cfg)

	// 健康检查（公开，不经过鉴权）
	r.GET("/healthz", h.Health)

	// 业务路由组（统一前缀 /api/v1）
	apiV1 := r.Group("/api/v1")
	{
		// 认证模块
		authGroup := apiV1.Group("/auth")
		{
			authGroup.POST("/login", h.Login) // 公开
			authGroup.Use(middleware.AuthN(cfg.JWT.Secret))
			authGroup.POST("/logout", h.Logout)
			authGroup.POST("/change-password", h.ChangePassword)
			authGroup.GET("/me", h.Me)
		}

		// 受 RBAC 保护的业务路由（骨架演示，后续按模块填充真实 handler）
		protected := apiV1.Group("")
		protected.Use(middleware.AuthN(cfg.JWT.Secret))
		{
			protected.GET("/environments", middleware.AuthZ("env.list", "read"), h.PlaceholderList)
			protected.GET("/products", middleware.AuthZ("product.list", "read"), h.PlaceholderList)
			protected.GET("/templates", middleware.AuthZ("testing.template", "read"), h.PlaceholderList)
			protected.GET("/tasks", middleware.AuthZ("testing.task", "read"), h.PlaceholderList)
			protected.GET("/reports", middleware.AuthZ("report.list", "read"), h.PlaceholderList)
			protected.GET("/logs/user", middleware.AuthZ("log.user", "read"), h.PlaceholderList)
			protected.GET("/roles", middleware.AuthZ("setting.admin", "write"), h.PlaceholderList)
		}
	}

	// 兜底 404（统一响应）
	r.NoRoute(func(c *gin.Context) {
		response.Error(c, errcode.ErrNotFound)
	})

	return r
}
