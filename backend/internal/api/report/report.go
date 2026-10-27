package report

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
	rptsvc "auto_performance/internal/service/report"
)

// ListReports GET /api/v1/reports（report.list:read）
//
// 筛选：task_id / product_id / type(html|pdf|word) / status / source / keyword /
// start_time|end_time（兼容 created_from|created_to）+ 分页。
func (h *Handler) ListReports(c *gin.Context) {
	taskID, err := parseIDValue(c.Query("task_id"), false)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	productID, err := parseIDValue(c.Query("product_id"), false)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	from, err := parseTimeQuery(c, "start_time", "created_from")
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	to, err := parseTimeQuery(c, "end_time", "created_to")
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	page, _ := parseIntQuery(c, "page")
	pageSize, _ := parseIntQuery(c, "page_size")

	res, err := h.svc.List(c.Request.Context(), rptsvc.ListQuery{
		TaskID:      taskID,
		ProductID:   productID,
		Type:        c.Query("type"),
		Status:      c.Query("status"),
		Source:      c.Query("source"),
		Keyword:     c.Query("keyword"),
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
		"list":      toListDTO(res.List),
		"total":     res.Total,
		"page":      res.Page,
		"page_size": res.PageSize,
	})
}

// GetReport GET /api/v1/reports/{id}（report.list:read；不存在 10301）
func (h *Handler) GetReport(c *gin.Context) {
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
	response.OK(c, toDTO(view))
}

// GenerateReport POST /api/v1/reports/{id}/generate（report.list:write）
//
// 语义：报告状态 generation_failed → generating 的重新生成；生成中重复调用 → 10401，
// 生成失败 → 24004（报告状态置 generation_failed，可再次重试）。
func (h *Handler) GenerateReport(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	view, err := h.svc.Regenerate(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toDTO(view))
}

// GetArtifact GET /api/v1/reports/{id}/artifact?format=html|pdf|word（report.list:read）
//
// html/pdf 内联预览，word 作为附件下载；文件不存在/越界一律 24001。
func (h *Handler) GetArtifact(c *gin.Context) {
	h.serveArtifact(c, false)
}

// ExportReport GET /api/v1/reports/{id}/export?format=html|pdf|word（report.list:read）
//
// 导出/离线读取：一律 Content-Disposition: attachment。
func (h *Handler) ExportReport(c *gin.Context) {
	h.serveArtifact(c, true)
}

// serveArtifact 下发报告产物（路径已由服务层做白名单目录校验，DES-T-002）。
func (h *Handler) serveArtifact(c *gin.Context, forceAttachment bool) {
	id, err := parseID(c)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	format := strings.ToLower(strings.TrimSpace(c.Query("format")))
	if format == "" {
		format = domain.ReportFormatHTML
	}
	file, err := h.svc.Artifact(c.Request.Context(), id, format)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}

	disposition := "inline"
	if forceAttachment || !file.Inline {
		disposition = "attachment"
	}
	c.Header("Content-Type", file.ContentType)
	c.Header("Content-Disposition", disposition+"; filename=\""+file.Filename+"\"")
	// 禁止浏览器猜测类型；HTML 产物按沙箱文档渲染（阻断导入报告内脚本执行）。
	c.Header("X-Content-Type-Options", "nosniff")
	if file.Format == domain.ReportFormatHTML {
		c.Header("Content-Security-Policy", "sandbox; default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:")
	}
	c.Header("Cache-Control", "private, no-store")
	c.Status(http.StatusOK)
	c.File(file.Path)
}

// importMaxBytes 兼容 multipart 边界开销（文件限制之外再留 1MB 余量）。
const importMaxBytes int64 = 1 << 20

// isBodyTooLarge 判断是否触发 http.MaxBytesReader 上限。
func isBodyTooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}
