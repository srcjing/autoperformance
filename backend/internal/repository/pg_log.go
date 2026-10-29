package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"auto_performance/internal/domain"
)

// auditChainLockKey user_log 追加的 DB 级互斥键（pg_advisory_xact_lock 参数）。
// 保证多连接/多实例并发追加时「读取链尾 + 插入」串行，避免审计链分叉。
const auditChainLockKey int64 = 0x6175646974 // "audit"

// LastAuditHash 返回最近一条用户日志的 hash；无记录时返回 (nil, nil)。
func (r *PGRepository) LastAuditHash(ctx context.Context) ([]byte, error) {
	const q = `SELECT hash FROM user_log ORDER BY created_at DESC, id DESC LIMIT 1`

	var hash []byte
	err := r.pool.QueryRow(ctx, q).Scan(&hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("查询审计链尾失败: %w", err)
	}
	return hash, nil
}

// AppendUserLog 追加一条用户日志（append-only，无 UPDATE/DELETE）。
// 事务内加 advisory lock，将「取链尾 → 插入」串行化（与 service/log.AppendAudit 的进程内锁互补）。
func (r *PGRepository) AppendUserLog(ctx context.Context, entry *domain.AuditLog) error {
	const q = `
		INSERT INTO user_log
			(admin_id, action, resource_type, resource_id, detail, ip, result, prev_hash, hash, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	return r.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, auditChainLockKey); err != nil {
			return fmt.Errorf("获取审计链写锁失败: %w", err)
		}
		if _, err := tx.Exec(ctx, q,
			entry.AdminID, entry.Action, nullString(entry.ResourceType), nullString(entry.ResourceID),
			nullString(entry.Detail), nullString(entry.IP), nullString(entry.Result),
			entry.PrevHash, entry.Hash, entry.CreatedAt,
		); err != nil {
			return fmt.Errorf("插入用户日志失败: %w", err)
		}
		return nil
	})
}

// nullString 空字符串写入 NULL（与表定义的可空列语义一致）。
func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ===================== 日志查询（Phase 6：用户/系统/测试日志，只读） =====================

// userLogFrom 用户日志查询来源：联表 admin 以填充操作人登录名（审计表本身只存 admin_id）。
const userLogFrom = ` FROM user_log ul LEFT JOIN admin a ON a.id = ul.admin_id`

// userLogColumns 用户日志查询列（顺序与 scanUserLog 一致）。
const userLogColumns = `ul.id, ul.admin_id, COALESCE(a.username, ''), ul.action,
	COALESCE(ul.resource_type, ''), COALESCE(ul.resource_id, ''), COALESCE(ul.detail, ''),
	COALESCE(ul.ip, ''), COALESCE(ul.result, ''), ul.created_at`

// userLogFilter 用户日志筛选条件（空值表示不限；关键字命中 detail）。
const userLogFilter = `
		WHERE ($1 = '' OR COALESCE(ul.detail, '') ILIKE $1)
		  AND ($2 = 0 OR ul.admin_id = $2)
		  AND ($3 = '' OR ul.action = $3)
		  AND ($4 = '' OR COALESCE(ul.resource_type, '') = $4)
		  AND ($5 = '' OR COALESCE(ul.resource_id, '') = $5)
		  AND ($6 = '' OR COALESCE(ul.result, '') = $6)
		  AND ($7::timestamptz IS NULL OR ul.created_at >= $7)
		  AND ($8::timestamptz IS NULL OR ul.created_at <= $8)`

// QueryUserLogs 用户日志查询（操作人/动作/关联对象/结果(级别)/关键字/时间范围 + 分页）。
// 时间倒序（最新操作在前）；筛选值均为空的条目不参与过滤。
func (r *PGRepository) QueryUserLogs(ctx context.Context, q domain.UserLogQuery) ([]domain.AuditLog, int64, error) {
	pattern := ""
	if q.Keyword != "" {
		pattern = "%" + q.Keyword + "%"
	}
	args := []any{pattern, q.AdminID, q.Action, q.ResourceType, q.ResourceID, q.Result, q.StartTime, q.EndTime}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*)`+userLogFrom+userLogFilter, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计用户日志失败: %w", err)
	}

	offset := (q.Page - 1) * q.PageSize
	if offset < 0 {
		offset = 0
	}
	query := `SELECT ` + userLogColumns + userLogFrom + userLogFilter +
		` ORDER BY ul.created_at DESC, ul.id DESC LIMIT $9 OFFSET $10`
	rows, err := r.pool.Query(ctx, query, append(args, q.PageSize, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("查询用户日志失败: %w", err)
	}
	defer rows.Close()

	out := make([]domain.AuditLog, 0, q.PageSize)
	for rows.Next() {
		var (
			entry   domain.AuditLog
			adminID *int64
		)
		if err := rows.Scan(&entry.ID, &adminID, &entry.AdminUsername, &entry.Action,
			&entry.ResourceType, &entry.ResourceID, &entry.Detail, &entry.IP,
			&entry.Result, &entry.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("扫描用户日志失败: %w", err)
		}
		entry.AdminID = adminID
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("遍历用户日志失败: %w", err)
	}
	return out, total, nil
}

