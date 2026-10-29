// Package testing 测试管理业务服务（模板/任务编排）。
package testing

import "auto_performance/internal/errcode"

// 测试业务错误码（230xx，对齐 specs/errcode.md §4.4）。
var (
	// ErrInvalidTaskState 任务状态不允许该操作。
	ErrInvalidTaskState = errcode.Register(&errcode.Error{Code: 23001, HTTP: 409, Message: "任务状态不允许该操作"})
	// ErrEnvBusy 环境被占用/正在测试（环境互斥）。
	ErrEnvBusy = errcode.Register(&errcode.Error{Code: 23002, HTTP: 409, Message: "环境被占用/正在测试"})
	// ErrInterfaceRangeEmpty 接口范围为空。
	ErrInterfaceRangeEmpty = errcode.Register(&errcode.Error{Code: 23003, HTTP: 400, Message: "接口范围为空"})
	// ErrConcurrencyInvalid 并发数非法或超出上限（max_vus）。
	ErrConcurrencyInvalid = errcode.Register(&errcode.Error{Code: 23004, HTTP: 400, Message: "并发数非法或超出上限"})
	// ErrTemplateNotFound 模板不存在。
	ErrTemplateNotFound = errcode.Register(&errcode.Error{Code: 23005, HTTP: 404, Message: "模板不存在"})
)
