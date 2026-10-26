// Package errcode 定义统一错误码模型与分层机制。
//
// 分层边界（对齐 specs/errcode.md）：
//   - 全局通用错误码（0 + 1xxxx）：框架级，业务禁止新增；
//   - 业务错误码（2xxxx）：各业务模块自维护，通过 Register 注册，不修改全局文件。
package errcode

// Error 统一错误模型：code + HTTP 状态码 + 含义。
type Error struct {
	Code    int    `json:"code"`
	HTTP    int    `json:"-"`
	Message string `json:"message"`
}

// Error 实现 error 接口。
func (e *Error) Error() string { return e.Message }

// 全局通用错误码（1xxxx，框架级，业务禁止新增）。
var (
	OK            = &Error{Code: 0, HTTP: 200, Message: "ok"}
	ErrParam      = &Error{Code: 10001, HTTP: 400, Message: "参数错误"}
	ErrParamMiss  = &Error{Code: 10002, HTTP: 400, Message: "参数缺失"}
	ErrParamFmt   = &Error{Code: 10003, HTTP: 400, Message: "参数格式非法"}
	ErrUnauth     = &Error{Code: 10101, HTTP: 401, Message: "未认证"}
	ErrSession    = &Error{Code: 10102, HTTP: 401, Message: "会话失效"}
	ErrMustPwd    = &Error{Code: 10103, HTTP: 403, Message: "需修改密码"}
	ErrForbidden  = &Error{Code: 10201, HTTP: 403, Message: "权限不足"}
	ErrDisabled   = &Error{Code: 10202, HTTP: 403, Message: "账号已停用"}
	ErrNotFound   = &Error{Code: 10301, HTTP: 404, Message: "资源不存在"}
	ErrConflict   = &Error{Code: 10302, HTTP: 400, Message: "资源已存在"}
	ErrState      = &Error{Code: 10401, HTTP: 409, Message: "状态冲突"}
	ErrConcurrent = &Error{Code: 10402, HTTP: 409, Message: "并发冲突"}
	ErrReferenced = &Error{Code: 10403, HTTP: 409, Message: "资源被引用"}
	ErrEnvBusy    = &Error{Code: 10404, HTTP: 409, Message: "环境被占用"}
	ErrRateLimit  = &Error{Code: 10501, HTTP: 429, Message: "请求过于频繁"}
	ErrLoginLock  = &Error{Code: 10502, HTTP: 429, Message: "登录锁定"}
	ErrInternal   = &Error{Code: 10601, HTTP: 500, Message: "服务器内部错误"}
	ErrDependency = &Error{Code: 10602, HTTP: 500, Message: "依赖服务异常"}
)
