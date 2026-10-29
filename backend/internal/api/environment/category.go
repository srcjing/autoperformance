package environment

import (
	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
	"auto_performance/internal/errcode"
	envsvc "auto_performance/internal/service/environment"
)

// ListCategories GET /api/v1/environment-categories（env.list:read）
func (h *Handler) ListCategories(c *gin.Context) {
	categories, err := h.svc.ListCategories(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	list := make([]categoryDTO, 0, len(categories))
	for i := range categories {
		list = append(list, toCategoryDTO(&categories[i]))
	}
	response.OK(c, gin.H{"list": list})
}

// CreateCategory POST /api/v1/environment-categories（env.list:write）
func (h *Handler) CreateCategory(c *gin.Context) {
	var req categoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParamMiss)
		return
	}
	view, err := h.svc.CreateCategory(c.Request.Context(), envsvc.CategoryInput{
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toCategoryDTO(view))
}

// UpdateCategory PUT /api/v1/environment-categories/{id}（env.list:write）
func (h *Handler) UpdateCategory(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	var req categoryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParamMiss)
		return
	}
	view, err := h.svc.UpdateCategory(c.Request.Context(), id, envsvc.CategoryInput{
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toCategoryDTO(view))
}

// DeleteCategory DELETE /api/v1/environment-categories/{id}（被引用 → 21002）
func (h *Handler) DeleteCategory(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	if err := h.svc.DeleteCategory(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, nil)
}
