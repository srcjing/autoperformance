package errcode

import (
	"fmt"
	"sync"
)

var (
	registryMu sync.RWMutex
	registry   = map[int]*Error{}
)

// Register 注册一个业务错误码（2xxxx）。
// 重复注册或越界段位将 panic，保证码值唯一；业务模块通过该方法扩展，不修改全局文件。
//
// 示例（业务模块 errors.go）：
//
//	var ErrCodeExists = errcode.Register(&errcode.Error{Code: 21001, HTTP: 400, Message: "环境编码已存在"})
func Register(e *Error) *Error {
	registryMu.Lock()
	defer registryMu.Unlock()
	if e.Code < 20000 || e.Code > 29999 {
		panic(fmt.Sprintf("errcode: 业务码越界 %d", e.Code))
	}
	if _, dup := registry[e.Code]; dup {
		panic(fmt.Sprintf("errcode: 重复注册 %d", e.Code))
	}
	registry[e.Code] = e
	return e
}

// ByCode 按码值查询（全局优先，其次业务注册表）。
func ByCode(code int) *Error {
	if e := globalByCode(code); e != nil {
		return e
	}
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registry[code]
}

// All 返回全部已注册的业务错误码（快照）。
func All() []*Error {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]*Error, 0, len(registry))
	for _, e := range registry {
		out = append(out, e)
	}
	return out
}
