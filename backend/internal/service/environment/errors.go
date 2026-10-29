// Package environment 环境管理业务服务（分类/标签/环境 CRUD）。
package environment

import "auto_performance/internal/errcode"

// 环境业务错误码（210xx，对齐 specs/errcode.md §4.2）。
var (
	// ErrCodeExists 环境编码已存在。
	ErrCodeExists = errcode.Register(&errcode.Error{Code: 21001, HTTP: 400, Message: "环境编码已存在"})
	// ErrCategoryRefed 分类已被环境引用，禁止删除。
	ErrCategoryRefed = errcode.Register(&errcode.Error{Code: 21002, HTTP: 409, Message: "分类已被环境引用，禁止删除"})
	// ErrTagRefed 标签已被环境引用，删除前需解除关联。
	ErrTagRefed = errcode.Register(&errcode.Error{Code: 21003, HTTP: 409, Message: "标签已被环境引用，删除前需解除关联"})
	// ErrEnvOccupied 环境被任务占用，禁止删除/停用。
	ErrEnvOccupied = errcode.Register(&errcode.Error{Code: 21004, HTTP: 409, Message: "环境被任务占用，禁止删除/停用"})
)
