// Package handler 承载 REST 接口 handler（认证、健康检查与骨架占位）。
//
// 对齐 architecture.md §3：api 层按模块分包（environment/product/task/report/log/setting），
// 业务模块 handler 随 Phase 2~7 增量实现；Phase 1 落地认证与系统设置（管理员/角色）。
package handler

import (
	"context"
	"time"

	"auto_performance/internal/config"
	authsvc "auto_performance/internal/service/auth"
)

// Pinger 数据库连通性探测接口（pgxpool.Pool 天然实现）。
type Pinger interface {
	Ping(ctx context.Context) error
}

// TokenRevoker 登录态失效能力（由 service/auth.Service 实现，登出/改密时调用）。
type TokenRevoker interface {
	// RevokeToken 将 jti 加入失效名单，ttl 为 token 剩余有效期。
	RevokeToken(jti string, ttl time.Duration)
}

// Deps handler 依赖聚合（由 router/应用装配注入）。
type Deps struct {
	// DB 数据库连通性探测器（可为 nil：无 DB 时降级运行）。
	DB Pinger
	// Config 应用配置。
	Config *config.Config
	// Tokens 登录态失效服务（可为 nil：登出不做失效登记）。
	Tokens TokenRevoker
	// Auth 认证业务服务（登录/改密/当前用户资料；可为 nil）。
	Auth *authsvc.Service
}

// Handler 聚合各模块 handler 的依赖。
type Handler struct {
	db     Pinger
	cfg    *config.Config
	tokens TokenRevoker
	auth   *authsvc.Service
}

// New 构造 Handler。
func New(deps Deps) *Handler {
	return &Handler{db: deps.DB, cfg: deps.Config, tokens: deps.Tokens, auth: deps.Auth}
}
