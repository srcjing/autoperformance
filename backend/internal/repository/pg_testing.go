package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// 测试管理相关查询列（顺序与 scan* 一致）。
const (
	templateColumns = `id, name, COALESCE(env_ids, '[]'::jsonb), COALESCE(interface_ids, '[]'::jsonb),
		COALESCE(flow, '{}'::jsonb), COALESCE(report_config, '{}'::jsonb),
		COALESCE(clean_config, '{}'::jsonb), COALESCE(description, ''),
		version, created_at, updated_at`

	taskColumns = `id, COALESCE(template_id, 0), name, env_id, COALESCE(target_host, ''),
		COALESCE(created_by, 0),
		COALESCE(product_ids, '[]'::jsonb), COALESCE(interface_ids, '[]'::jsonb),
		COALESCE(interface_range_snapshot, '[]'::jsonb), COALESCE(threshold_snapshot, '[]'::jsonb),
		COALESCE(flow, '{}'::jsonb), COALESCE(report_config, '{}'::jsonb),
		COALESCE(clean_config, '{}'::jsonb), status, started_at, finished_at,
		COALESCE(result, '{}'::jsonb), version, created_at, updated_at`
)

// execQuerier 事务/连接池统一访问接口（tx 为 nil 时回落到连接池）。
type execQuerier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// conn 返回本次操作使用的执行器（nil tx → 连接池）。
func (r *PGRepository) conn(tx Tx) execQuerier {
	if tx != nil {
		return tx
	}
	return r.pool
}

// scanTemplate 扫描一行模板（JSONB 数组/对象解码）。
func scanTemplate(row pgx.Row) (*domain.TestTemplate, error) {
	var (
		t                            domain.TestTemplate
		envIDs, interfaceIDs         []byte
		flow, reportConfig, cleanCfg []byte
	)
	if err := row.Scan(&t.ID, &t.Name, &envIDs, &interfaceIDs, &flow, &reportConfig, &cleanCfg,
		&t.Description, &t.Version, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	if err := decodeJSONArray(envIDs, &t.EnvIDs); err != nil {
		return nil, fmt.Errorf("解析模板环境列表失败: %w", err)
	}
	if err := decodeJSONArray(interfaceIDs, &t.InterfaceIDs); err != nil {
		return nil, fmt.Errorf("解析模板接口列表失败: %w", err)
	}
	t.Flow = normalizeRawJSON(flow)
	t.ReportConfig = normalizeRawJSON(reportConfig)
	t.CleanConfig = normalizeRawJSON(cleanCfg)
	return &t, nil
}

// scanTask 扫描一行任务（快照与配置 JSONB 解码）。
func scanTask(row pgx.Row) (*domain.TestTask, error) {
	var (
		t                                      domain.TestTask
		status                                 string
		productIDs, interfaceIDs               []byte
		interfaceSnapshots, thresholdSnapshots []byte
		flow, reportConfig, cleanCfg, result   []byte
	)
	if err := row.Scan(&t.ID, &t.TemplateID, &t.Name, &t.EnvID, &t.TargetHost, &t.CreatedBy,
		&productIDs, &interfaceIDs, &interfaceSnapshots, &thresholdSnapshots,
		&flow, &reportConfig, &cleanCfg, &status, &t.StartedAt, &t.FinishedAt,
		&result, &t.Version, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	t.Status = domain.TaskStatus(status)
	if err := decodeJSONArray(productIDs, &t.ProductIDs); err != nil {
		return nil, fmt.Errorf("解析任务产品列表失败: %w", err)
	}
	if err := decodeJSONArray(interfaceIDs, &t.InterfaceIDs); err != nil {
		return nil, fmt.Errorf("解析任务接口列表失败: %w", err)
	}
	if err := decodeJSONArray(interfaceSnapshots, &t.InterfaceSnapshots); err != nil {
		return nil, fmt.Errorf("解析接口范围快照失败: %w", err)
	}
	if err := decodeJSONArray(thresholdSnapshots, &t.ThresholdSnapshots); err != nil {
		return nil, fmt.Errorf("解析阈值快照失败: %w", err)
	}
	t.Flow = normalizeRawJSON(flow)
	t.ReportConfig = normalizeRawJSON(reportConfig)
	t.CleanConfig = normalizeRawJSON(cleanCfg)
	if len(result) > 0 && string(result) != "{}" {
		t.Result = normalizeRawJSON(result)
	}
	return &t, nil
}

// nowUTC 统一 UTC 时间（快照捕获时间）。
func nowUTC() time.Time { return time.Now().UTC() }

// dedupeInt64 去重并过滤非正 ID。
func dedupeInt64(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// decodeJSONArray 解码 JSONB 数组（NULL/空 → 空切片）。
func decodeJSONArray(raw []byte, dest any) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return err
	}
	return nil
}

// normalizeRawJSON 返回可安全序列化的 JSON 原文（空/NULL → nil）。
func normalizeRawJSON(raw []byte) json.RawMessage {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "{}" {
		return nil
	}
	out := make(json.RawMessage, len(raw))
	copy(out, raw)
	return out
}

// marshalIDs 序列化 ID 列表为 JSONB 文本（nil → []）。
func marshalIDs(ids []int64) (string, error) {
	if ids == nil {
		ids = []int64{}
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return "", fmt.Errorf("序列化 ID 列表失败: %w", err)
	}
	return string(raw), nil
}

// marshalSnapshot 序列化快照集合（nil → []）。
func marshalSnapshot(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("序列化快照失败: %w", err)
	}
	if string(raw) == "null" {
		return "[]", nil
	}
	return string(raw), nil
}

