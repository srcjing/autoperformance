// Package auth 认证与授权业务服务（登录/登出/改密/权限加载）。
package auth

import "auto_performance/internal/errcode"

// 认证业务错误码（200xx，对齐 specs/errcode.md §4.1）。
var (
	// ErrBadCredentials 用户名或密码错误（统一提示，不区分账号不存在/口令错误）。
	ErrBadCredentials = errcode.Register(&errcode.Error{Code: 20001, HTTP: 401, Message: "用户名或密码错误"})
	// ErrAccountLocked 账号被锁定。
	ErrAccountLocked = errcode.Register(&errcode.Error{Code: 20002, HTTP: 401, Message: "账号被锁定"})
	// ErrOldPasswordWrong 旧密码错误。
	ErrOldPasswordWrong = errcode.Register(&errcode.Error{Code: 20003, HTTP: 400, Message: "旧密码错误"})
	// ErrPasswordWeak 新密码不符合复杂度要求（≥8 位，含大小写/数字/特殊字符）。
	ErrPasswordWeak = errcode.Register(&errcode.Error{Code: 20004, HTTP: 400, Message: "新密码不符合复杂度要求"})
	// ErrPasswordSame 新密码与旧密码相同。
	ErrPasswordSame = errcode.Register(&errcode.Error{Code: 20005, HTTP: 400, Message: "新密码与旧密码相同"})
)
