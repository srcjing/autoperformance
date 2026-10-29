// Package setting 系统设置业务服务（管理员/角色/证书/SSH-Host）。
//
// Phase 1 落地管理员与角色（含权限矩阵）：对齐 PRD §4.11/§4.11.1/§8、
// api-contract §2.7 与 INV-1/INV-10；Phase 7 补齐证书与 SSH/Host（certificate.go / sshhost.go）。
package setting

import (
	"context"
	"errors"
	"strings"
	"time"

	"auto_performance/internal/auth"
	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
	authsvc "auto_performance/internal/service/auth"
)

// PermissionGrant 子模块授权项（复用认证模块 RBAC 模型，避免两套权限口径）。
type PermissionGrant = auth.PermissionGrant

// ErrPasswordWeak 口令复杂度不合规（复用认证模块 20004，策略单一事实源）。
var ErrPasswordWeak = authsvc.ErrPasswordWeak

// Repo 系统设置仓储能力（管理员 + 角色；由 repository.PGRepository 实现）。
type Repo interface {
	// ===== 管理员 =====
	ListAdmins(ctx context.Context, keyword string, page, pageSize int) ([]domain.Admin, int64, error)
	GetAdminByID(ctx context.Context, adminID int64) (*domain.Admin, error)
	CreateAdmin(ctx context.Context, admin *domain.Admin) error
	UpdateAdmin(ctx context.Context, adminID int64, name, phone, email, description string, version int64) error
	UpdateAdminStatus(ctx context.Context, adminID int64, status string, version int64) error
	UpdatePassword(ctx context.Context, adminID int64, passwordHash string, mustChange bool) error
	SoftDeleteAdmin(ctx context.Context, adminID int64) error
	LoadRolesByAdmin(ctx context.Context, adminID int64) ([]domain.Role, error)
	RolesByAdminIDs(ctx context.Context, adminIDs []int64) (map[int64][]domain.Role, error)
	BindRoles(ctx context.Context, adminID int64, roleIDs []int64) error

	// ===== 角色 =====
	ListRoles(ctx context.Context) ([]domain.Role, error)
	GetRole(ctx context.Context, roleID int64) (*domain.Role, error)
	CreateRole(ctx context.Context, role *domain.Role) error
	UpdateRole(ctx context.Context, role *domain.Role, version int64) error
	SoftDeleteRole(ctx context.Context, roleID int64) error
	CountAdminsByRole(ctx context.Context, roleID int64) (int64, error)
	AdminIDsByRole(ctx context.Context, roleID int64) ([]int64, error)
	CountRolesByIDs(ctx context.Context, roleIDs []int64) (int64, error)
}

// PermCacheInvalidator 权限缓存失效能力（由 service/auth.Service 实现）。
type PermCacheInvalidator interface {
	InvalidatePermCache(adminID int64)
}

// Service 系统设置业务服务（管理员/角色/证书/SSH-Host）。
type Service struct {
	repo  Repo
	perms PermCacheInvalidator
	now   func() time.Time

	// certs/hosts 为证书与 SSH/Host 仓储（WithSecrets 注入）；kek 为信封加密根密钥。
	// 未注入时对应能力返回 10602（依赖异常），不影响管理员/角色功能。
	certs CertRepo
	hosts SshHostRepo
	kek   []byte
}

// New 构造系统设置服务。perms 为 nil 时跳过权限缓存失效（仅测试/降级场景）。
func New(repo Repo, perms PermCacheInvalidator) *Service {
	return &Service{repo: repo, perms: perms, now: time.Now}
}

// WithSecrets 注入证书/SSH-Host 仓储与信封加密根密钥（DES-I-002/DES-I-008）。
//
// 采用链式注入保持既有 admin/role 构造签名不变（new(repo, perms) 仍可用）；
// kek 长度非 32 字节时拒绝明文落库（加密能力失败关闭）。
func (s *Service) WithSecrets(certs CertRepo, hosts SshHostRepo, kek []byte) *Service {
	if s != nil {
		s.certs = certs
		s.hosts = hosts
		s.kek = kek
	}
	return s
}

// invalidate 失效指定管理员的权限缓存（perms 未注入时静默跳过）。
func (s *Service) invalidate(adminIDs ...int64) {
	if s == nil || s.perms == nil {
		return
	}
	for _, id := range adminIDs {
		s.perms.InvalidatePermCache(id)
	}
}

// ===================== 管理员 =====================

// ListAdminsQuery 管理员列表查询（分页 + 关键字）。
type ListAdminsQuery struct {
	Keyword  string
	Page     int
	PageSize int
}

// AdminPage 管理员分页结果（对齐 api-contract §1.3）。
type AdminPage struct {
	List     []AdminView
	Total    int64
	Page     int
	PageSize int
}

