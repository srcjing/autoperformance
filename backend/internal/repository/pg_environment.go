package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// 环境相关查询列（顺序与 scan* 一致）。
const (
	categoryColumns = `id, name, COALESCE(description, ''), version, created_at, updated_at`

	tagColumns = `t.id, t.name, COALESCE(t.color, ''), COALESCE(t.description, ''), t.version, t.created_at, t.updated_at`

	environmentColumns = `e.id, e.name, e.code, e.category_id, e.type, COALESCE(e.hosts, '[]'::jsonb),
		COALESCE(e.os, ''), COALESCE(e.ssh_host_id, 0), COALESCE(e.cert_id, 0),
		COALESCE(e.description, ''), e.status, e.version, e.created_at, e.updated_at`
)

// scanCategory 扫描一行环境分类。
func scanCategory(row pgx.Row) (*domain.EnvironmentCategory, error) {
	var c domain.EnvironmentCategory
	if err := row.Scan(&c.ID, &c.Name, &c.Description, &c.Version, &c.CreatedAt, &c.UpdatedAt); err != nil {
		return nil, err
	}
	return &c, nil
}

// scanTag 扫描一行标签（columns 与 tagColumns 一致，可附带关联环境数）。
func scanTag(row pgx.Row, withCount bool) (*domain.Tag, error) {
	var t domain.Tag
	dest := []any{&t.ID, &t.Name, &t.Color, &t.Description, &t.Version, &t.CreatedAt, &t.UpdatedAt}
	if withCount {
		dest = append(dest, &t.EnvironmentCount)
	}
	if err := row.Scan(dest...); err != nil {
		return nil, err
	}
	return &t, nil
}

