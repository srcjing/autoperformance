package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"auto_performance/internal/domain"
)

// ===================== 请求详情（request_log） =====================

// AppendRequestLogs 批量写入请求详情（单条 INSERT 多值，降低往返开销）。
// 脱敏与截断由服务层完成（DES-I-001 / domain.MaxLogBodyBytes）。
func (r *PGRepository) AppendRequestLogs(ctx context.Context, entries []domain.RequestLog) error {
	if len(entries) == 0 {
		return nil
	}
	// 列顺序必须与下方 args 追加顺序严格一致。
	const columns = 14
	var b strings.Builder
	b.WriteString(`INSERT INTO request_log
		(task_id, interface_id, seq, occurred_at, method, url, request_body, request_headers,
		 response_status, response_body, response_headers, error_code, error_message, duration_ms) VALUES `)
	args := make([]any, 0, len(entries)*columns)
	for i := range entries {
		e := &entries[i]
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('(')
		for c := 1; c <= columns; c++ {
			if c > 1 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, "$%d", i*columns+c)
		}
		b.WriteByte(')')
		args = append(args, e.TaskID, e.InterfaceID, e.Seq, e.OccurredAt,
			e.Method, e.URL, e.RequestBody, e.RequestHeaders, e.ResponseStatus, e.ResponseBody,
			e.ResponseHeaders, e.ErrorCode, e.ErrorMessage, e.DurationMs)
	}

	if _, err := r.pool.Exec(ctx, b.String(), args...); err != nil {
		return fmt.Errorf("批量写入请求详情失败: %w", err)
	}
	return nil
}

// requestLogFilter 请求详情筛选条件（空值表示不限；关键字命中 url 或 error_message）。
const requestLogFilter = `
		WHERE ($1 = 0 OR task_id = $1)
		  AND ($2 = 0 OR interface_id = $2)
		  AND ($3 = '' OR url ILIKE $3 OR error_message ILIKE $3)
		  AND ($4::timestamptz IS NULL OR occurred_at >= $4)
		  AND ($5::timestamptz IS NULL OR occurred_at <= $5)`

