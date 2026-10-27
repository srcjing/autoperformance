// Package domain 领域模型（实体 + 枚举/状态机）。
//
// 本文件定义报告管理实体（Report/ReportArtifact）、报告状态机与导入导出约束，
// 对齐 PRD §3.4/§4.9/§7.3、api-contract §2.5 与安全要求：
//   - 报告状态机：generating → ready | generation_failed，generation_failed → generating（重新生成）；
//   - 报告格式白名单 html/pdf/word（决策 #4：任务完成后默认全量生成）；
//   - 导入报告允许无任务归属（source=external，标注「外部导入」）；
//   - 产物路径只作为 DB 记录保存，读取时统一做白名单目录前缀校验（DES-T-002）。
package domain

import (
	"strings"
	"time"
)

// 报告状态（PRD §7.3 报告状态机）。
const (
	// ReportStatusGenerating 生成中。
	ReportStatusGenerating = "generating"
	// ReportStatusReady 已生成。
	ReportStatusReady = "ready"
	// ReportStatusGenerationFailed 生成失败（可重新生成）。
	ReportStatusGenerationFailed = "generation_failed"
)

// 报告来源（内部生成 / 外部导入归档）。
const (
	// ReportSourceInternal 平台内任务生成的报告（含归属任务的导入报告）。
	ReportSourceInternal = "internal"
	// ReportSourceExternal 外部导入的孤立归档（无任务归属）。
	ReportSourceExternal = "external"
)

// Report 测试报告（PRD §4.9）。
//
// 一份报告记录对应一次生成/导入动作，可携带多个格式产物（report_artifact 1:N）；
// Type 为报告的主格式（内部生成取报告配置的首个格式，导入取文件格式）。
type Report struct {
	ID        int64
	TaskID    int64
	ProductID int64
	Source    string
	Type      string
	Title     string
	Summary   *ReportSummary
	Metrics   []byte
	Status    string
	Artifacts []ReportArtifact
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ReportArtifact 报告产物（format + 落盘路径 + 大小）。
//
// Path 仅在服务端内部流转：接口只返回元信息，读取时按白名单目录前缀校验（DES-T-002）。
type ReportArtifact struct {
	ID        int64
	ReportID  int64
	Format    string
	Path      string
	Size      int64
	CreatedAt time.Time
}

// ReportSummaryItem 单条阈值判定摘要（持久化在 test_report.summary）。
type ReportSummaryItem struct {
	InterfaceID   int64   `json:"interface_id"`
	InterfaceName string  `json:"interface_name,omitempty"`
	Metric        string  `json:"metric"`
	Operator      string  `json:"operator"`
	Threshold     float64 `json:"threshold"`
	Actual        float64 `json:"actual"`
	HasActual     bool    `json:"has_actual"`
	Pass          bool    `json:"pass"`
	Reason        string  `json:"reason,omitempty"`
}

// ReportSummary 报告摘要（达标统计 + 关键指标 + 逐条判定 + 导入元信息）。
type ReportSummary struct {
	State            string              `json:"state"`
	Pass             bool                `json:"pass"`
	TotalInterfaces  int                 `json:"total_interfaces"`
	PassedInterfaces int                 `json:"passed_interfaces"`
	FailedInterfaces int                 `json:"failed_interfaces"`
	PassRate         float64             `json:"pass_rate"`
	TotalThresholds  int                 `json:"total_thresholds"`
	PassedThresholds int                 `json:"passed_thresholds"`
	FailedThresholds int                 `json:"failed_thresholds"`
	DurationSeconds  float64             `json:"duration_seconds"`
	Metrics          map[string]float64  `json:"metrics,omitempty"`
	Items            []ReportSummaryItem `json:"items,omitempty"`
	MissingMetrics   []string            `json:"missing_metrics,omitempty"`
	// FailedFormats 生成失败的格式列表（部分成功时非空；不影响其他格式可用）。
	FailedFormats []string `json:"failed_formats,omitempty"`
	// OriginalFilename/SHA256/SizeBytes 仅用于导入归档（溯源与审计，DES-R-001）。
	OriginalFilename string `json:"original_filename,omitempty"`
	SHA256           string `json:"sha256,omitempty"`
	SizeBytes        int64  `json:"size_bytes,omitempty"`
}

// ReportQuery 报告列表查询条件（任务/类型/状态/来源/关键字/时间范围 + 分页）。
type ReportQuery struct {
	TaskID      int64
	ProductID   int64
	Type        string
	Status      string
	Source      string
	Keyword     string
	CreatedFrom *time.Time
	CreatedTo   *time.Time
	Page        int
	PageSize    int
}

// IsValidReportFormat 校验报告格式白名单（html/pdf/word）。
func IsValidReportFormat(format string) bool {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case ReportFormatHTML, ReportFormatPDF, ReportFormatWord:
		return true
	default:
		return false
	}
}

// IsValidReportStatus 校验报告状态枚举。
func IsValidReportStatus(status string) bool {
	switch status {
	case ReportStatusGenerating, ReportStatusReady, ReportStatusGenerationFailed:
		return true
	default:
		return false
	}
}

// IsValidReportSource 校验报告来源枚举（internal/external）。
func IsValidReportSource(source string) bool {
	switch source {
	case ReportSourceInternal, ReportSourceExternal:
		return true
	default:
		return false
	}
}

// CanRegenerateReport 报告是否允许（重新）生成：生成中禁止并发重复生成。
func CanRegenerateReport(status string) bool {
	switch status {
	case ReportStatusReady, ReportStatusGenerationFailed:
		return true
	default:
		return false
	}
}

// ReportTypeByExtension 按文件扩展名推断报告格式（导入校验失败返回 false）。
func ReportTypeByExtension(ext string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(ext)) {
	case ".html", ".htm":
		return ReportFormatHTML, true
	case ".pdf":
		return ReportFormatPDF, true
	case ".docx":
		return ReportFormatWord, true
	default:
		return "", false
	}
}

// ReportExtension 报告格式 → 落盘扩展名。
func ReportExtension(format string) string {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case ReportFormatHTML:
		return ".html"
	case ReportFormatPDF:
		return ".pdf"
	case ReportFormatWord:
		return ".docx"
	default:
		return ""
	}
}
