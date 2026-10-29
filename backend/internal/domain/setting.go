// Package domain 领域模型（实体 + 枚举/状态机）。
//
// 本文件定义系统设置与认证相关实体（Admin/Role/RolePermission/Certificate/SshHost），
// 对齐 PRD §4.11/§4.11.1/§4.12/§4.13 与 architecture.md §5.1；
// 其余领域实体（环境/产品/测试/报告/日志）随对应 Phase 增量补齐。
package domain

import "time"

// 证书状态（PRD §3.6.4：由有效期推导，不人工维护）。
const (
	CertStatusValid    = "valid"
	CertStatusExpiring = "expiring"
	CertStatusExpired  = "expired"
)

// SSH/Host 记录类型与认证方式（PRD §4.13）。
const (
	SshHostTypeHost = "host"
	SshHostTypeSSH  = "ssh"

	SshAuthTypePassword = "password"
	SshAuthTypeKey      = "key"
)

// Admin 管理员账号（对齐 PRD §4.11、architecture.md §5.1）。
// 数据库 ID 为 bigint 自增；DTO 序列化为字符串（规避 JS 精度丢失）。
//
// 权限不落在 Admin 上：管理员最终权限 = 所授角色权限并集（INV-1），
// 内置 admin 由 IsBuiltin 标记隐式全权限（不依赖 role 表）。
type Admin struct {
	ID                 int64
	Username           string
	Name               string
	Phone              string
	Email              string
	Description        string
	PasswordHash       string
	MustChangePassword bool
	Status             string // enabled | disabled
	LastLoginAt        *time.Time
	IsBuiltin          bool
	Version            int64
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// IsEnabled 账号是否启用（停用账号不可登录/不可继续访问）。
func (a *Admin) IsEnabled() bool { return a != nil && a.Status == AdminStatusEnabled }

// IsBuiltinAdmin 是否为内置 admin（不可删除/停用，首登强制改密，隐式全权限）。
func (a *Admin) IsBuiltinAdmin() bool { return a != nil && a.IsBuiltin }

// Role 角色（权限范围集合，对齐 PRD §4.11.1）。
type Role struct {
	ID          int64
	Name        string
	Description string
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// Permissions 角色的子模块授权项（RolePermission 集合，查询时按需填充）。
	Permissions []RolePermission
}

// RolePermission 角色权限授权项（一行 = 某角色在某子模块的读写权限）。
type RolePermission struct {
	ID         int64
	RoleID     int64
	ModuleCode string
	Permission string // read | write
	Version    int64
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// AdminRole 管理员↔角色 多对多绑定（admin_role 联合主键）。
type AdminRole struct {
	AdminID int64
	RoleID  int64
}

// Certificate 证书（对齐 PRD §4.12 / api-contract §2.7）。
//
// 敏感字段加密落库（DES-I-002/DES-I-008）：CertEnc/KeyEnc 为 AES-256-GCM 信封密文
// （KEK 包裹 DEK），绝不是明文；对外响应只下发元数据与 HasKey 标记。
// Status 仅作库内冗余快照（写时按有效期计算），读取时一律按 ValidTo 重新推导，
// 避免有效期变更后状态陈旧。
type Certificate struct {
	ID                int64
	Name              string
	Issuer            string
	ValidFrom         *time.Time
	ValidTo           *time.Time
	CertEnc           []byte // 证书文件内容密文（信封加密）
	KeyEnc            []byte // 私钥密文（信封加密）
	IsBuiltin         bool
	Status            string // valid | expiring | expired（冗余快照）
	AssociatedService string
	Version           int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// IsBuiltinCert 是否为系统内置自签证书（不可删除，可重新初始化有效期）。
func (c *Certificate) IsBuiltinCert() bool { return c != nil && c.IsBuiltin }

// SshHost SSH 信任 / Host 解析记录（对齐 PRD §4.13 / api-contract §2.7）。
//
// KeyEnc 为 SSH 私钥的 AES-256-GCM 信封密文（DES-I-002），响应仅返回 HasKey 标记。
type SshHost struct {
	ID          int64
	Type        string // host | ssh
	Host        string
	Port        int
	Username    string
	AuthType    string // password | key
	KeyEnc      []byte // SSH 私钥密文（信封加密）
	HostEntries []string
	Description string
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// HasKey SSH 记录是否已保存私钥（仅返回布尔，不返回密钥本身）。
func (h *SshHost) HasKey() bool { return h != nil && len(h.KeyEnc) > 0 }
