// Package web 通过 go:embed 内嵌前端构建产物（frontend/dist → web/dist）。
//
// 对齐 architecture.md §6.3：前端静态资源构建后复制到 backend/web/dist，编译期内嵌进单二进制，
// 实现离线自包含部署。
package web

import (
	"embed"
	"io/fs"
)

// FS 内嵌的前端静态资源（含 dist 下所有文件）。
//
//go:embed all:dist
var FS embed.FS

// IndexFile 前端入口文件名（SPA 回退目标）。
const IndexFile = "index.html"

// Dist 返回以 dist 为根的内嵌文件系统，供 HTTP 静态资源服务使用。
//
// 返回值可直接交给 fs.ReadFile / fs.Stat 等标准库函数；路径均相对 dist 根
// （例如 "index.html"、"assets/index-xxxx.js"），不包含 "dist/" 前缀。
func Dist() (fs.FS, error) {
	return fs.Sub(FS, "dist")
}
