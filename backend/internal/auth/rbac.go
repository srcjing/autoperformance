// Package auth 认证与授权基础能力。
//
// rbac.go 提供分级分权 RBAC 的权限并集计算（对齐 architecture.md §5.3 / INV-1）：
// 管理员最终权限 = 所授角色权限并集；write 蕴含 read。
package auth

// PermissionGrant 一条角色授权项（module_code + read/write）。
type PermissionGrant struct {
	Module     string
	Permission string // read | write
}

// UnionPermissions 计算授权项并集，返回权限点集合（键格式 module:perm）。
// 规则：同一 module 多角色并集时 write 覆盖 read（write 蕴含 read，见 HasPerm）。
func UnionPermissions(grants []PermissionGrant) map[string]bool {
	perms := make(map[string]bool, len(grants)*2)
	for _, g := range grants {
		if g.Module == "" || (g.Permission != "read" && g.Permission != "write") {
			continue
		}
		perms[g.Module+":"+g.Permission] = true
		if g.Permission == "write" && !IsSensitiveModule(g.Module) {
			// 非敏感模块 write 蕴含 read：显式补 read，便于直接集合包含判断。
			perms[g.Module+":read"] = true
		}
	}
	return perms
}