// AdminView 管理员视图（不含 password_hash）。
type AdminView struct {
	ID                 int64
	Username           string
	Name               string
	Phone              string
	Email              string
	Description        string
	Status             string
	IsBuiltin          bool
	MustChangePassword bool
	LastLoginAt        *time.Time
	Version            int64
	CreatedAt          time.Time
	UpdatedAt          time.Time
	Roles              []domain.Role
}

// CreateAdminInput 新建管理员输入（白名单字段，DES-E-002 防 Mass Assignment）。
//
// 故意不包含 status / role / role_ids / is_builtin / password_hash 等提权字段：
// 新建账号默认启用、默认无权限、首登强制改密，权限只能经「角色授权」接口授予。
type CreateAdminInput struct {
	Username    string
	Name        string
	Phone       string
	Email       string
	Description string
	Password    string
}

// UpdateAdminInput 修改管理员输入（白名单字段；用户名不可改）。
type UpdateAdminInput struct {
	Name        string
	Phone       string
	Email       string
	Description string
	Version     int64
}

// DefaultPageSize 列表默认页大小；MaxPageSize 分页上限（api-contract §1.3）。
const (
	DefaultPageSize = 20
	MaxPageSize     = 200
)

// normalizePaging 归一化分页参数。
func normalizePaging(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}
	return page, pageSize
}

// ListAdmins 管理员列表（分页 + 用户名/姓名关键字）。
func (s *Service) ListAdmins(ctx context.Context, q ListAdminsQuery) (*AdminPage, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	page, pageSize := normalizePaging(q.Page, q.PageSize)
	keyword := strings.TrimSpace(q.Keyword)

	admins, total, err := s.repo.ListAdmins(ctx, keyword, page, pageSize)
	if err != nil {
		return nil, err
	}

	ids := make([]int64, 0, len(admins))
	for i := range admins {
		ids = append(ids, admins[i].ID)
	}
	roleMap, err := s.repo.RolesByAdminIDs(ctx, ids)
	if err != nil {
		return nil, err
	}

	list := make([]AdminView, 0, len(admins))
	for i := range admins {
		list = append(list, toAdminView(&admins[i], roleMap[admins[i].ID]))
	}
	return &AdminPage{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// GetAdmin 管理员详情（含角色绑定）。
func (s *Service) GetAdmin(ctx context.Context, adminID int64) (*AdminView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if adminID <= 0 {
		return nil, errcode.ErrParamFmt
	}
	admin, err := s.repo.GetAdminByID(ctx, adminID)
	if err != nil {
		return nil, err
	}
	roles, err := s.repo.LoadRolesByAdmin(ctx, adminID)
	if err != nil {
		return nil, err
	}
	view := toAdminView(admin, roles)
	return &view, nil
}

// CreateAdmin 新建管理员（bcrypt 存储、默认无权限、首登强制改密）。
func (s *Service) CreateAdmin(ctx context.Context, in CreateAdminInput) (*AdminView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}

	username := strings.TrimSpace(in.Username)
	name := strings.TrimSpace(in.Name)
	if username == "" || name == "" || in.Password == "" {
		return nil, errcode.ErrParamMiss
	}
	if len(username) > 64 || len(name) > 64 {
		return nil, errcode.ErrParamFmt
	}
	if err := authsvc.ValidatePassword(in.Password); err != nil {
		return nil, err
	}

	hash, err := authsvc.HashPassword(in.Password)
	if err != nil {
		return nil, err
	}

	// 仅绑定白名单字段；status/is_builtin/must_change_password 由服务端固定取值。
	admin := &domain.Admin{
		Username:           username,
		Name:               name,
		Phone:              strings.TrimSpace(in.Phone),
		Email:              strings.TrimSpace(in.Email),
		Description:        strings.TrimSpace(in.Description),
		PasswordHash:       hash,
		MustChangePassword: true,
		Status:             domain.AdminStatusEnabled,
		IsBuiltin:          false,
	}
	if err := s.repo.CreateAdmin(ctx, admin); err != nil {
		if errors.Is(err, errcode.ErrConflict) {
			return nil, ErrUsernameExists
		}
		return nil, err
	}

	view := toAdminView(admin, nil)
	return &view, nil
}

// UpdateAdmin 修改管理员基础信息（白名单字段；用户名/状态/内置标记不可改）。
func (s *Service) UpdateAdmin(ctx context.Context, adminID int64, in UpdateAdminInput) (*AdminView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if adminID <= 0 {
		return nil, errcode.ErrParamFmt
	}

	current, err := s.repo.GetAdminByID(ctx, adminID)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		name = current.Name // 未传姓名时保持原值，避免误清空必填项
	}
	if len(name) > 64 {
		return nil, errcode.ErrParamFmt
	}

	if err := s.repo.UpdateAdmin(ctx, adminID, name,
		strings.TrimSpace(in.Phone), strings.TrimSpace(in.Email),
		strings.TrimSpace(in.Description), in.Version); err != nil {
		return nil, err
	}
	return s.GetAdmin(ctx, adminID)
}

