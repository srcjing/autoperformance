package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// roleColumns role 表查询列（顺序与 scanRole 一致）。
const roleColumns = `id, name, description, version, created_at, updated_at`

// scanRole 扫描一行 role 记录。
func scanRole(row pgx.Row) (*domain.Role, error) {
	var r domain.Role
	if err := row.Scan(&r.ID, &r.Name, &r.Description, &r.Version, &r.CreatedAt, &r.UpdatedAt); err != nil {
		return nil, err
	}
	return &r, nil
}

// ===================== 管理员 =====================

// ListAdmins 管理员列表（用户名/姓名模糊匹配 + 分页）。
func (r *PGRepository) ListAdmins(ctx context.Context, keyword string, page, pageSize int) ([]domain.Admin, int64, error) {
	pattern := ""
	if keyword != "" {
		pattern = "%" + keyword + "%"
	}
	const where = `WHERE deleted_at IS NULL AND ($1 = '' OR username ILIKE $1 OR name ILIKE $1)`

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM admin `+where, pattern).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("统计管理员失败: %w", err)
	}

	offset := (page - 1) * pageSize
	if offset < 0 {
		offset = 0
	}
	q := `SELECT ` + adminColumns + ` FROM admin ` + where + ` ORDER BY id LIMIT $2 OFFSET $3`

	rows, err := r.pool.Query(ctx, q, pattern, pageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("查询管理员列表失败: %w", err)
	}
	defer rows.Close()

	admins := make([]domain.Admin, 0, pageSize)
	for rows.Next() {
		a, err := scanAdmin(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("扫描管理员失败: %w", err)
		}
		admins = append(admins, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return admins, total, nil
}

// CreateAdmin 新建管理员（唯一约束冲突 → errcode.ErrConflict）。
func (r *PGRepository) CreateAdmin(ctx context.Context, admin *domain.Admin) error {
	const q = `
		INSERT INTO admin (username, name, phone, email, description, password_hash,
		                   must_change_password, status, is_builtin)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, version, created_at, updated_at`

	err := r.pool.QueryRow(ctx, q,
		admin.Username, admin.Name, admin.Phone, admin.Email, admin.Description,
		admin.PasswordHash, admin.MustChangePassword, admin.Status, admin.IsBuiltin,
	).Scan(&admin.ID, &admin.Version, &admin.CreatedAt, &admin.UpdatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return errcode.ErrConflict
		}
		return fmt.Errorf("创建管理员失败: %w", err)
	}
	return nil
}

// UpdateAdmin 修改管理员基础信息（version>0 启用乐观锁）。
func (r *PGRepository) UpdateAdmin(ctx context.Context, adminID int64, name, phone, email, description string, version int64) error {
	sets := map[string]any{
		"name":        name,
		"phone":       phone,
		"email":       email,
		"description": description,
	}
	if version > 0 {
		return optimisticUpdate(ctx, r.pool, "admin", adminID, version, sets)
	}
	return updateByID(ctx, r.pool, "admin", adminID, sets)
}

// UpdateAdminStatus 停用/启用管理员。
func (r *PGRepository) UpdateAdminStatus(ctx context.Context, adminID int64, status string, version int64) error {
	sets := map[string]any{"status": status}
	if version > 0 {
		return optimisticUpdate(ctx, r.pool, "admin", adminID, version, sets)
	}
	return updateByID(ctx, r.pool, "admin", adminID, sets)
}

// SoftDeleteAdmin 逻辑删除管理员并解绑角色。
func (r *PGRepository) SoftDeleteAdmin(ctx context.Context, adminID int64) error {
	return r.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		if err := applySoftDelete(ctx, tx, "admin", adminID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM admin_role WHERE admin_id = $1`, adminID); err != nil {
			return fmt.Errorf("解绑管理员角色失败: %w", err)
		}
		return nil
	})
}