// scanEnvironment 扫描一行环境（hosts 由 JSONB 解码为字符串列表）。
func scanEnvironment(row pgx.Row) (*domain.Environment, error) {
	var env domain.Environment
	var hosts []byte
	err := row.Scan(
		&env.ID, &env.Name, &env.Code, &env.CategoryID, &env.Type, &hosts,
		&env.OS, &env.SshHostID, &env.CertID, &env.Description,
		&env.Status, &env.Version, &env.CreatedAt, &env.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if len(hosts) > 0 {
		if err := json.Unmarshal(hosts, &env.Hosts); err != nil {
			return nil, fmt.Errorf("解析环境主机列表失败: %w", err)
		}
	}
	if env.Hosts == nil {
		env.Hosts = []string{}
	}
	return &env, nil
}

// isForeignKeyViolation 判断是否为外键约束冲突（PostgreSQL 23503）。
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

// nullableID 将 0 视为未绑定（DB NULL）。
func nullableID(id int64) any {
	if id <= 0 {
		return nil
	}
	return id
}

// marshalHosts 序列化主机列表为 JSONB 文本。
func marshalHosts(hosts []string) (string, error) {
	if hosts == nil {
		hosts = []string{}
	}
	raw, err := json.Marshal(hosts)
	if err != nil {
		return "", fmt.Errorf("序列化主机列表失败: %w", err)
	}
	return string(raw), nil
}

// ===================== 环境分类 =====================

// ListCategories 环境分类列表（按 ID 稳定排序）。
func (r *PGRepository) ListCategories(ctx context.Context) ([]domain.EnvironmentCategory, error) {
	const q = `SELECT ` + categoryColumns + ` FROM environment_category WHERE deleted_at IS NULL ORDER BY id`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("查询环境分类失败: %w", err)
	}
	defer rows.Close()

	out := make([]domain.EnvironmentCategory, 0)
	for rows.Next() {
		c, err := scanCategory(rows)
		if err != nil {
			return nil, fmt.Errorf("扫描环境分类失败: %w", err)
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// GetCategory 环境分类详情（不存在 → 10301）。
func (r *PGRepository) GetCategory(ctx context.Context, categoryID int64) (*domain.EnvironmentCategory, error) {
	const q = `SELECT ` + categoryColumns + ` FROM environment_category WHERE id = $1 AND deleted_at IS NULL`
	c, err := scanCategory(r.pool.QueryRow(ctx, q, categoryID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errcode.ErrNotFound
		}
		return nil, fmt.Errorf("查询环境分类失败: %w", err)
	}
	return c, nil
}

// CreateCategory 新建环境分类（重名 → 10302）。
func (r *PGRepository) CreateCategory(ctx context.Context, category *domain.EnvironmentCategory) error {
	const q = `INSERT INTO environment_category (name, description) VALUES ($1, $2)
	           RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q, category.Name, category.Description).
		Scan(&category.ID, &category.Version, &category.CreatedAt, &category.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return errcode.ErrConflict
		}
		return fmt.Errorf("创建环境分类失败: %w", err)
	}
	return nil
}

// UpdateCategory 修改环境分类（重名 → 10302）。
func (r *PGRepository) UpdateCategory(ctx context.Context, category *domain.EnvironmentCategory) error {
	sets := map[string]any{"name": category.Name, "description": category.Description}
	if err := updateByID(ctx, r.pool, "environment_category", category.ID, sets); err != nil {
		if isUniqueViolation(err) {
			return errcode.ErrConflict
		}
		return err
	}
	return nil
}

// SoftDeleteCategory 逻辑删除环境分类（引用校验在服务层完成）。
func (r *PGRepository) SoftDeleteCategory(ctx context.Context, categoryID int64) error {
	return applySoftDelete(ctx, r.pool, "environment_category", categoryID)
}

// CountEnvironmentsByCategory 统计引用该分类的（未删除）环境数。
func (r *PGRepository) CountEnvironmentsByCategory(ctx context.Context, categoryID int64) (int64, error) {
	const q = `SELECT count(*) FROM environment WHERE category_id = $1 AND deleted_at IS NULL`
	var n int64
	if err := r.pool.QueryRow(ctx, q, categoryID).Scan(&n); err != nil {
		return 0, fmt.Errorf("统计分类引用失败: %w", err)
	}
	return n, nil
}

// ===================== 标签 =====================

// ListTags 标签列表（含关联环境数，仅统计未删除环境）。
func (r *PGRepository) ListTags(ctx context.Context) ([]domain.Tag, error) {
	const q = `
		SELECT ` + tagColumns + `, count(e.id)
		FROM tag t
		LEFT JOIN env_tag et ON et.tag_id = t.id
		LEFT JOIN environment e ON e.id = et.env_id AND e.deleted_at IS NULL
		WHERE t.deleted_at IS NULL
		GROUP BY t.id
		ORDER BY t.id`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("查询标签列表失败: %w", err)
	}
	defer rows.Close()

	out := make([]domain.Tag, 0)
	for rows.Next() {
		t, err := scanTag(rows, true)
		if err != nil {
			return nil, fmt.Errorf("扫描标签失败: %w", err)
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// GetTag 标签详情（含关联环境数；不存在 → 10301）。
func (r *PGRepository) GetTag(ctx context.Context, tagID int64) (*domain.Tag, error) {
	const q = `
		SELECT ` + tagColumns + `, count(e.id)
		FROM tag t
		LEFT JOIN env_tag et ON et.tag_id = t.id
		LEFT JOIN environment e ON e.id = et.env_id AND e.deleted_at IS NULL
		WHERE t.deleted_at IS NULL AND t.id = $1
		GROUP BY t.id`
	t, err := scanTag(r.pool.QueryRow(ctx, q, tagID), true)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errcode.ErrNotFound
		}
		return nil, fmt.Errorf("查询标签失败: %w", err)
	}
	return t, nil
}

// CreateTag 新建标签（重名 → 10302）。
func (r *PGRepository) CreateTag(ctx context.Context, tag *domain.Tag) error {
	const q = `INSERT INTO tag (name, color, description) VALUES ($1, $2, $3)
	           RETURNING id, version, created_at, updated_at`
	err := r.pool.QueryRow(ctx, q, tag.Name, tag.Color, tag.Description).
		Scan(&tag.ID, &tag.Version, &tag.CreatedAt, &tag.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return errcode.ErrConflict
		}
		return fmt.Errorf("创建标签失败: %w", err)
	}
	return nil
}

// UpdateTag 修改标签（重名 → 10302）。
func (r *PGRepository) UpdateTag(ctx context.Context, tag *domain.Tag) error {
	sets := map[string]any{"name": tag.Name, "color": tag.Color, "description": tag.Description}
	if err := updateByID(ctx, r.pool, "tag", tag.ID, sets); err != nil {
		if isUniqueViolation(err) {
			return errcode.ErrConflict
		}
		return err
	}
	return nil
}

// SoftDeleteTag 逻辑删除标签。
func (r *PGRepository) SoftDeleteTag(ctx context.Context, tagID int64) error {
	return applySoftDelete(ctx, r.pool, "tag", tagID)
}

// CountEnvironmentsByTag 统计引用该标签的（未删除）环境数。
func (r *PGRepository) CountEnvironmentsByTag(ctx context.Context, tagID int64) (int64, error) {
	const q = `
		SELECT count(*)
		FROM env_tag et
		JOIN environment e ON e.id = et.env_id AND e.deleted_at IS NULL
		WHERE et.tag_id = $1`
	var n int64
	if err := r.pool.QueryRow(ctx, q, tagID).Scan(&n); err != nil {
		return 0, fmt.Errorf("统计标签引用失败: %w", err)
	}
	return n, nil
}

// CountTagsByIDs 统计存在（未删除）的标签数量，用于标签绑定校验。
func (r *PGRepository) CountTagsByIDs(ctx context.Context, tagIDs []int64) (int64, error) {
	if len(tagIDs) == 0 {
		return 0, nil
	}
	const q = `SELECT count(*) FROM tag WHERE deleted_at IS NULL AND id = ANY($1)`
	var n int64
	if err := r.pool.QueryRow(ctx, q, tagIDs).Scan(&n); err != nil {
		return 0, fmt.Errorf("校验标签存在性失败: %w", err)
	}
	return n, nil
}

// ===================== 环境 =====================

// envFilterClause 环境列表公共过滤条件（$1 关键字、$2 分类、$3 状态、$4 标签交集）。
const envFilterClause = `
	WHERE e.deleted_at IS NULL
	  AND ($1 = '' OR e.name ILIKE $1 OR e.code ILIKE $1)
	  AND ($2 = 0 OR e.category_id = $2)
	  AND ($3 = '' OR e.status = $3)
	  AND ($4::bigint[] IS NULL OR (
	        SELECT count(DISTINCT et.tag_id) FROM env_tag et
	        WHERE et.env_id = e.id AND et.tag_id = ANY($4::bigint[])
	      ) = cardinality($4::bigint[]))`

// ListEnvironments 环境列表（关键字/分类/状态/标签交集 + 分页）。
func (r *PGRepository) ListEnvironments(ctx context.Context, q domain.EnvironmentQuery) ([]domain.Environment, int64, error) {
	pattern := ""
	if q.Keyword != "" {
		pattern = "%" + q.Keyword + "%"
	}
	var tagIDs []int64
	if len(q.TagIDs) > 0 {
		tagIDs = q.TagIDs
	}

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM environment e `+envFilterClause,
		pattern, q.CategoryID, q.Status, tagIDs).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计环境失败: %w", err)
	}

	offset := (q.Page - 1) * q.PageSize
	if offset < 0 {
		offset = 0
	}
	query := `SELECT ` + environmentColumns + ` FROM environment e ` + envFilterClause +
		` ORDER BY e.id LIMIT $5 OFFSET $6`

	rows, err := r.pool.Query(ctx, query, pattern, q.CategoryID, q.Status, tagIDs, q.PageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("查询环境列表失败: %w", err)
	}
	defer rows.Close()

	out := make([]domain.Environment, 0, q.PageSize)
	for rows.Next() {
		env, err := scanEnvironment(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("扫描环境失败: %w", err)
		}
		out = append(out, *env)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	tagged, err := r.attachTags(ctx, out)
	if err != nil {
		return nil, 0, err
	}
	return tagged, total, nil
}

// EnvironmentsByTag 标签反查环境（分页）。
func (r *PGRepository) EnvironmentsByTag(ctx context.Context, tagID int64, page, pageSize int) ([]domain.Environment, int64, error) {
	const countQ = `
		SELECT count(*)
		FROM env_tag et
		JOIN environment e ON e.id = et.env_id AND e.deleted_at IS NULL
		WHERE et.tag_id = $1`
	var total int64
	if err := r.pool.QueryRow(ctx, countQ, tagID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计标签关联环境失败: %w", err)
	}

	offset := (page - 1) * pageSize
	if offset < 0 {
		offset = 0
	}
	const q = `
		SELECT ` + environmentColumns + `
		FROM environment e
		JOIN env_tag et ON et.env_id = e.id
		WHERE et.tag_id = $1 AND e.deleted_at IS NULL
		ORDER BY e.id
		LIMIT $2 OFFSET $3`
	rows, err := r.pool.Query(ctx, q, tagID, pageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("查询标签关联环境失败: %w", err)
	}
	defer rows.Close()

	out := make([]domain.Environment, 0, pageSize)
	for rows.Next() {
		env, err := scanEnvironment(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("扫描环境失败: %w", err)
		}
		out = append(out, *env)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	tagged, err := r.attachTags(ctx, out)
	if err != nil {
		return nil, 0, err
	}
	return tagged, total, nil
}

// GetEnvironment 环境详情（含标签 ID；不存在 → 10301）。
func (r *PGRepository) GetEnvironment(ctx context.Context, envID int64) (*domain.Environment, error) {
	const q = `SELECT ` + environmentColumns + ` FROM environment e WHERE e.id = $1 AND e.deleted_at IS NULL`
	env, err := scanEnvironment(r.pool.QueryRow(ctx, q, envID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errcode.ErrNotFound
		}
		return nil, fmt.Errorf("查询环境失败: %w", err)
	}
	tagged, err := r.attachTags(ctx, []domain.Environment{*env})
	if err != nil {
		return nil, err
	}
	return &tagged[0], nil
}

// CreateEnvironment 新建环境（编码唯一冲突 → 10302，由服务层映射 21001）。
func (r *PGRepository) CreateEnvironment(ctx context.Context, env *domain.Environment) error {
	hosts, err := marshalHosts(env.Hosts)
	if err != nil {
		return err
	}
	const q = `
		INSERT INTO environment (name, code, category_id, type, hosts, os, ssh_host_id, cert_id, description, status)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7, $8, $9, $10)
		RETURNING id, version, created_at, updated_at`
	err = r.pool.QueryRow(ctx, q,
		env.Name, env.Code, env.CategoryID, env.Type, hosts, env.OS,
		nullableID(env.SshHostID), nullableID(env.CertID), env.Description, env.Status,
	).Scan(&env.ID, &env.Version, &env.CreatedAt, &env.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return errcode.ErrConflict
		}
		if isForeignKeyViolation(err) {
			return errcode.ErrNotFound
		}
		return fmt.Errorf("创建环境失败: %w", err)
	}
	return nil
}

// UpdateEnvironment 全量修改环境（version 乐观锁：冲突 → 10402）。
func (r *PGRepository) UpdateEnvironment(ctx context.Context, env *domain.Environment, version int64) error {
	hosts, err := marshalHosts(env.Hosts)
	if err != nil {
		return err
	}
	const q = `
		UPDATE environment
		SET name = $3, code = $4, category_id = $5, type = $6, hosts = $7::jsonb, os = $8,
		    ssh_host_id = $9, cert_id = $10, description = $11,
		    version = version + 1, updated_at = now()
		WHERE id = $1 AND version = $2 AND deleted_at IS NULL`
	tag, err := r.pool.Exec(ctx, q,
		env.ID, version, env.Name, env.Code, env.CategoryID, env.Type, hosts, env.OS,
		nullableID(env.SshHostID), nullableID(env.CertID), env.Description,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return errcode.ErrConflict
		}
		if isForeignKeyViolation(err) {
			return errcode.ErrNotFound
		}
		return fmt.Errorf("修改环境失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errcode.ErrConcurrent
	}
	return nil
}

// UpdateEnvironmentStatus 停用/启用环境；testing 环境拒绝（DB 侧兜底防占用篡改）。
func (r *PGRepository) UpdateEnvironmentStatus(ctx context.Context, envID int64, status string) error {
	const q = `UPDATE environment SET status = $2, version = version + 1, updated_at = now()
	           WHERE id = $1 AND deleted_at IS NULL AND status <> 'testing'`
	tag, err := r.pool.Exec(ctx, q, envID, status)
	if err != nil {
		return fmt.Errorf("修改环境状态失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return r.environmentGuardErr(ctx, envID)
	}
	return nil
}

// SoftDeleteEnvironment 逻辑删除环境并解绑标签；testing 环境拒绝。
func (r *PGRepository) SoftDeleteEnvironment(ctx context.Context, envID int64) error {
	const q = `UPDATE environment SET deleted_at = now()
	           WHERE id = $1 AND deleted_at IS NULL AND status <> 'testing'`
	tag, err := r.pool.Exec(ctx, q, envID)
	if err != nil {
		return fmt.Errorf("删除环境失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return r.environmentGuardErr(ctx, envID)
	}
	if _, err := r.pool.Exec(ctx, `DELETE FROM env_tag WHERE env_id = $1`, envID); err != nil {
		return fmt.Errorf("解绑环境标签失败: %w", err)
	}
	return nil
}

// environmentGuardErr 在 UPDATE 未命中时区分「不存在」与「被任务占用」。
func (r *PGRepository) environmentGuardErr(ctx context.Context, envID int64) error {
	const q = `SELECT status FROM environment WHERE id = $1 AND deleted_at IS NULL`
	var status string
	if err := r.pool.QueryRow(ctx, q, envID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errcode.ErrNotFound
		}
		return fmt.Errorf("校验环境状态失败: %w", err)
	}
	if status == domain.EnvStatusTesting {
		return errcode.ErrEnvBusy
	}
	return errcode.ErrState
}

// ReplaceEnvironmentTags 全量替换环境标签绑定（单事务先删后插）。
func (r *PGRepository) ReplaceEnvironmentTags(ctx context.Context, envID int64, tagIDs []int64) error {
	return r.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM env_tag WHERE env_id = $1`, envID); err != nil {
			return fmt.Errorf("清空环境标签失败: %w", err)
		}
		for _, tagID := range tagIDs {
			if _, err := tx.Exec(ctx,
				`INSERT INTO env_tag (env_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
				envID, tagID); err != nil {
				if isForeignKeyViolation(err) {
					return errcode.ErrNotFound
				}
				return fmt.Errorf("绑定环境标签失败: %w", err)
			}
		}
		return nil
	})
}

