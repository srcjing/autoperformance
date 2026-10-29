// Package setting 系统设置业务服务（管理员/角色/证书/SSH-Host）。
package setting

import "auto_performance/internal/errcode"

// 设置业务错误码（260xx，对齐 specs/errcode.md §4.7）。
var (
	// ErrUsernameExists 用户名已存在。
	ErrUsernameExists = errcode.Register(&errcode.Error{Code: 26001, HTTP: 400, Message: "用户名已存在"})
	// ErrBuiltinAdminProtected 内置 admin 不可删除/停用。
	ErrBuiltinAdminProtected = errcode.Register(&errcode.Error{Code: 26002, HTTP: 403, Message: "内置 admin 不可删除/停用"})
	// ErrRoleReferenced 角色被管理员引用，禁止删除。
	ErrRoleReferenced = errcode.Register(&errcode.Error{Code: 26003, HTTP: 409, Message: "角色被管理员引用，禁止删除"})
	// ErrBuiltinCertProtected 内置自签证书不可删除。
	ErrBuiltinCertProtected = errcode.Register(&errcode.Error{Code: 26004, HTTP: 403, Message: "内置自签证书不可删除"})
	// ErrSshHostReferenced SSH/Host 已被环境引用，禁止删除。
	ErrSshHostReferenced = errcode.Register(&errcode.Error{Code: 26005, HTTP: 409, Message: "SSH/Host 已被环境引用，禁止删除"})
)