// RolesByAdminIDs 批量查询管理员角色绑定（列表页避免 N+1）。
func (r *PGRepository) RolesByAdminIDs(ctx context.Context, adminIDs []int64) (map[int64][]domain.Role, error) {
	out := make(map[int64][]domain.Role, len(adminIDs))
	for _, id := range adminIDs {
		out[id] = []domain.Role{}
	}
	if len(adminIDs) == 0 {
		return out, nil
	}

	const q = `
		SELECT ar.admin_id, r.id, r.name, r.description, r.version, r.created_at, r.updated_at
		FROM admin_role ar
		JOIN role r ON r.id = ar.role_id AND r.deleted_at IS NULL
		WHERE ar.admin_id = ANY($1)
		ORDER BY r.id`

	rows, err := r.pool.Query(ctx, q, adminIDs)
	if err != nil {
		return nil, fmt.Errorf("批量查询管理员角色失败: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var adminID int64
		var role domain.Role
		if err := rows.Scan(&adminID, &role.ID, &role.Name, &role.Description, &role.Version, &role.CreatedAt, &role.UpdatedAt); err != nil {
			return nil, fmt.Errorf("扫描管理员角色失败: %w", err)
		}
		out[adminID] = append(out[adminID], role)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// BindRoles 全量替换管理员角色绑定（同一事务内先删后插）。
func (r *PGRepository) BindRoles(ctx context.Context, adminID int64, roleIDs []int64) error {
	return r.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM admin_role WHERE admin_id = $1`, adminID); err != nil {
			return fmt.Errorf("清空管理员角色失败: %w", err)
		}
		for _, roleID := range roleIDs {
			if _, err := tx.Exec(ctx,
				`INSERT INTO admin_role (admin_id, role_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
				adminID, roleID); err != nil {
				return fmt.Errorf("绑定管理员角色失败: %w", err)
			}
		}
		return nil
	})
}

// ===================== 角色 =====================

// ListRoles 角色列表（含权限矩阵）。
func (r *PGRepository) ListRoles(ctx context.Context) ([]domain.Role, error) {
	q := `SELECT ` + roleColumns + ` FROM role WHERE deleted_at IS NULL ORDER BY id`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("查询角色列表失败: %w", err)
	}
	defer rows.Close()

	roles := make([]domain.Role, 0)
	for rows.Next() {
		role, err := scanRole(rows)
		if err != nil {
			return nil, fmt.Errorf("扫描角色失败: %w", err)
		}
		roles = append(roles, *role)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(roles) == 0 {
		return roles, nil
	}

	perms, err := r.loadPermissionsByRoleIDs(ctx)
	if err != nil {
		return nil, err
	}
	for i := range roles {
		roles[i].Permissions = perms[roles[i].ID]
	}
	return roles, nil
}

// GetRole 角色详情（含权限矩阵）。
func (r *PGRepository) GetRole(ctx context.Context, roleID int64) (*domain.Role, error) {
	q := `SELECT ` + roleColumns + ` FROM role WHERE id = $1 AND deleted_at IS NULL`
	role, err := scanRole(r.pool.QueryRow(ctx, q, roleID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errcode.ErrNotFound
		}
		return nil, fmt.Errorf("查询角色失败: %w", err)
	}

	const pq = `
		SELECT id, role_id, module_code, permission, version, created_at, updated_at
		FROM role_permission
		WHERE role_id = $1 AND deleted_at IS NULL
		ORDER BY id`
	rows, err := r.pool.Query(ctx, pq, roleID)
	if err != nil {
		return nil, fmt.Errorf("查询角色权限失败: %w", err)
	}
	defer rows.Close()

	role.Permissions = make([]domain.RolePermission, 0)
	for rows.Next() {
		var p domain.RolePermission
		if err := rows.Scan(&p.ID, &p.RoleID, &p.ModuleCode, &p.Permission, &p.Version, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("扫描角色权限失败: %w", err)
		}
		role.Permissions = append(role.Permissions, p)
	}
	return role, rows.Err()
}

// CreateRole 新建角色并写入权限矩阵（单事务）。
func (r *PGRepository) CreateRole(ctx context.Context, role *domain.Role) error {
	return r.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		const q = `INSERT INTO role (name, description) VALUES ($1, $2)
		           RETURNING id, version, created_at, updated_at`
		err := tx.QueryRow(ctx, q, role.Name, role.Description).
			Scan(&role.ID, &role.Version, &role.CreatedAt, &role.UpdatedAt)
		if err != nil {
			if isUniqueViolation(err) {
				return errcode.ErrConflict
			}
			return fmt.Errorf("创建角色失败: %w", err)
		}
		return replaceRolePermissions(ctx, tx, role)
	})
}

// UpdateRole 修改角色并全量替换权限矩阵（单事务；version>0 启用乐观锁）。
func (r *PGRepository) UpdateRole(ctx context.Context, role *domain.Role, version int64) error {
	return r.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		sets := map[string]any{"name": role.Name, "description": role.Description}
		var err error
		if version > 0 {
			err = optimisticUpdate(ctx, tx, "role", role.ID, version, sets)
		} else {
			err = updateByID(ctx, tx, "role", role.ID, sets)
		}
		if err != nil {
			if isUniqueViolation(err) {
				return errcode.ErrConflict
			}
			return err
		}

		if _, err := tx.Exec(ctx,
			`UPDATE role_permission SET deleted_at = now() WHERE role_id = $1 AND deleted_at IS NULL`,
			role.ID); err != nil {
			return fmt.Errorf("清理角色权限失败: %w", err)
		}
		return replaceRolePermissions(ctx, tx, role)
	})
}

