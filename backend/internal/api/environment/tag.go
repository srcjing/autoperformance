package environment

import (
	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
	"auto_performance/internal/errcode"
	envsvc "auto_performance/internal/service/environment"
)

// ListTags GET /api/v1/tags（env.tag:read；含关联环境数）
func (h *Handler) ListTags(c *gin.Context) {
	tags, err := h.svc.ListTags(c.Request.Context(), envsvc.ListTagsQuery{Keyword: c.Query("keyword")})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	list := make([]tagDTO, 0, len(tags))
	for i := range tags {
		list = append(list, toTagDTO(&tags[i]))
	}
	response.OK(c, gin.H{"list": list})
}

// CreateTag POST /api/v1/tags（env.tag:write）
func (h *Handler) CreateTag(c *gin.Context) {
	var req tagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParamMiss)
		return
	}
	view, err := h.svc.CreateTag(c.Request.Context(), envsvc.TagInput{
		Name:        req.Name,
		Color:       req.Color,
		Description: req.Description,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toTagDTO(view))
}

// UpdateTag PUT /api/v1/tags/{id}（env.tag:write）
func (h *Handler) UpdateTag(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	var req tagRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParamMiss)
		return
	}
	view, err := h.svc.UpdateTag(c.Request.Context(), id, envsvc.TagInput{
		Name:        req.Name,
		Color:       req.Color,
		Description: req.Description,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toTagDTO(view))
}

// DeleteTag DELETE /api/v1/tags/{id}（被引用 → 21003）
func (h *Handler) DeleteTag(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	if err := h.svc.DeleteTag(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, nil)
}

// TagEnvironments GET /api/v1/tags/{id}/environments（env.tag:read；标签关联环境反查）
func (h *Handler) TagEnvironments(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	page, _ := parseIntQuery(c, "page")
	pageSize, _ := parseIntQuery(c, "page_size")

	res, err := h.svc.TagEnvironments(c.Request.Context(), id, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, gin.H{
		"list":      toEnvironmentListDTO(res.List, canReadHosts(c)),
		"total":     res.Total,
		"page":      res.Page,
		"page_size": res.PageSize,
	})
}
