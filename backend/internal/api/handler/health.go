package handler

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
)

// Health 健康检查 GET /healthz（公开，不经过鉴权）。
func (h *Handler) Health(c *gin.Context) {
	dbStatus := "not_configured"
	if h.db != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := h.db.Ping(ctx); err != nil {
			dbStatus = "down"
		} else {
			dbStatus = "up"
		}
	}

	response.OK(c, gin.H{
		"status": "ok",
		"db":     dbStatus,
	})
}
