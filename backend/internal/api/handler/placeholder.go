package handler

import (
	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
)

// PlaceholderList 占位列表接口（骨架演示，返回空分页结构）。
// 用于演示完整中间件链（AuthN + AuthZ）已正确串联，后续替换为各模块真实 handler。
func (h *Handler) PlaceholderList(c *gin.Context) {
	response.OK(c, gin.H{
		"list":      []interface{}{},
		"total":     0,
		"page":      1,
		"page_size": 20,
	})
}
