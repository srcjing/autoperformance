// Package reportgen 报告生成器（HTML/PDF/Word）。
//
// 对齐 architecture.md §1.4 与 PRD §3.4：
//   - HTML：html/template + 内嵌模板（backend/templates/report.html），CSS/JS 全内联，可离线读取；
//   - PDF：headless Chromium 打印 HTML（见 internal/reportgen/pdf），超时/不可用映射 24004；
//   - Word：gooxml 生成 docx（见 internal/reportgen/word），与 HTML 共用渲染模型。
//
// 渲染模型（ReportData）在本包 report.go 中统一构建：达标统计口径、接口级结论、指标顺序
// 与脱敏规则只实现一次，保证三种格式一致（INV-2 / DES-I-001）。
package reportgen
