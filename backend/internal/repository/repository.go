// Package repository 数据访问层（隔离业务与存储）。
//
// 采用 pgx/v5 原生访问（非 gorm，对齐 ADR-0004）。本文件定义仓储接口与事务抽象。
// 各业务模块仓储接口（Environment/Product/Testing/Report/Log/Setting）随对应
// Phase（2~7）在各自的 pg_*.go 中定义并实现；Phase 0 先落地认证仓储 AuthRepo 与
// 事务抽象 WithTx、乐观锁/软删除通用 helper。
package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"auto_performance/internal/domain"
)

// Tx 事务抽象：直接复用 pgx.Tx（原生接口，Begin 返回）。供环境互斥/快照等事务使用。
type Tx = pgx.Tx

// TxFunc 事务内执行函数；返回 error 则回滚，否则提交。
type TxFunc func(ctx context.Context, tx Tx) error

// AuthRepo 认证/授权仓储接口（Phase 1 登录与鉴权依赖）。
type AuthRepo interface {
	// GetAdminByUsername 按登录名查询管理员（含停用/软删除过滤之外的字段，由调用方判定）。
	GetAdminByUsername(ctx context.Context, username string) (*domain.Admin, error)
	// ListPermissionsByAdmin 计算管理员最终权限并集（admin → admin_role → role → role_permission）。
	ListPermissionsByAdmin(ctx context.Context, adminID int64) (map[string]bool, error)
	// LoadRolesByAdmin 查询管理员绑定的角色列表。
	LoadRolesByAdmin(ctx context.Context, adminID int64) ([]domain.Role, error)
}

// LogRepo 日志仓储接口（审计链追加 + 四类日志查询 + 日志设置/清理）。
//
// 查询方法对应用户/系统/测试/请求详情日志检索与系统日志写入（启动/重启/停止），
// 审计表（user_log）不提供任何 UPDATE/DELETE 能力（DES-T-006）；
// 仅日志设置触发的过期/超量清理（PurgeLogs）作用于 request_log/test_log/system_log。
type LogRepo interface {
	// LastAuditHash 返回最近一条用户日志的 hash（无记录返回 nil, nil）。
	LastAuditHash(ctx context.Context) ([]byte, error)
	// AppendUserLog 追加一条用户日志（user_log）。
	AppendUserLog(ctx context.Context, entry *domain.AuditLog) error
	// QueryUserLogs 查询用户日志（联表填充操作人登录名）。
	QueryUserLogs(ctx context.Context, q domain.UserLogQuery) ([]domain.AuditLog, int64, error)
	// QuerySystemLogs 查询系统日志。
	QuerySystemLogs(ctx context.Context, q domain.SystemLogQuery) ([]domain.SystemLog, int64, error)
	// QueryTestLogs 查询测试日志（task_id 命中时按 seq 升序）。
	QueryTestLogs(ctx context.Context, q domain.TestLogQuery) ([]domain.TestLog, int64, error)
	// AppendSystemLog 追加一条系统日志（system_log）。
	AppendSystemLog(ctx context.Context, entry *domain.SystemLog) error
	// LastSystemEvent 返回最近一条系统日志事件（无记录返回 ""）。
	LastSystemEvent(ctx context.Context) (string, error)
	// AppendRequestLogs 批量写入压测请求详情（request_log）。
	AppendRequestLogs(ctx context.Context, entries []domain.RequestLog) error
	// QueryRequestLogs 查询请求详情（任务/接口/时间范围 + 分页）。
	QueryRequestLogs(ctx context.Context, q domain.RequestLogQuery) ([]domain.RequestLog, int64, error)
	// GetLogSetting 读取日志设置（单行；无记录返回 nil, nil）。
	GetLogSetting(ctx context.Context) (*domain.LogSetting, error)
	// SaveLogSetting 保存日志设置（单行 upsert）。
	SaveLogSetting(ctx context.Context, setting *domain.LogSetting) error
	// PurgeLogs 按保留策略（截止时间 + 最大条数）清理日志，返回各表删除行数。
	PurgeLogs(ctx context.Context, cutoff time.Time, maxCount int64) (domain.PurgeResult, error)
}

// Repository 仓储聚合接口：认证/日志仓储 + 事务 + 连接生命周期。
type Repository interface {
	AuthRepo
	LogRepo
	WithTx(ctx context.Context, fn TxFunc) error
	Ping(ctx context.Context) error
	Close()
}

// PGRepository pgx 仓储实现。
type PGRepository struct {
	pool *pgxpool.Pool
}

// NewRepository 构造 pgx 仓储。
func NewRepository(pool *pgxpool.Pool) *PGRepository {
	return &PGRepository{pool: pool}
}

// Ping 连通性探测。
func (r *PGRepository) Ping(ctx context.Context) error {
	return r.pool.Ping(ctx)
}

// Close 关闭连接池。
func (r *PGRepository) Close() {
	r.pool.Close()
}

// WithTx 在单个事务内执行 fn；fn 返回 nil 提交，否则回滚。
func (r *PGRepository) WithTx(ctx context.Context, fn TxFunc) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // commit 后 rollback 为 no-op

	if err := fn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
