package environment

import (
	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
	"auto_performance/internal/errcode"
	envsvc "auto_performance/internal/service/environment"
)

// ListEnvironments GET /api/v1/environments（env.list:read）
//
// 支持 keyword / category_id / tag_ids（逗号分隔，取交集）/ status / page / page_size。
// 无 env.list:read 时响应不含 hosts（DES-I-001 按权限裁剪）。
func (h *Handler) ListEnvironments(c *gin.Context) {
	categoryID, err := parseIDValue(c.Query("category_id"), false)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	tagIDs, err := parseCSVIDs(c.Query("tag_ids"))
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	page, _ := parseIntQuery(c, "page")
	pageSize, _ := parseIntQuery(c, "page_size")

	res, err := h.svc.ListEnvironments(c.Request.Context(), envsvc.ListEnvironmentsQuery{
		Keyword:    c.Query("keyword"),
		CategoryID: categoryID,
		TagIDs:     tagIDs,
		Status:     c.Query("status"),
		Page:       page,
		PageSize:   pageSize,
	})
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

// CreateEnvironment POST /api/v1/environments（env.list:write）
func (h *Handler) CreateEnvironment(c *gin.Context) {
	in, ok := bindEnvironmentInput(c)
	if !ok {
		return
	}
	view, err := h.svc.CreateEnvironment(c.Request.Context(), in)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toEnvironmentDTO(view, canReadHosts(c)))
}

// GetEnvironment GET /api/v1/environments/{id}（env.list:read）
func (h *Handler) GetEnvironment(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	view, err := h.svc.GetEnvironment(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toEnvironmentDTO(view, canReadHosts(c)))
}

// UpdateEnvironment PUT /api/v1/environments/{id}（env.list:write；version 乐观锁 → 10402）
func (h *Handler) UpdateEnvironment(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	in, ok := bindEnvironmentInput(c)
	if !ok {
		return
	}
	view, err := h.svc.UpdateEnvironment(c.Request.Context(), id, in)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toEnvironmentDTO(view, canReadHosts(c)))
}

// DeleteEnvironment DELETE /api/v1/environments/{id}（逻辑删除；testing → 21004）
func (h *Handler) DeleteEnvironment(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	if err := h.svc.DeleteEnvironment(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, nil)
}

// BindTags PUT /api/v1/environments/{id}/tags（env.list:write；全量替换）
func (h *Handler) BindTags(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	var req bindTagsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParam)
		return
	}
	tagIDs, err := parseIDList(req.TagIDs, false)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	if err := h.svc.BindTags(c.Request.Context(), id, tagIDs); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, nil)
}

// UpdateStatus PATCH /api/v1/environments/{id}/status（停用/启用；testing → 21004/10401）
func (h *Handler) UpdateStatus(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	var req envStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParamMiss)
		return
	}
	if err := h.svc.UpdateStatus(c.Request.Context(), id, req.Status); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, nil)
}

// bindEnvironmentInput 绑定并解析环境请求体（含字符串/数字兼容的 ID 字段）。
func bindEnvironmentInput(c *gin.Context) (envsvc.EnvironmentInput, bool) {
	var req environmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParamMiss)
		return envsvc.EnvironmentInput{}, false
	}

	if len(req.Hosts) == 0 {
		// validator 对空数组不生效，显式拦截（主机地址列表必填，PRD §4.3）。
		response.Error(c, errcode.ErrParamMiss)
		return envsvc.EnvironmentInput{}, false
	}

	categoryID, err := parseIDValue(string(req.CategoryID), true)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return envsvc.EnvironmentInput{}, false
	}
	sshHostID, err := parseIDValue(string(req.SshHostID), false)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return envsvc.EnvironmentInput{}, false
	}
	certID, err := parseIDValue(string(req.CertID), false)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return envsvc.EnvironmentInput{}, false
	}
	tagIDs, err := parseIDList(req.TagIDs, false)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return envsvc.EnvironmentInput{}, false
	}

	return envsvc.EnvironmentInput{
		Name:        req.Name,
		Code:        req.Code,
		CategoryID:  categoryID,
		TagIDs:      tagIDsOrNil(req.TagIDs, tagIDs),
		Type:        req.Type,
		Hosts:       req.Hosts,
		OS:          req.OS,
		SshHostID:   sshHostID,
		CertID:      certID,
		Description: req.Description,
		Version:     req.Version,
	}, true
}

// tagIDsOrNil 区分「请求体未携带 tag_ids」（nil，保持原绑定）与「显式空数组」（清空）。
func tagIDsOrNil(raw idList, parsed []int64) []int64 {
	if raw == nil {
		return nil
	}
	return parsed
}
