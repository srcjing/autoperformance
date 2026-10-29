package setting

import (
	"context"
	"strings"
	"time"

	"auto_performance/internal/auth"
	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// RoleInput 角色新建/修改输入（含权限矩阵；白名单校验子模块与可授权限）。
type RoleInput struct {
	Name        string
	Description string
	Permissions []PermissionGrant
	Version     int64
}

// RoleView 角色视图（含权限矩阵，供前端勾选展示）。
type RoleView struct {
	ID          int64
	Name        string
	Description string
	Permissions []PermissionGrant
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ListRoles 角色列表（含权限矩阵）。
func (s *Service) ListRoles(ctx context.Context) ([]RoleView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	roles, err := s.repo.ListRoles(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]RoleView, 0, len(roles))
	for i := range roles {
		out = append(out, toRoleView(&roles[i]))
	}
	return out, nil
}

// GetRole 角色详情（含权限矩阵）。
func (s *Service) GetRole(ctx context.Context, roleID int64) (*RoleView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if roleID <= 0 {
		return nil, errcode.ErrParamFmt
	}
	role, err := s.repo.GetRole(ctx, roleID)
	if err != nil {
		return nil, err
	}
	view := toRoleView(role)
	return &view, nil
}

// CreateRole 新建角色（含权限矩阵，按白名单校验授权项）。
func (s *Service) CreateRole(ctx context.Context, in RoleInput) (*RoleView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	name, description, perms, err := validateRoleInput(in)
	if err != nil {
		return nil, err
	}

	role := &domain.Role{Name: name, Description: description, Permissions: perms}
	if err := s.repo.CreateRole(ctx, role); err != nil {
		return nil, err
	}
	view := toRoleView(role)
	return &view, nil
}

// UpdateRole 修改角色（名称/描述/权限矩阵全量替换；权限变更即时失效绑定管理员缓存）。
func (s *Service) UpdateRole(ctx context.Context, roleID int64, in RoleInput) (*RoleView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if roleID <= 0 {
		return nil, errcode.ErrParamFmt
	}
	if _, err := s.repo.GetRole(ctx, roleID); err != nil {
		return nil, err
	}

	name, description, perms, err := validateRoleInput(in)
	if err != nil {
		return nil, err
	}

	role := &domain.Role{ID: roleID, Name: name, Description: description, Permissions: perms}
	if err := s.repo.UpdateRole(ctx, role, in.Version); err != nil {
		return nil, err
	}

	// 权限矩阵变化：绑定该角色的管理员权限缓存必须即时失效（INV-1）。
	if adminIDs, err := s.repo.AdminIDsByRole(ctx, roleID); err == nil {
		s.invalidate(adminIDs...)
	}

	// 重新读取，返回更新后的 version/updated_at（供后续乐观锁使用）。
	return s.GetRole(ctx, roleID)
}

// DeleteRole 删除角色；被管理员引用时禁止删除（26003，INV-6）。
func (s *Service) DeleteRole(ctx context.Context, roleID int64) error {
	if s == nil || s.repo == nil {
		return errcode.ErrDependency
	}
	if _, err := s.repo.GetRole(ctx, roleID); err != nil {
		return err
	}
	refs, err := s.repo.CountAdminsByRole(ctx, roleID)
	if err != nil {
		return err
	}
	if refs > 0 {
		return ErrRoleReferenced
	}
	return s.repo.SoftDeleteRole(ctx, roleID)
}

// PermissionModules 返回子模块授权项元数据（GET /permissions，含敏感标记）。
func (s *Service) PermissionModules() []auth.PermissionModule {
	return auth.PermissionModules()
}

// validateRoleInput 校验并归一化角色输入：名称必填、长度受限、授权项白名单 + 去重。
func validateRoleInput(in RoleInput) (string, string, []domain.RolePermission, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return "", "", nil, errcode.ErrParamMiss
	}
	if len(name) > 64 {
		return "", "", nil, errcode.ErrParamFmt
	}
	perms, err := normalizePermissions(in.Permissions)
	if err != nil {
		return "", "", nil, err
	}
	return name, strings.TrimSpace(in.Description), perms, nil
}

// normalizePermissions 校验授权项：子模块必须已知，权限必须在该子模块可授权范围内。
// 非法授权（如 setting.admin:read / log.user:write / 未知子模块）一律拒绝，避免越权组合入库。
func normalizePermissions(grants []PermissionGrant) ([]domain.RolePermission, error) {
	seen := make(map[string]bool, len(grants))
	out := make([]domain.RolePermission, 0, len(grants))
	for _, g := range grants {
		module := strings.TrimSpace(g.Module)
		permission := strings.TrimSpace(g.Permission)
		if module == "" || permission == "" {
			return nil, errcode.ErrParamFmt
		}
		if !auth.IsKnownModule(module) || !auth.ModuleAllowsPermission(module, permission) {
			return nil, errcode.ErrParamFmt
		}
		key := module + ":" + permission
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, domain.RolePermission{ModuleCode: module, Permission: permission})
	}
	return out, nil
}

// toRoleView 领域实体 → 视图。
func toRoleView(r *domain.Role) RoleView {
	perms := make([]PermissionGrant, 0, len(r.Permissions))
	for _, p := range r.Permissions {
		perms = append(perms, PermissionGrant{Module: p.ModuleCode, Permission: p.Permission})
	}
	return RoleView{
		ID:          r.ID,
		Name:        r.Name,
		Description: r.Description,
		Permissions: perms,
		Version:     r.Version,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}
