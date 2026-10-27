package auth

import "sort"

// 鉴权上下文键（中间件写入 gin.Context，handler/审计中间件读取）。
const (
	CtxAdminID  = "auth.admin_id"             // string（bigint 十进制，DTO 口径）
	CtxUsername = "auth.username"             // string
	CtxPerms    = "auth.permissions"          // map[string]bool（权限点并集）
	CtxTokenID  = "auth.token_id"             // string（jti，登出时加入失效名单）
	CtxTokenExp = "auth.token_exp"            // time.Time（用于计算 jti 黑名单 TTL）
	CtxMustPwd  = "auth.must_change_password" // bool（首登强制改密标记）
)

// Permission 权限点：module_code + read/write。
type Permission struct {
	Module     string `json:"module"`
	Permission string `json:"permission"` // read / write
}

// SkeletonModuleCodes 子模块授权码清单（由 modules.go 权限矩阵派生，避免两处漂移）。
var SkeletonModuleCodes = ModuleCodes()

// SensitiveModules 敏感子模块（仅 write，write 不蕴含 read，见 architecture.md §5.3）。
var SensitiveModules = sensitiveModuleSet()

// IsSensitiveModule 判断是否为敏感子模块。
func IsSensitiveModule(module string) bool { return SensitiveModules[module] }

// SkeletonPermissions 骨架：默认授予全部子模块 write，非敏感模块附带 read。
// 正式实现：从 DB（admin → admin_role → role → role_permission）实时/缓存读取并集。
func SkeletonPermissions() map[string]bool {
	p := make(map[string]bool, len(SkeletonModuleCodes)*2)
	for _, m := range SkeletonModuleCodes {
		p[m+":write"] = true
		if !IsSensitiveModule(m) {
			p[m+":read"] = true
		}
	}
	return p
}

// HasPerm 判断权限集合是否包含 module:perm（write 蕴含 read）。
func HasPerm(perms map[string]bool, module, perm string) bool {
	if perms == nil {
		return false
	}
	if perms[module+":"+perm] {
		return true
	}
	if perm == "read" && perms[module+":write"] && !IsSensitiveModule(module) {
		return true
	}
	return false
}

// PermissionList 将权限集合转为权限点字符串列表（排序，稳定输出）。
func PermissionList(perms map[string]bool) []string {
	out := make([]string, 0, len(perms))
	for k, v := range perms {
		if v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
