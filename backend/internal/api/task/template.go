package task

import (
	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
	"auto_performance/internal/errcode"
	testsvc "auto_performance/internal/service/testing"
)

// ListTemplates GET /api/v1/templates（testing.template:read）
func (h *Handler) ListTemplates(c *gin.Context) {
	page, _ := parseIntQuery(c, "page")
	pageSize, _ := parseIntQuery(c, "page_size")

	res, err := h.svc.ListTemplates(c.Request.Context(), testsvc.ListTemplatesQuery{
		Keyword:  c.Query("keyword"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, gin.H{
		"list":      toTemplateListDTO(res.List),
		"total":     res.Total,
		"page":      res.Page,
		"page_size": res.PageSize,
	})
}

// CreateTemplate POST /api/v1/templates（testing.template:write）
func (h *Handler) CreateTemplate(c *gin.Context) {
	in, ok := bindTemplateInput(c)
	if !ok {
		return
	}
	view, err := h.svc.CreateTemplate(c.Request.Context(), in)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toTemplateDTO(view))
}

// GetTemplate GET /api/v1/templates/{id}（testing.template:read）
func (h *Handler) GetTemplate(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	view, err := h.svc.GetTemplate(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toTemplateDTO(view))
}

// UpdateTemplate PUT /api/v1/templates/{id}（testing.template:write；version 乐观锁）
func (h *Handler) UpdateTemplate(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	in, ok := bindTemplateInput(c)
	if !ok {
		return
	}
	view, err := h.svc.UpdateTemplate(c.Request.Context(), id, in)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toTemplateDTO(view))
}

// DeleteTemplate DELETE /api/v1/templates/{id}（testing.template:write）
func (h *Handler) DeleteTemplate(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	if err := h.svc.DeleteTemplate(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, nil)
}

// bindTemplateInput 绑定模板请求体（ID 列表兼容字符串/数字）。
func bindTemplateInput(c *gin.Context) (testsvc.CreateTemplateInput, bool) {
	var req templateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParamMiss)
		return testsvc.CreateTemplateInput{}, false
	}
	envIDs, err := parseIDList(req.EnvIDs, false)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return testsvc.CreateTemplateInput{}, false
	}
	interfaceIDs, err := parseIDList(req.InterfaceIDs, false)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return testsvc.CreateTemplateInput{}, false
	}
	return testsvc.CreateTemplateInput{
		Name:         req.Name,
		EnvIDs:       envIDs,
		InterfaceIDs: interfaceIDs,
		Flow:         req.Flow,
		ReportConfig: req.ReportConfig,
		CleanConfig:  req.CleanConfig,
		Description:  req.Description,
		Version:      req.Version,
	}, true
}
