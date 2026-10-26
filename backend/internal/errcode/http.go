package errcode

// globalMap 全局通用错误码（0 + 1xxxx）查询表。
// 业务错误码（2xxxx）不在此表，通过 registry.Register 注册到 registry.go。
var globalMap = map[int]*Error{
	OK.Code:            OK,
	ErrParam.Code:      ErrParam,
	ErrParamMiss.Code:  ErrParamMiss,
	ErrParamFmt.Code:   ErrParamFmt,
	ErrUnauth.Code:     ErrUnauth,
	ErrSession.Code:    ErrSession,
	ErrMustPwd.Code:    ErrMustPwd,
	ErrForbidden.Code:  ErrForbidden,
	ErrDisabled.Code:   ErrDisabled,
	ErrNotFound.Code:   ErrNotFound,
	ErrConflict.Code:   ErrConflict,
	ErrState.Code:      ErrState,
	ErrConcurrent.Code: ErrConcurrent,
	ErrReferenced.Code: ErrReferenced,
	ErrEnvBusy.Code:    ErrEnvBusy,
	ErrRateLimit.Code:  ErrRateLimit,
	ErrLoginLock.Code:  ErrLoginLock,
	ErrInternal.Code:   ErrInternal,
	ErrDependency.Code: ErrDependency,
}

// globalByCode 按码值查询全局错误码。
func globalByCode(code int) *Error {
	return globalMap[code]
}

// HTTPStatus 返回错误码对应的 HTTP 状态码；未知码兜底 500。
func HTTPStatus(code int) int {
	if e := ByCode(code); e != nil {
		return e.HTTP
	}
	return 500
}
