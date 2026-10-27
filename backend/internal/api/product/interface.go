package product

import (
	"encoding/json"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
	productsvc "auto_performance/internal/service/product"
)

// ListInterfaces GET /api/v1/products/{id}/interfaces（product.interface:read）
//
// 支持 keyword / status / page / page_size；响应不含认证头明文（仅 auth_headers 名称）。
func (h *Handler) ListInterfaces(c *gin.Context) {
	productID, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	page, _ := parseIntQuery(c, "page")
	pageSize, _ := parseIntQuery(c, "page_size")

	res, err := h.svc.ListInterfaces(c.Request.Context(), productsvc.ListInterfacesQuery{
		ProductID: productID,
		Keyword:   c.Query("keyword"),
		Status:    c.Query("status"),
		Page:      page,
		PageSize:  pageSize,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, gin.H{
		"list":      toInterfaceListDTO(res.List),
		"total":     res.Total,
		"page":      res.Page,
		"page_size": res.PageSize,
	})
}

// CreateInterface POST /api/v1/products/{id}/interfaces（product.interface:write）
func (h *Handler) CreateInterface(c *gin.Context) {
	productID, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	in, ok := bindInterfaceInput(c)
	if !ok {
		return
	}
	view, err := h.svc.CreateInterface(c.Request.Context(), productID, in)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toInterfaceDTO(view))
}

// UpdateInterface PUT /api/v1/interfaces/{id}（product.interface:write；version 乐观锁 → 10402）
func (h *Handler) UpdateInterface(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	in, ok := bindInterfaceInput(c)
	if !ok {
		return
	}
	view, err := h.svc.UpdateInterface(c.Request.Context(), id, in)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toInterfaceDTO(view))
}

// DeleteInterface DELETE /api/v1/interfaces/{id}（逻辑删除）
func (h *Handler) DeleteInterface(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	if err := h.svc.DeleteInterface(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, nil)
}

// UpdateInterfaceStatus PATCH /api/v1/interfaces/{id}/status（停用/启用；同状态 → 10401）
func (h *Handler) UpdateInterfaceStatus(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	var req interfaceStatusRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParamMiss)
		return
	}
	if err := h.svc.UpdateInterfaceStatus(c.Request.Context(), id, req.Status); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, nil)
}

// bindInterfaceInput 绑定并解析接口请求体（body 为任意 JSON 值，转为 JSON 文本保存）。
func bindInterfaceInput(c *gin.Context) (productsvc.InterfaceInput, bool) {
	var req interfaceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParamMiss)
		return productsvc.InterfaceInput{}, false
	}

	body := ""
	if len(req.Body) > 0 && string(req.Body) != "null" {
		if !json.Valid(req.Body) {
			response.Error(c, errcode.ErrParamFmt)
			return productsvc.InterfaceInput{}, false
		}
		body = string(req.Body)
	}

	timeouts := domain.InterfaceTimeouts{}
	if req.Timeouts != nil {
		timeouts = domain.InterfaceTimeouts{
			ConnectMS: req.Timeouts.ConnectMS,
			ReadMS:    req.Timeouts.ReadMS,
			TotalMS:   req.Timeouts.TotalMS,
		}
	}

	return productsvc.InterfaceInput{
		Name:        req.Name,
		Method:      req.Method,
		Scheme:      req.Scheme,
		Port:        req.Port,
		Path:        req.Path,
		Headers:     req.Headers,
		Body:        body,
		Timeouts:    timeouts,
		Description: req.Description,
		Version:     req.Version,
	}, true
}
