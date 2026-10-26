// Package handler 承载 REST 接口 handler（按模块分包，骨架仅含认证与健康检查）。
//
// 对齐 architecture.md §3：api 层按模块分包（environment/product/task/report/log/setting），
// 骨架阶段先落地认证与健康检查，其余模块目录以 doc.go 占位。
package handler

import (
	"context"

	"auto_performance/internal/config"
)

// Pinger 数据库连通性探测接口（pgxpool.Pool 天然实现）。
type Pinger interface {
	Ping(ctx context.Context) error
}

// Handler 聚合各模块 handler 的依赖。
type Handler struct {
	db  Pinger
	cfg *config.Config
}

// New 构造 Handler。
func New(db Pinger, cfg *config.Config) *Handler {
	return &Handler{db: db, cfg: cfg}
}
