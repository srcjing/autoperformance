// Package setting 系统设置 HTTP handler（管理员/角色/权限矩阵）。
//
// 对齐 api-contract §2.7 与 security-design-report DES-E-002：
//   - 请求体采用显式白名单 DTO，夹带的 role/status/is_super 等提权字段被丢弃；
//   - 敏感子模块路由（setting.admin）在 API 层由 AuthZ 强制 write 鉴权。
package setting

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/auth"
	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
	settingsvc "auto_performance/internal/service/setting"
)

// Service 系统设置业务服务能力（*settingsvc.Service 实现；便于 handler 层测试注入桩）。
type Service interface {
	// 管理员
	ListAdmins(ctx context.Context, q settingsvc.ListAdminsQuery) (*settingsvc.AdminPage, error)
	GetAdmin(ctx context.Context, adminID int64) (*settingsvc.AdminView, error)
	CreateAdmin(ctx context.Context, in settingsvc.CreateAdminInput) (*settingsvc.AdminView, error)
	UpdateAdmin(ctx context.Context, adminID int64, in settingsvc.UpdateAdminInput) (*settingsvc.AdminView, error)
	DeleteAdmin(ctx context.Context, adminID int64) error
	UpdateAdminStatus(ctx context.Context, adminID int64, status string, version int64) error
	ResetAdminPassword(ctx context.Context, adminID int64, newPassword string) error
	AdminRoles(ctx context.Context, adminID int64) ([]domain.Role, error)
	BindAdminRoles(ctx context.Context, adminID int64, roleIDs []int64) error

	// 角色
	ListRoles(ctx context.Context) ([]settingsvc.RoleView, error)
	GetRole(ctx context.Context, roleID int64) (*settingsvc.RoleView, error)
	CreateRole(ctx context.Context, in settingsvc.RoleInput) (*settingsvc.RoleView, error)
	UpdateRole(ctx context.Context, roleID int64, in settingsvc.RoleInput) (*settingsvc.RoleView, error)
	DeleteRole(ctx context.Context, roleID int64) error
	PermissionModules() []auth.PermissionModule

	// 证书
	ListCertificates(ctx context.Context, page, pageSize int) (*settingsvc.CertificatePage, error)
	CreateCertificate(ctx context.Context, in settingsvc.CertificateInput) (*settingsvc.CertificateView, error)
	DeleteCertificate(ctx context.Context, certID int64) error
	ReinitCertificate(ctx context.Context, certID int64) (*settingsvc.CertificateView, error)

	// SSH/Host
	ListSshHosts(ctx context.Context, q settingsvc.ListSshHostsQuery) (*settingsvc.SshHostPage, error)
	CreateSshHost(ctx context.Context, in settingsvc.SshHostInput) (*settingsvc.SshHostView, error)
	UpdateSshHost(ctx context.Context, sshHostID int64, in settingsvc.SshHostInput) (*settingsvc.SshHostView, error)
	DeleteSshHost(ctx context.Context, sshHostID int64) error
	ImportSshHosts(ctx context.Context, filename string, size int64, content io.Reader) (*settingsvc.SshHostImportResult, error)
}

// Handler 系统设置 HTTP handler。
type Handler struct {
	svc Service
}

// New 构造系统设置 handler。
func New(svc Service) *Handler { return &Handler{svc: svc} }

// 编译期断言：业务服务满足 handler 依赖（避免签名漂移）。
var _ Service = (*settingsvc.Service)(nil)

// ===================== 公共 DTO / 解析 =====================

// adminDTO 管理员响应（不含 password_hash；id 序列化为字符串）。
type adminDTO struct {
	ID                 string         `json:"id"`
	Username           string         `json:"username"`
	Name               string         `json:"name"`
	Phone              string         `json:"phone"`
	Email              string         `json:"email"`
	Description        string         `json:"description"`
	Status             string         `json:"status"`
	IsBuiltin          bool           `json:"is_builtin"`
	MustChangePassword bool           `json:"must_change_password"`
	LastLoginAt        *string        `json:"last_login_at"`
	Version            int64          `json:"version"`
	Roles              []roleBriefDTO `json:"roles"`
}

