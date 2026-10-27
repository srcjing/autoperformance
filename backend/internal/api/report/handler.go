// Package report 报告管理 HTTP handler（列表/详情/生成/在线查看/导出/导入）。
//
// 对齐 api-contract §2.5 与 §5.2：
//   - 路由权限点：report.list:read（查询/查看/导出）、report.list:write（重新生成/导入）；
//   - 错误码：24001 文件不存在 / 24002 类型不允许 / 24003 过大 / 24004 生成失败；
//   - ID 一律以字符串输出（api-contract §1.7），请求体/query 兼容字符串/数字 ID；
//   - 产物只经鉴权接口下发，不暴露文件系统 URL（api-contract §1.6）。
package report

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
	rptsvc "auto_performance/internal/service/report"
)

// DefaultMaxUpload 导入大小上限缺省值（配置项 report.max_size_bytes）。
const DefaultMaxUpload int64 = 50 * 1024 * 1024

// Service 报告管理业务服务能力（*rptsvc.Service 实现；便于 handler 测试注入桩）。
type Service interface {
	List(ctx context.Context, q rptsvc.ListQuery) (*rptsvc.Page, error)
	Get(ctx context.Context, id int64) (*rptsvc.View, error)
	Regenerate(ctx context.Context, id int64) (*rptsvc.View, error)
	Artifact(ctx context.Context, id int64, format string) (*rptsvc.ArtifactFile, error)
	Import(ctx context.Context, in rptsvc.ImportInput) (*rptsvc.View, error)
}

// Handler 报告管理 HTTP handler。
type Handler struct {
	svc       Service
	maxUpload int64
}

// New 构造报告 handler（maxUpload <=0 时取默认 50MB，用作 multipart 请求体硬限制）。
func New(svc Service, maxUpload int64) *Handler {
	if maxUpload <= 0 {
		maxUpload = DefaultMaxUpload
	}
	return &Handler{svc: svc, maxUpload: maxUpload}
}

// 编译期断言：业务服务满足 handler 依赖（避免签名漂移）。
var _ Service = (*rptsvc.Service)(nil)

// ===================== DTO =====================

// artifactDTO 产物元信息（不含文件系统绝对路径之外的任何能力；下载仍走鉴权接口）。
type artifactDTO struct {
	Format      string `json:"format"`
	Path        string `json:"path"`
	Size        int64  `json:"size"`
	Inline      bool   `json:"inline"`
	ContentType string `json:"content_type"`
	CreatedAt   string `json:"created_at"`
}

// summaryItemDTO 逐条阈值判定（接口 ID 转字符串）。
type summaryItemDTO struct {
	InterfaceID   string  `json:"interface_id"`
	InterfaceName string  `json:"interface_name,omitempty"`
	Metric        string  `json:"metric"`
	Operator      string  `json:"operator"`
	Threshold     float64 `json:"threshold"`
	Actual        float64 `json:"actual"`
	HasActual     bool    `json:"has_actual"`
	Pass          bool    `json:"pass"`
	Reason        string  `json:"reason,omitempty"`
}

// summaryDTO 报告摘要（达标统计 + 指标 + 逐条判定 + 导入元信息）。
type summaryDTO struct {
	State            string             `json:"state"`
	Pass             bool               `json:"pass"`
	TotalInterfaces  int                `json:"total_interfaces"`
	PassedInterfaces int                `json:"passed_interfaces"`
	FailedInterfaces int                `json:"failed_interfaces"`
	PassRate         float64            `json:"pass_rate"`
	TotalThresholds  int                `json:"total_thresholds"`
	PassedThresholds int                `json:"passed_thresholds"`
	FailedThresholds int                `json:"failed_thresholds"`
	DurationSeconds  float64            `json:"duration_seconds"`
	Metrics          map[string]float64 `json:"metrics,omitempty"`
	Items            []summaryItemDTO   `json:"items"`
	MissingMetrics   []string           `json:"missing_metrics,omitempty"`
	OriginalFilename string             `json:"original_filename,omitempty"`
	SHA256           string             `json:"sha256,omitempty"`
	SizeBytes        int64              `json:"size_bytes,omitempty"`
}

// reportDTO 报告响应。
type reportDTO struct {
	ID        string        `json:"id"`
	TaskID    string        `json:"task_id,omitempty"`
	ProductID string        `json:"product_id,omitempty"`
	Source    string        `json:"source"`
	Type      string        `json:"type"`
	Title     string        `json:"title"`
	Status    string        `json:"status"`
	Summary   *summaryDTO   `json:"summary,omitempty"`
	Artifacts []artifactDTO `json:"artifacts"`
	CreatedAt string        `json:"created_at"`
	UpdatedAt string        `json:"updated_at"`
}

