// Package templates 内嵌报告 HTML 模板。
//
// 报告必须可离线读取（PRD §3.4：支持在线与离线读取），因此模板、CSS、JS 全部内联，
// 不引用任何 CDN/外部资源（govet/测试会校验无外链）。
package templates

import "embed"

// FS 报告模板文件系统（report.html）。
//
//go:embed report.html
var FS embed.FS

// ReportHTML 报告模板文件名。
const ReportHTML = "report.html"