// roleBriefDTO 角色摘要（管理员列表/详情内嵌）。
type roleBriefDTO struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// roleDTO 角色响应（含权限矩阵）。
type roleDTO struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Permissions []permissionDTO `json:"permissions"`
	Version     int64           `json:"version"`
	CreatedAt   string          `json:"created_at"`
	UpdatedAt   string          `json:"updated_at"`
}

// permissionDTO 子模块授权项。
type permissionDTO struct {
	Module     string `json:"module"`
	Permission string `json:"permission"`
}

// idList 兼容 ["1","2"] 与 [1,2] 两种 ID 数组写法（api-contract §1.7）。
type idList []string

// UnmarshalJSON 逐项接受字符串或数字 ID。
func (l *idList) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("id 列表格式非法: %w", err)
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		var s string
		if err := json.Unmarshal(item, &s); err == nil {
			out = append(out, s)
			continue
		}
		var n json.Number
		if err := json.Unmarshal(item, &n); err == nil {
			out = append(out, n.String())
			continue
		}
		return fmt.Errorf("id 取值非法: %s", string(item))
	}
	*l = out
	return nil
}

// parseIntQuery 解析整型 query 参数；缺失或非法返回 (0,false)（由服务层取默认值）。
func parseIntQuery(c *gin.Context, key string) (int, bool) {
	raw := c.Query(key)
	if raw == "" {
		return 0, false
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return v, true
}

// itoa int64 → 十进制字符串（DTO 的 ID 统一为字符串）。
func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// parseID 解析路径参数 :id（数字或字符串形式的 bigint）。
func parseID(c *gin.Context) (int64, error) {
	raw := c.Param("id")
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, errcode.ErrParamFmt
	}
	return id, nil
}

// parseIDs 将字符串 ID 列表转换为 int64。
func parseIDs(raw []string) ([]int64, error) {
	out := make([]int64, 0, len(raw))
	for _, s := range raw {
		id, err := strconv.ParseInt(s, 10, 64)
		if err != nil || id <= 0 {
			return nil, errcode.ErrParamFmt
		}
		out = append(out, id)
	}
	return out, nil
}

func toAdminDTO(v *settingsvc.AdminView) adminDTO {
	roles := make([]roleBriefDTO, 0, len(v.Roles))
	for _, r := range v.Roles {
		roles = append(roles, roleBriefDTO{ID: strconv.FormatInt(r.ID, 10), Name: r.Name, Description: r.Description})
	}
	dto := adminDTO{
		ID:                 strconv.FormatInt(v.ID, 10),
		Username:           v.Username,
		Name:               v.Name,
		Phone:              v.Phone,
		Email:              v.Email,
		Description:        v.Description,
		Status:             v.Status,
		IsBuiltin:          v.IsBuiltin,
		MustChangePassword: v.MustChangePassword,
		Version:            v.Version,
		Roles:              roles,
	}
	if v.LastLoginAt != nil {
		s := v.LastLoginAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		dto.LastLoginAt = &s
	}
	return dto
}

func toRoleDTO(v *settingsvc.RoleView) roleDTO {
	perms := make([]permissionDTO, 0, len(v.Permissions))
	for _, p := range v.Permissions {
		perms = append(perms, permissionDTO{Module: p.Module, Permission: p.Permission})
	}
	return roleDTO{
		ID:          strconv.FormatInt(v.ID, 10),
		Name:        v.Name,
		Description: v.Description,
		Permissions: perms,
		Version:     v.Version,
		CreatedAt:   v.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		UpdatedAt:   v.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}
}

func toPermissionGrants(in []permissionDTO) []settingsvc.PermissionGrant {
	out := make([]settingsvc.PermissionGrant, 0, len(in))
	for _, p := range in {
		out = append(out, settingsvc.PermissionGrant{Module: p.Module, Permission: p.Permission})
	}
	return out
}
