package product

import (
	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
	"auto_performance/internal/errcode"
	productsvc "auto_performance/internal/service/product"
)

// ListProducts GET /api/v1/products（product.list:read）
//
// 支持 keyword / product_version / platform / status / page / page_size。
func (h *Handler) ListProducts(c *gin.Context) {
	page, _ := parseIntQuery(c, "page")
	pageSize, _ := parseIntQuery(c, "page_size")

	res, err := h.svc.ListProducts(c.Request.Context(), productsvc.ListProductsQuery{
		Keyword:        c.Query("keyword"),
		ProductVersion: c.Query("product_version"),
		Platform:       c.Query("platform"),
		Status:         c.Query("status"),
		Page:           page,
		PageSize:       pageSize,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, gin.H{
		"list":      toProductListDTO(res.List),
		"total":     res.Total,
		"page":      res.Page,
		"page_size": res.PageSize,
	})
}

// CreateProduct POST /api/v1/products（product.list:write；编码重复 → 22001）
func (h *Handler) CreateProduct(c *gin.Context) {
	in, ok := bindProductInput(c)
	if !ok {
		return
	}
	view, err := h.svc.CreateProduct(c.Request.Context(), in)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toProductDTO(view))
}

// GetProduct GET /api/v1/products/{id}（product.list:read）
func (h *Handler) GetProduct(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	view, err := h.svc.GetProduct(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toProductDTO(view))
}

// UpdateProduct PUT /api/v1/products/{id}（product.list:write；version 乐观锁 → 10402）
func (h *Handler) UpdateProduct(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	in, ok := bindProductInput(c)
	if !ok {
		return
	}
	view, err := h.svc.UpdateProduct(c.Request.Context(), id, in)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toProductDTO(view))
}

// DeleteProduct DELETE /api/v1/products/{id}（逻辑删除；下有接口 → 22002）
func (h *Handler) DeleteProduct(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	if err := h.svc.DeleteProduct(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, nil)
}

// UpdateStatus PATCH /api/v1/products/{id}/status（停用/启用；同状态 → 10401）
func (h *Handler) UpdateStatus(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	var req productStatusRequest
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

// bindProductInput 绑定并解析产品请求体。
func bindProductInput(c *gin.Context) (productsvc.ProductInput, bool) {
	var req productRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParamMiss)
		return productsvc.ProductInput{}, false
	}
	return productsvc.ProductInput{
		Name:           req.Name,
		Code:           req.Code,
		ProductVersion: req.ProductVersion,
		Platform:       req.Platform,
		Description:    req.Description,
		Version:        req.Version,
	}, true
}