// marshalConfig 序列化配置对象（nil → {}）。
func marshalConfig(v json.RawMessage) (string, error) {
	if len(v) == 0 {
		return "{}", nil
	}
	var probe map[string]any
	if err := json.Unmarshal(v, &probe); err != nil {
		return "", fmt.Errorf("配置不是合法 JSON 对象: %w", err)
	}
	return string(v), nil
}

// ===================== 模板 =====================

// ListTemplates 模板列表（关键字 + 分页）。
func (r *PGRepository) ListTemplates(ctx context.Context, q domain.TemplateQuery) ([]domain.TestTemplate, int64, error) {
	pattern := ""
	if q.Keyword != "" {
		pattern = "%" + q.Keyword + "%"
	}
	const countQ = `SELECT count(*) FROM test_template WHERE deleted_at IS NULL AND ($1 = '' OR name ILIKE $1)`
	var total int64
	if err := r.pool.QueryRow(ctx, countQ, pattern).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计模板失败: %w", err)
	}

	offset := (q.Page - 1) * q.PageSize
	if offset < 0 {
		offset = 0
	}
	query := `SELECT ` + templateColumns + ` FROM test_template
	          WHERE deleted_at IS NULL AND ($1 = '' OR name ILIKE $1)
	          ORDER BY id LIMIT $2 OFFSET $3`
	rows, err := r.pool.Query(ctx, query, pattern, q.PageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("查询模板列表失败: %w", err)
	}
	defer rows.Close()

	out := make([]domain.TestTemplate, 0, q.PageSize)
	for rows.Next() {
		t, err := scanTemplate(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("扫描模板失败: %w", err)
		}
		out = append(out, *t)
	}
	return out, total, rows.Err()
}

// GetTemplate 模板详情（不存在 → 10301）。
func (r *PGRepository) GetTemplate(ctx context.Context, id int64) (*domain.TestTemplate, error) {
	const q = `SELECT ` + templateColumns + ` FROM test_template WHERE id = $1 AND deleted_at IS NULL`
	t, err := scanTemplate(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errcode.ErrNotFound
		}
		return nil, fmt.Errorf("查询模板失败: %w", err)
	}
	return t, nil
}