// attachTags 批量填充环境标签 ID（列表页避免 N+1）。
func (r *PGRepository) attachTags(ctx context.Context, envs []domain.Environment) ([]domain.Environment, error) {
	if len(envs) == 0 {
		return envs, nil
	}
	ids := make([]int64, 0, len(envs))
	for i := range envs {
		ids = append(ids, envs[i].ID)
	}

	const q = `
		SELECT et.env_id, et.tag_id
		FROM env_tag et
		JOIN tag t ON t.id = et.tag_id AND t.deleted_at IS NULL
		WHERE et.env_id = ANY($1)
		ORDER BY et.env_id, t.id`
	rows, err := r.pool.Query(ctx, q, ids)
	if err != nil {
		return nil, fmt.Errorf("查询环境标签失败: %w", err)
	}
	defer rows.Close()

	byEnv := make(map[int64][]int64, len(envs))
	for rows.Next() {
		var envID, tagID int64
		if err := rows.Scan(&envID, &tagID); err != nil {
			return nil, fmt.Errorf("扫描环境标签失败: %w", err)
		}
		byEnv[envID] = append(byEnv[envID], tagID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range envs {
		if tags, ok := byEnv[envs[i].ID]; ok {
			envs[i].TagIDs = tags
		} else {
			envs[i].TagIDs = []int64{}
		}
	}
	return envs, nil
}
