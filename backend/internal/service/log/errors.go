// Package log 日志业务服务（用户/系统/测试日志 + 审计 HMAC 链）。
package log

import "auto_performance/internal/errcode"

// 日志业务错误码（250xx，对齐 specs/errcode.md §4.6）。
var (
	// ErrInvalidTimeRange 查询时间范围非法。
	ErrInvalidTimeRange = errcode.Register(&errcode.Error{Code: 25001, HTTP: 400, Message: "查询时间范围非法"})
)
