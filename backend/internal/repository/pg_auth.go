package repository

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"auto_performance/internal/auth"
	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// execer 执行 SQL 的最小接口（*pgxpool.Pool 与 pgx.Tx 均实现）。
type execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

// adminColumns admin 表查询列（顺序与 scanAdmin 一致）。
const adminColumns = `id, username, name, phone, email, description, password_hash,
	must_change_password, status, last_login_at, is_builtin, version, created_at, updated_at`

// scanAdmin 扫描一行 admin 记录。
func scanAdmin(row pgx.Row) (*domain.Admin, error) {
	var a domain.Admin
	err := row.Scan(
		&a.ID, &a.Username, &a.Name, &a.Phone, &a.Email, &a.Description, &a.PasswordHash,
		&a.MustChangePassword, &a.Status, &a.LastLoginAt, &a.IsBuiltin,
		&a.Version, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// isUniqueViolation 判断是否为唯一约束冲突（PostgreSQL 23505）。
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// GetAdminByUsername 按登录名查询管理员（不含软删除）。
func (r *PGRepository) GetAdminByUsername(ctx context.Context, username string) (*domain.Admin, error) {
	q := `SELECT ` + adminColumns + ` FROM admin WHERE username = $1 AND deleted_at IS NULL`

	a, err := scanAdmin(r.pool.QueryRow(ctx, q, username))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errcode.ErrNotFound
		}
		return nil, fmt.Errorf("查询管理员失败: %w", err)
	}
	return a, nil
}

// GetAdminByID 按 ID 查询管理员（供登录快照、改密与设置模块复用）。
func (r *PGRepository) GetAdminByID(ctx context.Context, adminID int64) (*domain.Admin, error) {
	q := `SELECT ` + adminColumns + ` FROM admin WHERE id = $1 AND deleted_at IS NULL`

	a, err := scanAdmin(r.pool.QueryRow(ctx, q, adminID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errcode.ErrNotFound
		}
		return nil, fmt.Errorf("查询管理员失败: %w", err)
	}
	return a, nil
}

// UpdateLastLogin 记录最后登录时间（审计字段，不参与乐观锁版本）。
func (r *PGRepository) UpdateLastLogin(ctx context.Context, adminID int64, at time.Time) error {
	const q = `UPDATE admin SET last_login_at = $2 WHERE id = $1 AND deleted_at IS NULL`
	if _, err := r.pool.Exec(ctx, q, adminID, at); err != nil {
		return fmt.Errorf("更新最后登录时间失败: %w", err)
	}
	return nil
}