// DeleteAdmin 删除管理员（逻辑删除）；内置 admin 返回 26002（INV-10）。
func (s *Service) DeleteAdmin(ctx context.Context, adminID int64) error {
	if s == nil || s.repo == nil {
		return errcode.ErrDependency
	}
	admin, err := s.repo.GetAdminByID(ctx, adminID)
	if err != nil {
		return err
	}
	if admin.IsBuiltinAdmin() {
		return ErrBuiltinAdminProtected
	}
	if err := s.repo.SoftDeleteAdmin(ctx, adminID); err != nil {
		return err
	}
	s.invalidate(adminID)
	return nil
}

// UpdateAdminStatus 停用/启用管理员；内置 admin 返回 26002。
//
// 停用即时生效：AuthN 每请求实时校验账号状态（无需等待 token 过期）。
func (s *Service) UpdateAdminStatus(ctx context.Context, adminID int64, status string, version int64) error {
	if s == nil || s.repo == nil {
		return errcode.ErrDependency
	}
	if status != domain.AdminStatusEnabled && status != domain.AdminStatusDisabled {
		return errcode.ErrParamFmt
	}
	admin, err := s.repo.GetAdminByID(ctx, adminID)
	if err != nil {
		return err
	}
	if admin.IsBuiltinAdmin() {
		return ErrBuiltinAdminProtected
	}
	if err := s.repo.UpdateAdminStatus(ctx, adminID, status, version); err != nil {
		return err
	}
	s.invalidate(adminID)
	return nil
}

// ResetAdminPassword 重置管理员口令（bcrypt 存储；下次登录强制改密）。
func (s *Service) ResetAdminPassword(ctx context.Context, adminID int64, newPassword string) error {
	if s == nil || s.repo == nil {
		return errcode.ErrDependency
	}
	if _, err := s.repo.GetAdminByID(ctx, adminID); err != nil {
		return err
	}
	if err := authsvc.ValidatePassword(newPassword); err != nil {
		return err
	}
	hash, err := authsvc.HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.repo.UpdatePassword(ctx, adminID, hash, true); err != nil {
		return err
	}
	s.invalidate(adminID)
	return nil
}

// AdminRoles 查询管理员已绑定角色。
func (s *Service) AdminRoles(ctx context.Context, adminID int64) ([]domain.Role, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if _, err := s.repo.GetAdminByID(ctx, adminID); err != nil {
		return nil, err
	}
	roles, err := s.repo.LoadRolesByAdmin(ctx, adminID)
	if err != nil {
		return nil, err
	}
	if roles == nil {
		roles = []domain.Role{}
	}
	return roles, nil
}

// BindAdminRoles 全量替换管理员角色绑定，并即时失效权限缓存（INV-1 权限即时生效）。
func (s *Service) BindAdminRoles(ctx context.Context, adminID int64, roleIDs []int64) error {
	if s == nil || s.repo == nil {
		return errcode.ErrDependency
	}
	if _, err := s.repo.GetAdminByID(ctx, adminID); err != nil {
		return err
	}

	ids := dedupeIDs(roleIDs)
	if len(ids) > 0 {
		count, err := s.repo.CountRolesByIDs(ctx, ids)
		if err != nil {
			return err
		}
		if count != int64(len(ids)) {
			return errcode.ErrNotFound
		}
	}

	if err := s.repo.BindRoles(ctx, adminID, ids); err != nil {
		return err
	}
	s.invalidate(adminID)
	return nil
}

// toAdminView 领域实体 → 视图（不暴露口令哈希）。
func toAdminView(a *domain.Admin, roles []domain.Role) AdminView {
	if roles == nil {
		roles = []domain.Role{}
	}
	return AdminView{
		ID:                 a.ID,
		Username:           a.Username,
		Name:               a.Name,
		Phone:              a.Phone,
		Email:              a.Email,
		Description:        a.Description,
		Status:             a.Status,
		IsBuiltin:          a.IsBuiltin,
		MustChangePassword: a.MustChangePassword,
		LastLoginAt:        a.LastLoginAt,
		Version:            a.Version,
		CreatedAt:          a.CreatedAt,
		UpdatedAt:          a.UpdatedAt,
		Roles:              roles,
	}
}

// dedupeIDs 去重并过滤非正 ID（保持原顺序）。
func dedupeIDs(ids []int64) []int64 {
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