// SoftDeleteRole 逻辑删除角色及其权限矩阵（被引用校验在服务层完成）。
func (r *PGRepository) SoftDeleteRole(ctx context.Context, roleID int64) error {
	return r.WithTx(ctx, func(ctx context.Context, tx Tx) error {
		if err := applySoftDelete(ctx, tx, "role", roleID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`UPDATE role_permission SET deleted_at = now() WHERE role_id = $1 AND deleted_at IS NULL`,
			roleID); err != nil {
			return fmt.Errorf("删除角色权限失败: %w", err)
		}
		return nil
	})
}

// CountAdminsByRole 统计引用该角色的（未删除）管理员数。
func (r *PGRepository) CountAdminsByRole(ctx context.Context, roleID int64) (int64, error) {
	const q = `
		SELECT count(*)
		FROM admin_role ar
		JOIN admin a ON a.id = ar.admin_id AND a.deleted_at IS NULL
		WHERE ar.role_id = $1`
	var n int64
	if err := r.pool.QueryRow(ctx, q, roleID).Scan(&n); err != nil {
		return 0, fmt.Errorf("统计角色引用失败: %w", err)
	}
	return n, nil
}

// AdminIDsByRole 查询引用该角色的管理员 ID（权限缓存失效用）。
func (r *PGRepository) AdminIDsByRole(ctx context.Context, roleID int64) ([]int64, error) {
	const q = `
		SELECT ar.admin_id
		FROM admin_role ar
		JOIN admin a ON a.id = ar.admin_id AND a.deleted_at IS NULL
		WHERE ar.role_id = $1
		ORDER BY ar.admin_id`
	rows, err := r.pool.Query(ctx, q, roleID)
	if err != nil {
		return nil, fmt.Errorf("查询角色引用管理员失败: %w", err)
	}
	defer rows.Close()

	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("扫描角色引用管理员失败: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// CountRolesByIDs 统计存在（未删除）的角色数量，用于绑定角色时校验。
func (r *PGRepository) CountRolesByIDs(ctx context.Context, roleIDs []int64) (int64, error) {
	if len(roleIDs) == 0 {
		return 0, nil
	}
	const q = `SELECT count(*) FROM role WHERE deleted_at IS NULL AND id = ANY($1)`
	var n int64
	if err := r.pool.QueryRow(ctx, q, roleIDs).Scan(&n); err != nil {
		return 0, fmt.Errorf("校验角色存在性失败: %w", err)
	}
	return n, nil
}

// loadPermissionsByRoleIDs 一次查询全部角色权限并按键分组。
func (r *PGRepository) loadPermissionsByRoleIDs(ctx context.Context) (map[int64][]domain.RolePermission, error) {
	const q = `
		SELECT id, role_id, module_code, permission, version, created_at, updated_at
		FROM role_permission
		WHERE deleted_at IS NULL
		ORDER BY role_id, id`
	rows, err := r.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("查询角色权限失败: %w", err)
	}
	defer rows.Close()

	out := make(map[int64][]domain.RolePermission)
	for rows.Next() {
		var p domain.RolePermission
		if err := rows.Scan(&p.ID, &p.RoleID, &p.ModuleCode, &p.Permission, &p.Version, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("扫描角色权限失败: %w", err)
		}
		out[p.RoleID] = append(out[p.RoleID], p)
	}
	return out, rows.Err()
}

// replaceRolePermissions 写入角色权限矩阵（调用方需先清理旧授权）。
func replaceRolePermissions(ctx context.Context, tx Tx, role *domain.Role) error {
	const q = `INSERT INTO role_permission (role_id, module_code, permission) VALUES ($1, $2, $3)`
	for i := range role.Permissions {
		p := &role.Permissions[i]
		if _, err := tx.Exec(ctx, q, role.ID, p.ModuleCode, p.Permission); err != nil {
			if isUniqueViolation(err) {
				return errcode.ErrConflict
			}
			return fmt.Errorf("写入角色权限失败: %w", err)
		}
		p.RoleID = role.ID
	}
	return nil
}