// systemLogFilter 系统日志筛选条件（空值表示不限；关键字命中 message）。
const systemLogFilter = `
		WHERE ($1 = '' OR COALESCE(message, '') ILIKE $1)
		  AND ($2 = '' OR level = $2)
		  AND ($3 = '' OR COALESCE(event, '') = $3)
		  AND ($4::timestamptz IS NULL OR created_at >= $4)
		  AND ($5::timestamptz IS NULL OR created_at <= $5)`

// QuerySystemLogs 系统日志查询（事件/级别/关键字/时间范围 + 分页，时间倒序）。
func (r *PGRepository) QuerySystemLogs(ctx context.Context, q domain.SystemLogQuery) ([]domain.SystemLog, int64, error) {
	pattern := ""
	if q.Keyword != "" {
		pattern = "%" + q.Keyword + "%"
	}
	args := []any{pattern, q.Level, q.Event, q.StartTime, q.EndTime}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM system_log`+systemLogFilter, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计系统日志失败: %w", err)
	}

	offset := (q.Page - 1) * q.PageSize
	if offset < 0 {
		offset = 0
	}
	query := `SELECT id, level, COALESCE(event, ''), COALESCE(message, ''), created_at FROM system_log` +
		systemLogFilter + ` ORDER BY created_at DESC, id DESC LIMIT $6 OFFSET $7`
	rows, err := r.pool.Query(ctx, query, append(args, q.PageSize, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("查询系统日志失败: %w", err)
	}
	defer rows.Close()

	out := make([]domain.SystemLog, 0, q.PageSize)
	for rows.Next() {
		var entry domain.SystemLog
		if err := rows.Scan(&entry.ID, &entry.Level, &entry.Event, &entry.Message, &entry.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("扫描系统日志失败: %w", err)
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("遍历系统日志失败: %w", err)
	}
	return out, total, nil
}

// testLogFilter 测试日志筛选条件（空值表示不限；关键字命中 message）。
const testLogFilter = `
		WHERE ($1 = '' OR COALESCE(message, '') ILIKE $1)
		  AND ($2 = '' OR level = $2)
		  AND ($3 = 0 OR task_id = $3)
		  AND ($4::timestamptz IS NULL OR created_at >= $4)
		  AND ($5::timestamptz IS NULL OR created_at <= $5)`

// QueryTestLogs 测试日志查询（任务定位/级别/关键字/时间范围 + 分页）。
//
// 排序：带 task_id 时按 seq 升序（任务内增量回溯，seq 即执行顺序）；
// 跨任务时按时间倒序（最新执行在前）。
func (r *PGRepository) QueryTestLogs(ctx context.Context, q domain.TestLogQuery) ([]domain.TestLog, int64, error) {
	pattern := ""
	if q.Keyword != "" {
		pattern = "%" + q.Keyword + "%"
	}
	args := []any{pattern, q.Level, q.TaskID, q.StartTime, q.EndTime}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM test_log`+testLogFilter, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计测试日志失败: %w", err)
	}

	offset := (q.Page - 1) * q.PageSize
	if offset < 0 {
		offset = 0
	}
	order := ` ORDER BY created_at DESC, id DESC`
	if q.TaskID > 0 {
		order = ` ORDER BY seq ASC, id ASC`
	}
	query := `SELECT id, COALESCE(task_id, 0), COALESCE(seq, 0), level, COALESCE(message, ''), created_at` +
		` FROM test_log` + testLogFilter + order + ` LIMIT $6 OFFSET $7`
	rows, err := r.pool.Query(ctx, query, append(args, q.PageSize, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("查询测试日志失败: %w", err)
	}
	defer rows.Close()

	out := make([]domain.TestLog, 0, q.PageSize)
	for rows.Next() {
		var entry domain.TestLog
		if err := rows.Scan(&entry.ID, &entry.TaskID, &entry.Seq, &entry.Level, &entry.Message, &entry.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("扫描测试日志失败: %w", err)
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("遍历测试日志失败: %w", err)
	}
	return out, total, nil
}

// AppendSystemLog 追加一条系统日志（append-only）。
func (r *PGRepository) AppendSystemLog(ctx context.Context, entry *domain.SystemLog) error {
	const q = `INSERT INTO system_log (level, event, message, created_at) VALUES ($1, $2, $3, $4)`
	if _, err := r.pool.Exec(ctx, q, entry.Level, nullString(entry.Event), nullString(entry.Message), entry.CreatedAt); err != nil {
		return fmt.Errorf("写入系统日志失败: %w", err)
	}
	return nil
}

// LastSystemEvent 返回最近一条系统日志事件；无记录时返回 ("", nil)。
func (r *PGRepository) LastSystemEvent(ctx context.Context) (string, error) {
	const q = `SELECT COALESCE(event, '') FROM system_log ORDER BY created_at DESC, id DESC LIMIT 1`

	var event string
	err := r.pool.QueryRow(ctx, q).Scan(&event)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("查询系统日志链尾失败: %w", err)
	}
	return event, nil
}