// QueryRequestLogs 请求详情查询（任务/接口/关键字/时间范围 + 分页）。
//
// 排序：带 task_id 时按 seq 升序（任务内请求顺序）；跨任务按发生时间倒序。
func (r *PGRepository) QueryRequestLogs(ctx context.Context, q domain.RequestLogQuery) ([]domain.RequestLog, int64, error) {
	pattern := ""
	if q.Keyword != "" {
		pattern = "%" + q.Keyword + "%"
	}
	args := []any{q.TaskID, q.InterfaceID, pattern, q.StartTime, q.EndTime}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM request_log`+requestLogFilter, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计请求详情失败: %w", err)
	}

	offset := (q.Page - 1) * q.PageSize
	if offset < 0 {
		offset = 0
	}
	order := ` ORDER BY occurred_at DESC, id DESC`
	if q.TaskID > 0 {
		order = ` ORDER BY seq ASC, id ASC`
	}
	const columns = `id, task_id, interface_id, seq, occurred_at, method, url, request_body, request_headers,
		response_status, response_body, response_headers, error_code, error_message, duration_ms`
	query := `SELECT ` + columns + ` FROM request_log` + requestLogFilter + order +
		` LIMIT $6 OFFSET $7`
	rows, err := r.pool.Query(ctx, query, append(args, q.PageSize, offset)...)
	if err != nil {
		return nil, 0, fmt.Errorf("查询请求详情失败: %w", err)
	}
	defer rows.Close()

	out := make([]domain.RequestLog, 0, q.PageSize)
	for rows.Next() {
		var entry domain.RequestLog
		if err := rows.Scan(&entry.ID, &entry.TaskID, &entry.InterfaceID, &entry.Seq, &entry.OccurredAt,
			&entry.Method, &entry.URL, &entry.RequestBody, &entry.RequestHeaders, &entry.ResponseStatus,
			&entry.ResponseBody, &entry.ResponseHeaders, &entry.ErrorCode, &entry.ErrorMessage,
			&entry.DurationMs); err != nil {
			return nil, 0, fmt.Errorf("扫描请求详情失败: %w", err)
		}
		out = append(out, entry)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("遍历请求详情失败: %w", err)
	}
	return out, total, nil
}

// ===================== 日志设置（log_setting，单行） =====================

// GetLogSetting 读取日志设置；未初始化（无记录）时返回 (nil, nil)，由服务层套用默认值。
func (r *PGRepository) GetLogSetting(ctx context.Context) (*domain.LogSetting, error) {
	const q = `SELECT enabled, max_count, retention_days, log_types, updated_at
		FROM log_setting WHERE id = 1`

	var (
		setting domain.LogSetting
		raw     []byte
	)
	err := r.pool.QueryRow(ctx, q).Scan(&setting.Enabled, &setting.MaxCount,
		&setting.RetentionDays, &raw, &setting.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("读取日志设置失败: %w", err)
	}
	if len(raw) > 0 {
		var types []string
		if err := json.Unmarshal(raw, &types); err != nil {
			return nil, fmt.Errorf("解析日志类型失败: %w", err)
		}
		setting.LogTypes = types
	}
	return &setting, nil
}

// SaveLogSetting 保存日志设置（单行 upsert，id 恒为 1）。
func (r *PGRepository) SaveLogSetting(ctx context.Context, setting *domain.LogSetting) error {
	if setting == nil {
		return fmt.Errorf("日志设置为空")
	}
	types := setting.LogTypes
	if types == nil {
		types = []string{}
	}
	raw, err := json.Marshal(types)
	if err != nil {
		return fmt.Errorf("序列化日志类型失败: %w", err)
	}

	const q = `
		INSERT INTO log_setting (id, enabled, max_count, retention_days, log_types, updated_at)
		VALUES (1, $1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET
			enabled = EXCLUDED.enabled,
			max_count = EXCLUDED.max_count,
			retention_days = EXCLUDED.retention_days,
			log_types = EXCLUDED.log_types,
			updated_at = EXCLUDED.updated_at`
	if _, err := r.pool.Exec(ctx, q, setting.Enabled, setting.MaxCount,
		setting.RetentionDays, string(raw), setting.UpdatedAt); err != nil {
		return fmt.Errorf("保存日志设置失败: %w", err)
	}
	return nil
}

// purgeTables 参与过期/超量清理的日志表及其时间列。
//
// user_log 为 append-only 审计表（000002_audit_trigger 禁止 UPDATE/DELETE），不参与清理。
var purgeTables = []struct{ table, timeColumn string }{
	{"request_log", "occurred_at"},
	{"test_log", "created_at"},
	{"system_log", "created_at"},
}

// PurgeLogs 按保留策略清理日志：删除早于 cutoff 的记录，并仅保留最新 maxCount 条。
// 表名/列名来自包内静态清单（非用户输入），不存在注入风险。
func (r *PGRepository) PurgeLogs(ctx context.Context, cutoff time.Time, maxCount int64) (domain.PurgeResult, error) {
	if maxCount <= 0 {
		maxCount = domain.DefaultLogMaxCount
	}
	var out domain.PurgeResult
	err := r.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		for _, t := range purgeTables {
			deleted, err := purgeOne(ctx, tx, t.table, t.timeColumn, cutoff, maxCount)
			if err != nil {
				return err
			}
			switch t.table {
			case "request_log":
				out.RequestLogs = deleted
			case "test_log":
				out.TestLogs = deleted
			case "system_log":
				out.SystemLogs = deleted
			}
		}
		return nil
	})
	if err != nil {
		return domain.PurgeResult{}, err
	}
	return out, nil
}

// purgeOne 清理单表：过期记录 + 超出 maxCount 的最旧记录，返回删除行数。
func purgeOne(ctx context.Context, tx Tx, table, timeColumn string, cutoff time.Time, maxCount int64) (int64, error) {
	var deleted int64

	expiredSQL := fmt.Sprintf(`DELETE FROM %s WHERE %s < $1`, table, timeColumn)
	if tag, err := tx.Exec(ctx, expiredSQL, cutoff); err != nil {
		return 0, fmt.Errorf("清理 %s 过期记录失败: %w", table, err)
	} else {
		deleted += tag.RowsAffected()
	}

	overflowSQL := fmt.Sprintf(
		`DELETE FROM %s WHERE id NOT IN (SELECT id FROM %s ORDER BY id DESC LIMIT $1)`,
		table, table)
	if tag, err := tx.Exec(ctx, overflowSQL, maxCount); err != nil {
		return 0, fmt.Errorf("清理 %s 超量记录失败: %w", table, err)
	} else {
		deleted += tag.RowsAffected()
	}
	return deleted, nil
}
