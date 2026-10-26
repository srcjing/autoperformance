// Package repository 负责数据访问层（隔离业务与存储）。
//
// 骨架阶段：仅提供 PostgreSQL 连接池初始化（pgx），业务 CRUD 后续按实现计划填充。
// 正式实现将采用 gorm 或 pgx，并在此层实现 Environment/Product/Task/Report 等仓储接口。
package repository

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"auto_performance/internal/config"
)

// Open 初始化 PostgreSQL 连接池（占位实现）。
//
// 使用 pgxpool 且 MinConns=0（lazy 建连）：即使数据库暂不可达也不会阻塞进程启动，
// 便于骨架阶段无数据库也能运行健康检查。
func Open(cfg config.DatabaseConfig) (*pgxpool.Pool, error) {
	dsn := buildDSN(cfg)

	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("解析数据库 DSN 失败: %w", err)
	}
	poolCfg.MaxConns = 10
	poolCfg.MinConns = 0
	poolCfg.MaxConnLifetime = time.Hour

	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		return nil, fmt.Errorf("创建连接池失败: %w", err)
	}
	return pool, nil
}

// buildDSN 构造 PostgreSQL 连接串（对密码做 URL 转义）。
func buildDSN(cfg config.DatabaseConfig) string {
	u := url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Path:   cfg.Name,
	}
	q := u.Query()
	q.Set("sslmode", cfg.SSLMode)
	u.RawQuery = q.Encode()
	return u.String()
}