// CreateTemplate 新建模板。
func (r *PGRepository) CreateTemplate(ctx context.Context, t *domain.TestTemplate) error {
	envIDs, err := marshalIDs(t.EnvIDs)
	if err != nil {
		return err
	}
	interfaceIDs, err := marshalIDs(t.InterfaceIDs)
	if err != nil {
		return err
	}
	flow, err := marshalConfig(t.Flow)
	if err != nil {
		return err
	}
	reportConfig, err := marshalConfig(t.ReportConfig)
	if err != nil {
		return err
	}
	cleanConfig, err := marshalConfig(t.CleanConfig)
	if err != nil {
		return err
	}

	const q = `
		INSERT INTO test_template (name, env_ids, interface_ids, flow, report_config, clean_config, description)
		VALUES ($1, $2::jsonb, $3::jsonb, $4::jsonb, $5::jsonb, $6::jsonb, $7)
		RETURNING id, version, created_at, updated_at`
	err = r.pool.QueryRow(ctx, q, t.Name, envIDs, interfaceIDs, flow, reportConfig, cleanConfig, t.Description).
		Scan(&t.ID, &t.Version, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return fmt.Errorf("创建模板失败: %w", err)
	}
	return nil
}

// UpdateTemplate 全量修改模板（version 乐观锁：冲突 → 10402）。
func (r *PGRepository) UpdateTemplate(ctx context.Context, t *domain.TestTemplate, version int64) error {
	envIDs, err := marshalIDs(t.EnvIDs)
	if err != nil {
		return err
	}
	interfaceIDs, err := marshalIDs(t.InterfaceIDs)
	if err != nil {
		return err
	}
	flow, err := marshalConfig(t.Flow)
	if err != nil {
		return err
	}
	reportConfig, err := marshalConfig(t.ReportConfig)
	if err != nil {
		return err
	}
	cleanConfig, err := marshalConfig(t.CleanConfig)
	if err != nil {
		return err
	}

	const q = `
		UPDATE test_template
		SET name = $3, env_ids = $4::jsonb, interface_ids = $5::jsonb, flow = $6::jsonb,
		    report_config = $7::jsonb, clean_config = $8::jsonb, description = $9,
		    version = version + 1, updated_at = now()
		WHERE id = $1 AND version = $2 AND deleted_at IS NULL`
	tag, err := r.pool.Exec(ctx, q, t.ID, version, t.Name, envIDs, interfaceIDs, flow, reportConfig, cleanConfig, t.Description)
	if err != nil {
		return fmt.Errorf("修改模板失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errcode.ErrConcurrent
	}
	return nil
}

// SoftDeleteTemplate 逻辑删除模板（不存在 → 10301，由服务层映射 23005）。
func (r *PGRepository) SoftDeleteTemplate(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE test_template SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL`, id)
	if err != nil {
		return fmt.Errorf("删除模板失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errcode.ErrNotFound
	}
	return nil
}

// ===================== 环境互斥 =====================

// LockEnvironment 事务内锁定环境行（SELECT ... FOR UPDATE，INV-4 / DES-D-001）。
func (r *PGRepository) LockEnvironment(ctx context.Context, tx Tx, envID int64) (*domain.Environment, error) {
	const q = `SELECT id, status, COALESCE(ssh_host_id, 0), COALESCE(hosts, '[]'::jsonb)
	           FROM environment WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`
	env := &domain.Environment{}
	var hosts []byte
	err := r.conn(tx).QueryRow(ctx, q, envID).Scan(&env.ID, &env.Status, &env.SshHostID, &hosts)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errcode.ErrNotFound
		}
		return nil, fmt.Errorf("锁定环境失败: %w", err)
	}
	if err := decodeJSONArray(hosts, &env.Hosts); err != nil {
		return nil, fmt.Errorf("解析环境主机列表失败: %w", err)
	}
	if env.Hosts == nil {
		env.Hosts = []string{}
	}
	return env, nil
}

// MarkEnvironmentTesting 占用环境（pending/disabled → testing）。
func (r *PGRepository) MarkEnvironmentTesting(ctx context.Context, tx Tx, envID int64) error {
	const q = `UPDATE environment SET status = 'testing', version = version + 1, updated_at = now()
	           WHERE id = $1 AND deleted_at IS NULL`
	tag, err := r.conn(tx).Exec(ctx, q, envID)
	if err != nil {
		return fmt.Errorf("占用环境失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errcode.ErrNotFound
	}
	return nil
}

// ReleaseEnvironment 释放环境占用（testing → pending）。
func (r *PGRepository) ReleaseEnvironment(ctx context.Context, tx Tx, envID int64) error {
	const q = `UPDATE environment SET status = 'pending', version = version + 1, updated_at = now()
	           WHERE id = $1 AND status = 'testing' AND deleted_at IS NULL`
	if _, err := r.conn(tx).Exec(ctx, q, envID); err != nil {
		return fmt.Errorf("释放环境失败: %w", err)
	}
	return nil
}

// EnvironmentSshHost 查询环境绑定的 SSH 主机 ID（未绑定返回 0）。
func (r *PGRepository) EnvironmentSshHost(ctx context.Context, envID int64) (int64, error) {
	const q = `SELECT COALESCE(ssh_host_id, 0) FROM environment WHERE id = $1 AND deleted_at IS NULL`
	var hostID int64
	err := r.pool.QueryRow(ctx, q, envID).Scan(&hostID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, errcode.ErrNotFound
		}
		return 0, fmt.Errorf("查询环境 SSH 主机失败: %w", err)
	}
	return hostID, nil
}

// ===================== 快照 =====================

// SnapshotInterfaces 固化接口范围快照（INV-5）。
//
// 认证头仅保存密文（headers_enc），不落明文（INV-7）；任一接口不存在/已删除 → 10301。
func (r *PGRepository) SnapshotInterfaces(ctx context.Context, tx Tx, ids []int64) ([]domain.InterfaceSnapshot, error) {
	if len(ids) == 0 {
		return []domain.InterfaceSnapshot{}, nil
	}
	const q = `
		SELECT i.id, i.product_id, COALESCE(p.name, ''), i.name, i.method,
		       COALESCE(i.scheme, 'http'), COALESCE(i.port, 80), COALESCE(i.path, '/'),
		       COALESCE(i.headers, '{}'::jsonb), COALESCE(i.headers_enc, ''::bytea),
		       COALESCE(i.body::text, ''), COALESCE(i.timeouts, '{}'::jsonb), i.status
		FROM interface i
		LEFT JOIN product p ON p.id = i.product_id
		WHERE i.deleted_at IS NULL AND i.id = ANY($1)
		ORDER BY i.id`
	rows, err := r.conn(tx).Query(ctx, q, ids)
	if err != nil {
		return nil, fmt.Errorf("查询接口快照失败: %w", err)
	}
	defer rows.Close()

	out := make([]domain.InterfaceSnapshot, 0, len(ids))
	for rows.Next() {
		var (
			snap     domain.InterfaceSnapshot
			headers  []byte
			timeouts []byte
		)
		if err := rows.Scan(&snap.InterfaceID, &snap.ProductID, &snap.ProductName, &snap.Name,
			&snap.Method, &snap.Scheme, &snap.Port, &snap.Path,
			&headers, &snap.AuthHeadersEnc, &snap.Body, &timeouts, &snap.Status); err != nil {
			return nil, fmt.Errorf("扫描接口快照失败: %w", err)
		}
		if len(headers) > 0 {
			if err := json.Unmarshal(headers, &snap.Headers); err != nil {
				return nil, fmt.Errorf("解析接口请求头失败: %w", err)
			}
		}
		if len(timeouts) > 0 {
			if err := json.Unmarshal(timeouts, &snap.Timeouts); err != nil {
				return nil, fmt.Errorf("解析接口超时配置失败: %w", err)
			}
		}
		snap.CapturedAt = nowUTC()
		out = append(out, snap)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) != len(dedupeInt64(ids)) {
		return nil, errcode.ErrNotFound
	}
	return out, nil
}

// SnapshotThresholds 固化阈值快照（含停用项，Enabled 标记交由评估器过滤）。
func (r *PGRepository) SnapshotThresholds(ctx context.Context, tx Tx, ids []int64) ([]domain.ThresholdSnapshot, error) {
	if len(ids) == 0 {
		return []domain.ThresholdSnapshot{}, nil
	}
	const q = `
		SELECT t.interface_id, COALESCE(i.name, ''), t.metric, t.operator, t.value,
		       COALESCE(t.unit, ''), t.enabled
		FROM threshold t
		JOIN interface i ON i.id = t.interface_id AND i.deleted_at IS NULL
		WHERE t.deleted_at IS NULL AND t.interface_id = ANY($1)
		ORDER BY t.interface_id, t.id`
	rows, err := r.conn(tx).Query(ctx, q, ids)
	if err != nil {
		return nil, fmt.Errorf("查询阈值快照失败: %w", err)
	}
	defer rows.Close()

	out := make([]domain.ThresholdSnapshot, 0, len(ids))
	for rows.Next() {
		var s domain.ThresholdSnapshot
		if err := rows.Scan(&s.InterfaceID, &s.InterfaceName, &s.Metric, &s.Operator, &s.Value, &s.Unit, &s.Enabled); err != nil {
			return nil, fmt.Errorf("扫描阈值快照失败: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ===================== 任务 =====================

// CreateTask 新建任务（事务内调用，状态固定 pending）。
func (r *PGRepository) CreateTask(ctx context.Context, tx Tx, task *domain.TestTask) error {
	productIDs, err := marshalIDs(task.ProductIDs)
	if err != nil {
		return err
	}
	interfaceIDs, err := marshalIDs(task.InterfaceIDs)
	if err != nil {
		return err
	}
	ifaceSnapshots, err := marshalSnapshot(task.InterfaceSnapshots)
	if err != nil {
		return err
	}
	thresholdSnapshots, err := marshalSnapshot(task.ThresholdSnapshots)
	if err != nil {
		return err
	}
	flow, err := marshalConfig(task.Flow)
	if err != nil {
		return err
	}
	reportConfig, err := marshalConfig(task.ReportConfig)
	if err != nil {
		return err
	}
	cleanConfig, err := marshalConfig(task.CleanConfig)
	if err != nil {
		return err
	}
	if task.Status == "" {
		task.Status = domain.TaskPending
	}

	const q = `
		INSERT INTO test_task
			(template_id, name, env_id, target_host, created_by, product_ids, interface_ids,
			 interface_range_snapshot, threshold_snapshot, flow, report_config, clean_config, status)
		VALUES (NULLIF($1, 0), $2, $3, $4, NULLIF($5, 0), $6::jsonb, $7::jsonb,
		        $8::jsonb, $9::jsonb, $10::jsonb, $11::jsonb, $12::jsonb, $13)
		RETURNING id, version, created_at, updated_at`
	err = r.conn(tx).QueryRow(ctx, q,
		task.TemplateID, task.Name, task.EnvID, task.TargetHost, task.CreatedBy, productIDs, interfaceIDs,
		ifaceSnapshots, thresholdSnapshots, flow, reportConfig, cleanConfig, string(task.Status),
	).Scan(&task.ID, &task.Version, &task.CreatedAt, &task.UpdatedAt)
	if err != nil {
		if isForeignKeyViolation(err) {
			return errcode.ErrNotFound
		}
		return fmt.Errorf("创建任务失败: %w", err)
	}
	return nil
}

// GetTask 任务详情（不存在 → 10301）。
func (r *PGRepository) GetTask(ctx context.Context, id int64) (*domain.TestTask, error) {
	const q = `SELECT ` + taskColumns + ` FROM test_task WHERE id = $1 AND deleted_at IS NULL`
	task, err := scanTask(r.pool.QueryRow(ctx, q, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errcode.ErrNotFound
		}
		return nil, fmt.Errorf("查询任务失败: %w", err)
	}
	return task, nil
}

// ListTasks 任务列表（关键字/状态/环境/产品/模板/创建时间 + 分页）。
func (r *PGRepository) ListTasks(ctx context.Context, q domain.TaskQuery) ([]domain.TestTask, int64, error) {
	pattern := ""
	if q.Keyword != "" {
		pattern = "%" + q.Keyword + "%"
	}
	const filter = `
		WHERE deleted_at IS NULL
		  AND ($1 = '' OR name ILIKE $1)
		  AND ($2 = '' OR status = $2)
		  AND ($3 = 0 OR env_id = $3)
		  AND ($4 = 0 OR product_ids @> jsonb_build_array($4::bigint))
		  AND ($5 = 0 OR template_id = $5)
		  AND ($6::timestamptz IS NULL OR created_at >= $6)
		  AND ($7::timestamptz IS NULL OR created_at <= $7)`

	var total int64
	if err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM test_task`+filter,
		pattern, string(q.Status), q.EnvID, q.ProductID, q.TemplateID, q.CreatedFrom, q.CreatedTo,
	).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计任务失败: %w", err)
	}

	offset := (q.Page - 1) * q.PageSize
	if offset < 0 {
		offset = 0
	}
	query := `SELECT ` + taskColumns + ` FROM test_task` + filter +
		` ORDER BY id DESC LIMIT $8 OFFSET $9`
	rows, err := r.pool.Query(ctx, query,
		pattern, string(q.Status), q.EnvID, q.ProductID, q.TemplateID, q.CreatedFrom, q.CreatedTo,
		q.PageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("查询任务列表失败: %w", err)
	}
	defer rows.Close()

	out := make([]domain.TestTask, 0, q.PageSize)
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("扫描任务失败: %w", err)
		}
		out = append(out, *t)
	}
	return out, total, rows.Err()
}

