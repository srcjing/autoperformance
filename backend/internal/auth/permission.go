package auth

import "sort"

// 鉴权上下文键（中间件写入 gin.Context，handler 读取）。
const (
	CtxAdminID  = "auth.admin_id"
	CtxUsername = "auth.username"
	CtxPerms    = "auth.permissions"
)

// Permission 权限点：module_code + read/write。
type Permission struct {
	Module     string `json:"module"`
	Permission string `json:"permission"` // read / write
}

// SkeletonModuleCodes 子模块授权码清单（骨架，与 architecture.md §5.3 对齐）。
var SkeletonModuleCodes = []string{
	"env.list", "env.tag",
	"product.list", "product.interface",
	"testing.task", "testing.template",
	"report.list",
	"log.user", "log.system", "log.test",
	"setting.admin", "setting.cert", "setting.ssh",
}

// SkeletonPermissions 骨架：默认授予全部子模块 read+write。
// 正式实现：从 DB（admin → admin_role → role → role_permission）实时/缓存读取并集。
func SkeletonPermissions() map[string]bool {
	p := make(map[string]bool, len(SkeletonModuleCodes)*2)
	for _, m := range SkeletonModuleCodes {
		p[m+":read"] = true
		p[m+":write"] = true
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
	if perm == "read" && perms[module+":write"] {
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
