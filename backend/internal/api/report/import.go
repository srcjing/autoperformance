package report

import (
	"errors"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
	"auto_performance/internal/errcode"
	rptsvc "auto_performance/internal/service/report"
)

// Import POST /api/v1/reports/import（report.list:write）
//
// multipart/form-data：file（必填，html/pdf/docx）+ task_id（可选）+ product_id（可选）。
// 扩展名 + magic bytes 双校验（24002）、大小限制（24003）、随机文件名落盘（DES-T-001）。
func (h *Handler) Import(c *gin.Context) {
	// 请求体硬限制：文件上限 + multipart 边界开销（防止超大请求进入内存/临时目录）。
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, h.maxUpload+importMaxBytes)

	fileHeader, err := c.FormFile("file")
	if err != nil {
		if isBodyTooLarge(err) || errors.Is(err, multipart.ErrMessageTooLarge) {
			response.Error(c, rptsvc.ErrReportTooLarge)
			return
		}
		response.Error(c, errcode.ErrParamMiss)
		return
	}
	if fileHeader.Size <= 0 {
		response.Error(c, rptsvc.ErrReportTypeNotAllowed)
		return
	}
	if fileHeader.Size > h.maxUpload {
		response.Error(c, rptsvc.ErrReportTooLarge)
		return
	}

	taskID, err := parseIDValue(strings.TrimSpace(c.PostForm("task_id")), false)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	productID, err := parseIDValue(strings.TrimSpace(c.PostForm("product_id")), false)
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}

	content, err := fileHeader.Open()
	if err != nil {
		response.Error(c, errcode.ErrParamFmt)
		return
	}
	defer func() { _ = content.Close() }()

	view, err := h.svc.Import(c.Request.Context(), rptsvc.ImportInput{
		Filename:  fileHeader.Filename,
		Size:      fileHeader.Size,
		Content:   content,
		TaskID:    taskID,
		ProductID: productID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, toDTO(view))
}
