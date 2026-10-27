package product

import (
	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
	"auto_performance/internal/errcode"
	productsvc "auto_performance/internal/service/product"
)

// ListThresholds GET /api/v1/interfaces/{id}/thresholds（product.interface:read）
func (h *Handler) ListThresholds(c *gin.Context) {
	interfaceID, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	items, err := h.svc.ListThresholds(c.Request.Context(), interfaceID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, gin.H{"list": toThresholdListDTO(items)})
}

// ReplaceThresholds PUT /api/v1/interfaces/{id}/thresholds（product.interface:write；全量替换）
//
// 指标与方向非法组合 → 22003（INV-2 / PRD §9.3）。
func (h *Handler) ReplaceThresholds(c *gin.Context) {
	interfaceID, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	var req thresholdsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParam)
		return
	}

	inputs := make([]productsvc.ThresholdInput, 0, len(req.Thresholds))
	for _, item := range req.Thresholds {
		inputs = append(inputs, productsvc.ThresholdInput{
			Metric:   item.Metric,
			Operator: item.Operator,
			Value:    item.Value,
			Unit:     item.Unit,
			Enabled:  item.Enabled,
		})
	}

	saved, err := h.svc.ReplaceThresholds(c.Request.Context(), interfaceID, inputs)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, gin.H{"list": toThresholdListDTO(saved)})
}