// UpdatePassword 更新口令哈希与首登改密标记（新口令 bcrypt 存储，见 DES-I-001）。
func (r *PGRepository) UpdatePassword(ctx context.Context, adminID int64, passwordHash string, mustChange bool) error {
	const q = `UPDATE admin
	           SET password_hash = $2, must_change_password = $3,
	               version = version + 1, updated_at = now()
	           WHERE id = $1 AND deleted_at IS NULL`
	tag, err := r.pool.Exec(ctx, q, adminID, passwordHash, mustChange)
	if err != nil {
		return fmt.Errorf("更新口令失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errcode.ErrNotFound
	}
	return nil
}

// LoadRolesByAdmin 查询管理员绑定的角色列表。
func (r *PGRepository) LoadRolesByAdmin(ctx context.Context, adminID int64) ([]domain.Role, error) {
	const q = `
		SELECT r.id, r.name, r.description, r.version, r.created_at, r.updated_at
		FROM role r
		JOIN admin_role ar ON ar.role_id = r.id
		WHERE ar.admin_id = $1 AND r.deleted_at IS NULL
		ORDER BY r.id`

	rows, err := r.pool.Query(ctx, q, adminID)
	if err != nil {
		return nil, fmt.Errorf("查询管理员角色失败: %w", err)
	}
	defer rows.Close()

	roles := make([]domain.Role, 0)
	for rows.Next() {
		var role domain.Role
		if err := rows.Scan(&role.ID, &role.Name, &role.Description, &role.Version, &role.CreatedAt, &role.UpdatedAt); err != nil {
			return nil, fmt.Errorf("扫描角色失败: %w", err)
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

// ListPermissionsByAdmin 计算管理员最终权限并集（INV-1）。
// 内置 admin 返回全量权限（不依赖 role 表）。
func (r *PGRepository) ListPermissionsByAdmin(ctx context.Context, adminID int64) (map[string]bool, error) {
	a, err := r.GetAdminByID(ctx, adminID)
	if err != nil {
		return nil, err
	}
	if a.IsBuiltinAdmin() {
		return auth.SkeletonPermissions(), nil
	}

	const q = `
		SELECT rp.module_code, rp.permission
		FROM role_permission rp
		JOIN admin_role ar ON ar.role_id = rp.role_id
		JOIN role r ON r.id = rp.role_id AND r.deleted_at IS NULL
		WHERE ar.admin_id = $1 AND rp.deleted_at IS NULL`

	rows, err := r.pool.Query(ctx, q, adminID)
	if err != nil {
		return nil, fmt.Errorf("查询权限失败: %w", err)
	}
	defer rows.Close()

	grants := make([]auth.PermissionGrant, 0)
	for rows.Next() {
		var g auth.PermissionGrant
		if err := rows.Scan(&g.Module, &g.Permission); err != nil {
			return nil, fmt.Errorf("扫描权限失败: %w", err)
		}
		grants = append(grants, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return auth.UnionPermissions(grants), nil
}

// optimisticUpdate 通用乐观锁更新：SET sets（列=值）+ version=version+1 + updated_at=now()
// 条件 WHERE id=$1 AND version=$2 AND deleted_at IS NULL；RowsAffected==0 → errcode.ErrConcurrent。
// sets 的键为列名，值按参数顺序绑定（键排序保证 SQL 稳定）。
func optimisticUpdate(ctx context.Context, e execer, table string, id, version int64, sets map[string]any) error {
	if table == "" {
		return fmt.Errorf("optimisticUpdate: 表名不能为空")
	}

	keys := sortedKeys(sets)
	args := make([]any, 0, len(keys)+2)
	args = append(args, id, version)
	setClauses := make([]string, 0, len(keys))
	for i, k := range keys {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", k, i+3))
		args = append(args, sets[k])
	}

	sql := fmt.Sprintf(
		"UPDATE %s SET %s, version = version + 1, updated_at = now() WHERE id = $1 AND version = $2 AND deleted_at IS NULL",
		table,
		strings.Join(setClauses, ", "),
	)

	tag, err := e.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("乐观锁更新失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errcode.ErrConcurrent
	}
	return nil
}

// updateByID 无版本条件的更新（version 参数缺省时使用）；不存在返回 errcode.ErrNotFound。
func updateByID(ctx context.Context, e execer, table string, id int64, sets map[string]any) error {
	if table == "" {
		return fmt.Errorf("updateByID: 表名不能为空")
	}

	keys := sortedKeys(sets)
	args := make([]any, 0, len(keys)+1)
	args = append(args, id)
	setClauses := make([]string, 0, len(keys))
	for i, k := range keys {
		setClauses = append(setClauses, fmt.Sprintf("%s = $%d", k, i+2))
		args = append(args, sets[k])
	}

	sql := fmt.Sprintf(
		"UPDATE %s SET %s, version = version + 1, updated_at = now() WHERE id = $1 AND deleted_at IS NULL",
		table,
		strings.Join(setClauses, ", "),
	)
	tag, err := e.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("更新失败: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errcode.ErrNotFound
	}
	return nil
}

// sortedKeys 返回排序后的列名（保证 SQL 与参数顺序稳定）。
func sortedKeys(sets map[string]any) []string {
	keys := make([]string, 0, len(sets))
	for k := range sets {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// applySoftDelete 通用软删除：SET deleted_at = now() WHERE id=$1 AND deleted_at IS NULL。
func applySoftDelete(ctx context.Context, e execer, table string, id int64) error {
	if table == "" {
		return fmt.Errorf("applySoftDelete: 表名不能为空")
	}
	sql := fmt.Sprintf("UPDATE %s SET deleted_at = now() WHERE id = $1 AND deleted_at IS NULL", table)
	_, err := e.Exec(ctx, sql, id)
	if err != nil {
		return fmt.Errorf("软删除失败: %w", err)
	}
	return nil
}
