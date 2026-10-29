// Package product 产品管理业务服务（产品/接口/阈值 CRUD）。
package product

import "auto_performance/internal/errcode"

// 产品业务错误码（220xx，对齐 specs/errcode.md §4.3）。
var (
	// ErrCodeExists 产品编码已存在。
	ErrCodeExists = errcode.Register(&errcode.Error{Code: 22001, HTTP: 400, Message: "产品编码已存在"})
	// ErrProductHasInterfaces 产品下存在接口，禁止删除。
	ErrProductHasInterfaces = errcode.Register(&errcode.Error{Code: 22002, HTTP: 409, Message: "产品下存在接口，禁止删除"})
	// ErrThresholdMismatch 阈值方向与指标类型不匹配。
	ErrThresholdMismatch = errcode.Register(&errcode.Error{Code: 22003, HTTP: 400, Message: "阈值方向与指标类型不匹配"})
)