// TransitionTask 条件式状态流转（from 不匹配 → 10401，不存在 → 10301）。
//
// 条件更新保证并发下只有一个状态迁移成功（例如 cancel 与执行完成竞态）。
func (r *PGRepository) TransitionTask(ctx context.Context, tx Tx, id int64, from, to domain.TaskStatus) error {
	const q = `UPDATE test_task SET status = $3, version = version + 1, updated_at = now()
	           WHERE id = $1 AND status = $2 AND deleted_at IS NULL`
	tag, err := r.conn(tx).Exec(ctx, q, id, string(from), string(to))
	if err != nil {
		return fmt.Errorf("更新任务状态失败: %w", err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}

	var exists bool
	if err := r.conn(tx).QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM test_task WHERE id = $1 AND deleted_at IS NULL)`, id).Scan(&exists); err != nil {
		return fmt.Errorf("校验任务存在性失败: %w", err)
	}
	if !exists {
		return errcode.ErrNotFound
	}
	return errcode.ErrState
}

// SetTaskStarted 记录任务开始时间。
func (r *PGRepository) SetTaskStarted(ctx context.Context, tx Tx, id int64, at time.Time) error {
	return r.updateTaskExecution(ctx, tx, id, `started_at = $2`, at)
}

// SetTaskFinished 记录任务结束时间。
func (r *PGRepository) SetTaskFinished(ctx context.Context, tx Tx, id int64, at time.Time) error {
	return r.updateTaskExecution(ctx, tx, id, `finished_at = $2`, at)
}

// SetTaskResult 写入执行/评估结果摘要（JSONB）。
func (r *PGRepository) SetTaskResult(ctx context.Context, tx Tx, id int64, result []byte) error {
	if len(result) == 0 {
		result = []byte("{}")
	}
	const q = `UPDATE test_task SET result = $2::jsonb, version = version + 1, updated_at = now()
	           WHERE id = $1 AND deleted_at IS NULL`
	tag, err := r.conn(tx).Exec(ctx, q, id, string(result))
	if err != nil {
		return fmt.Errorf("写入任务结果失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errcode.ErrNotFound
	}
	return nil
}

// UpdateTaskSnapshots 刷新任务的接口/阈值快照（仅待执行任务使用）。
func (r *PGRepository) UpdateTaskSnapshots(ctx context.Context, tx Tx, id int64, interfaces []domain.InterfaceSnapshot, thresholds []domain.ThresholdSnapshot) error {
	ifaceSnapshots, err := marshalSnapshot(interfaces)
	if err != nil {
		return err
	}
	thresholdSnapshots, err := marshalSnapshot(thresholds)
	if err != nil {
		return err
	}
	const q = `UPDATE test_task SET interface_range_snapshot = $2::jsonb,
	           threshold_snapshot = $3::jsonb, version = version + 1, updated_at = now()
	         WHERE id = $1 AND deleted_at IS NULL`
	tag, err := r.conn(tx).Exec(ctx, q, id, ifaceSnapshots, thresholdSnapshots)
	if err != nil {
		return fmt.Errorf("刷新任务快照失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errcode.ErrNotFound
	}
	return nil
}

// ResetTaskExecution 清理执行痕迹（重试前调用）。
func (r *PGRepository) ResetTaskExecution(ctx context.Context, tx Tx, id int64) error {
	const q = `UPDATE test_task SET started_at = NULL, finished_at = NULL, result = NULL,
	           version = version + 1, updated_at = now()
	           WHERE id = $1 AND deleted_at IS NULL`
	tag, err := r.conn(tx).Exec(ctx, q, id)
	if err != nil {
		return fmt.Errorf("重置任务执行信息失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errcode.ErrNotFound
	}
	return nil
}

// updateTaskExecution 更新单个时间字段（列名来自内部常量，无用户输入）。
func (r *PGRepository) updateTaskExecution(ctx context.Context, tx Tx, id int64, setClause string, at time.Time) error {
	q := `UPDATE test_task SET ` + setClause + `, version = version + 1, updated_at = now()
	      WHERE id = $1 AND deleted_at IS NULL`
	tag, err := r.conn(tx).Exec(ctx, q, id, at)
	if err != nil {
		return fmt.Errorf("更新任务执行时间失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errcode.ErrNotFound
	}
	return nil
}

// ===================== 测试日志 =====================

// AppendTestLog 追加一条测试执行日志（应用层只追加）。
func (r *PGRepository) AppendTestLog(ctx context.Context, taskID, seq int64, level, message string) error {
	const q = `INSERT INTO test_log (task_id, seq, level, message) VALUES ($1, $2, $3, $4)`
	if _, err := r.pool.Exec(ctx, q, taskID, seq, level, message); err != nil {
		return fmt.Errorf("写入测试日志失败: %w", err)
	}
	return nil
}

// ListTestLogs 按任务查询测试日志（seq 升序，最多 limit 条）。
func (r *PGRepository) ListTestLogs(ctx context.Context, taskID int64, limit int) ([]domain.TestLog, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	const q = `SELECT id, COALESCE(task_id, 0), COALESCE(seq, 0), level, COALESCE(message, ''), created_at
	           FROM test_log WHERE task_id = $1 ORDER BY seq, id LIMIT $2`
	rows, err := r.pool.Query(ctx, q, taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("查询测试日志失败: %w", err)
	}
	defer rows.Close()

	out := make([]domain.TestLog, 0, limit)
	for rows.Next() {
		var l domain.TestLog
		if err := rows.Scan(&l.ID, &l.TaskID, &l.Seq, &l.Level, &l.Message, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("扫描测试日志失败: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}