// ===================== 解析 / 转换 =====================

// parseID 解析路径参数 :id（必须为正整数）。
func parseID(c *gin.Context) (int64, error) {
	return parseIDValue(c.Param("id"), true)
}

// parseIDValue 解析 bigint 字符串；required 为 true 时 0/空视为非法。
func parseIDValue(raw string, required bool) (int64, error) {
	if raw == "" {
		if required {
			return 0, errcode.ErrParamFmt
		}
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 0 || (required && id == 0) {
		return 0, errcode.ErrParamFmt
	}
	return id, nil
}

// parseIntQuery 解析整型 query 参数（缺失/非法返回 0,false）。
func parseIntQuery(c *gin.Context, key string) (int, bool) {
	raw := c.Query(key)
	if raw == "" {
		return 0, false
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return v, true
}

// parseTimeQuery 解析 RFC3339 时间参数（多候选键，缺失返回 nil）。
func parseTimeQuery(c *gin.Context, keys ...string) (*time.Time, error) {
	for _, key := range keys {
		raw := c.Query(key)
		if raw == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return nil, errcode.ErrParamFmt
		}
		return &t, nil
	}
	return nil, nil
}

// itoa int64 → 十进制字符串。
func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// formatTime 统一 UTC RFC3339 输出（零值输出空字符串）。
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05Z07:00")
}

// toSummaryDTO 摘要 → 响应（接口 ID 统一字符串）。
func toSummaryDTO(summary *domain.ReportSummary) *summaryDTO {
	if summary == nil {
		return nil
	}
	items := make([]summaryItemDTO, 0, len(summary.Items))
	for _, item := range summary.Items {
		items = append(items, summaryItemDTO{
			InterfaceID:   itoa(item.InterfaceID),
			InterfaceName: item.InterfaceName,
			Metric:        item.Metric,
			Operator:      item.Operator,
			Threshold:     item.Threshold,
			Actual:        item.Actual,
			HasActual:     item.HasActual,
			Pass:          item.Pass,
			Reason:        item.Reason,
		})
	}
	return &summaryDTO{
		State:            summary.State,
		Pass:             summary.Pass,
		TotalInterfaces:  summary.TotalInterfaces,
		PassedInterfaces: summary.PassedInterfaces,
		FailedInterfaces: summary.FailedInterfaces,
		PassRate:         summary.PassRate,
		TotalThresholds:  summary.TotalThresholds,
		PassedThresholds: summary.PassedThresholds,
		FailedThresholds: summary.FailedThresholds,
		DurationSeconds:  summary.DurationSeconds,
		Metrics:          summary.Metrics,
		Items:            items,
		MissingMetrics:   summary.MissingMetrics,
		OriginalFilename: summary.OriginalFilename,
		SHA256:           summary.SHA256,
		SizeBytes:        summary.SizeBytes,
	}
}

// toDTO 报告视图 → 响应。
func toDTO(view *rptsvc.View) reportDTO {
	artifacts := make([]artifactDTO, 0, len(view.Artifacts))
	for _, artifact := range view.Artifacts {
		artifacts = append(artifacts, artifactDTO{
			Format:      artifact.Format,
			Path:        artifact.Path,
			Size:        artifact.Size,
			Inline:      artifact.Format != domain.ReportFormatWord,
			ContentType: contentType(artifact.Format),
			CreatedAt:   formatTime(artifact.CreatedAt),
		})
	}
	taskID, productID := "", ""
	if view.TaskID > 0 {
		taskID = itoa(view.TaskID)
	}
	if view.ProductID > 0 {
		productID = itoa(view.ProductID)
	}
	return reportDTO{
		ID:        itoa(view.ID),
		TaskID:    taskID,
		ProductID: productID,
		Source:    view.Source,
		Type:      view.Type,
		Title:     view.Title,
		Status:    view.Status,
		Summary:   toSummaryDTO(view.Summary),
		Artifacts: artifacts,
		CreatedAt: formatTime(view.CreatedAt),
		UpdatedAt: formatTime(view.UpdatedAt),
	}
}

// toListDTO 批量转换报告视图。
func toListDTO(views []rptsvc.View) []reportDTO {
	out := make([]reportDTO, 0, len(views))
	for i := range views {
		out = append(out, toDTO(&views[i]))
	}
	return out
}

// contentType 格式 → Content-Type（与 product/service 保持同一口径）。
func contentType(format string) string {
	switch format {
	case domain.ReportFormatHTML:
		return "text/html; charset=utf-8"
	case domain.ReportFormatPDF:
		return "application/pdf"
	case domain.ReportFormatWord:
		return "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	default:
		return "application/octet-stream"
	}
}
