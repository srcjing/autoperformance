// Package report 报告管理业务服务（生成/导入/导出）。
package report

import "auto_performance/internal/errcode"

// 报告业务错误码（240xx，对齐 specs/errcode.md §4.5）。
var (
	// ErrReportFileNotFound 报告文件不存在。
	ErrReportFileNotFound = errcode.Register(&errcode.Error{Code: 24001, HTTP: 404, Message: "报告文件不存在"})
	// ErrReportTypeNotAllowed 上传文件类型不允许。
	ErrReportTypeNotAllowed = errcode.Register(&errcode.Error{Code: 24002, HTTP: 400, Message: "上传文件类型不允许"})
	// ErrReportTooLarge 上传文件过大。
	ErrReportTooLarge = errcode.Register(&errcode.Error{Code: 24003, HTTP: 400, Message: "上传文件过大"})
	// ErrReportGenerateFailed 报告生成失败。
	ErrReportGenerateFailed = errcode.Register(&errcode.Error{Code: 24004, HTTP: 500, Message: "报告生成失败"})
)
