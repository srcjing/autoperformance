package task

import (
	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
	"auto_performance/internal/errcode"
	testsvc "auto_performance/internal/service/testing"
)

// CreateTask POST /api/v1/tasks（testing.task:write）
//
// 五要素校验：接口范围空 → 23003；并发数非法/超上限 → 23004；环境占用 → 23002。
// auto_start=true 时创建成功后立即执行（默认 false，需显式 start）。
func (h *Handler) CreateTask(c *gin.Context) {
	var req taskRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, errcode.ErrParamMiss)
		return
	}
	templateID, err := parseIDValue(string(req.TemplateID), false)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	envID, err := parseIDValue(string(req.EnvironmentID), false)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	productIDs, err := parseIDList(req.ProductIDs, false)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	interfaceIDs, err := parseIDList(req.InterfaceIDs, false)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}

	view, err := h.svc.Create(c.Request.Context(), testsvc.CreateTaskInput{
		Name:         req.Name,
		TemplateID:   templateID,
		EnvID:        envID,
		TargetHost:   req.TargetHost,
		ProductIDs:   productIDs,
		InterfaceIDs: interfaceIDs,
		Flow:         req.Flow,
		ReportConfig: req.ReportConfig,
		CleanConfig:  req.CleanConfig,
		CreatedBy:    currentAdminID(c),
		AutoStart:    req.AutoStart,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toTaskDTO(view))
}

// ListTasks GET /api/v1/tasks（testing.task:read）
//
// 支持 keyword / status / env_id / product_id / template_id / created_from / created_to / page / page_size。
func (h *Handler) ListTasks(c *gin.Context) {
	envID, err := parseIDValue(c.Query("env_id"), false)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	productID, err := parseIDValue(c.Query("product_id"), false)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	templateID, err := parseIDValue(c.Query("template_id"), false)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	from, err := parseTimeQuery(c, "created_from")
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	to, err := parseTimeQuery(c, "created_to")
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	page, _ := parseIntQuery(c, "page")
	pageSize, _ := parseIntQuery(c, "page_size")

	res, err := h.svc.List(c.Request.Context(), testsvc.ListTasksQuery{
		Keyword:     c.Query("keyword"),
		Status:      c.Query("status"),
		EnvID:       envID,
		ProductID:   productID,
		TemplateID:  templateID,
		CreatedFrom: from,
		CreatedTo:   to,
		Page:        page,
		PageSize:    pageSize,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, gin.H{
		"list":      toTaskListDTO(res.List),
		"total":     res.Total,
		"page":      res.Page,
		"page_size": res.PageSize,
	})
}

// GetTask GET /api/v1/tasks/{id}（testing.task:read）
func (h *Handler) GetTask(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	view, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toTaskDTO(view))
}

// StartTask POST /api/v1/tasks/{id}/start（testing.task:write；pending → running）
func (h *Handler) StartTask(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	view, err := h.svc.Start(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toTaskDTO(view))
}

// RefreshSnapshotTask POST /api/v1/tasks/{id}/refresh-snapshot（testing.task:write；仅 pending）
func (h *Handler) RefreshSnapshotTask(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	view, err := h.svc.RefreshSnapshot(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toTaskDTO(view))
}

// CancelTask POST /api/v1/tasks/{id}/cancel（testing.task:write；running → cancelled → cleanup_pending）
func (h *Handler) CancelTask(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	view, err := h.svc.Cancel(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toTaskDTO(view))
}

// RetryTask POST /api/v1/tasks/{id}/retry（testing.task:write；failed/cleanup_pending → pending）
func (h *Handler) RetryTask(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	view, err := h.svc.Retry(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toTaskDTO(view))
}

// CleanupTask POST /api/v1/tasks/{id}/cleanup（testing.task:write；仅 cleanup_pending）
func (h *Handler) CleanupTask(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	view, err := h.svc.Cleanup(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toTaskDTO(view))
}
