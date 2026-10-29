// Package response 定义统一响应封装 {code, message, data}。
//
// 约定（对齐 specs/api-contract.md §1.2）：
//   - code=0 表示成功；非 0 表示失败（见 errcode）；
//   - HTTP 状态码与 code 语义对齐，前端以 HTTP 状态码做全局拦截、以 code 做业务分支。
package response

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/errcode"
)

// Body 统一响应体。
type Body struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

// OK 成功响应（HTTP 200，code=0）。
func OK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Body{Code: 0, Message: "ok", Data: data})
}

// Error 失败响应（HTTP 状态码与 errcode.HTTP 对齐，data=null）。
func Error(c *gin.Context, e *errcode.Error) {
	c.JSON(e.HTTP, Body{Code: e.Code, Message: e.Message, Data: nil})
}

// ErrorFrom 将业务错误统一映射为响应：
//   - *errcode.Error（含经 %w 包装）原样输出码值与 HTTP 状态；
//   - 其余（未预期异常）兑底为 10601，避免泄露内部错误细节。
func ErrorFrom(c *gin.Context, err error) {
	var e *errcode.Error
	if errors.As(err, &e) {
		Error(c, e)
		return
	}
	Error(c, errcode.ErrInternal)
}
