// Package web 通过 go:embed 内嵌前端构建产物（frontend/dist → web/dist）。
//
// 对齐 architecture.md §6.3：前端静态资源构建后复制到 backend/web/dist，编译期内嵌进单二进制，
// 实现离线自包含部署。
package web

import "embed"

// FS 内嵌的前端静态资源（含 dist 下所有文件）。
//
//go:embed all:dist
var FS embed.FS
