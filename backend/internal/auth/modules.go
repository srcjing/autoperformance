// Package auth 认证与授权基础能力。
//
// modules.go 定义子模块授权项元数据（权限矩阵事实源），
// 对齐 specs/api-contract.md §5.1 与 PRD §8：
//   - 普通子模块可授予 read / write；
//   - 日志子模块默认只读（仅 read）；
//   - 系统设置敏感子模块仅 write（不授予 read）。
package auth

// 权限级别常量（与 role_permission.permission 取值一致）。
const (
	PermissionRead  = "read"
	PermissionWrite = "write"
)

// PermissionModule 子模块授权项元数据（GET /permissions 返回给前端权限矩阵）。
type PermissionModule struct {
	ModuleCode  string   `json:"module_code"`
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"` // 可授权项：read / write
	Sensitive   bool     `json:"sensitive"`
}

// readWrite 普通子模块可授权项。
var readWrite = []string{PermissionRead, PermissionWrite}

// readOnly 只读子模块可授权项（日志）。
var readOnly = []string{PermissionRead}

// writeOnly 敏感子模块可授权项（系统设置）。
var writeOnly = []string{PermissionWrite}

// permissionModules 子模块授权项清单（顺序与 api-contract §5.1 一致）。
var permissionModules = []PermissionModule{
	{"env.list", "环境管理 · 环境列表（含环境分类）", readWrite, false},
	{"env.tag", "环境管理 · 标签维护", readWrite, false},
	{"product.list", "产品管理 · 产品列表", readWrite, false},
	{"product.interface", "产品管理 · 接口管理（含阈值）", readWrite, false},
	{"testing.task", "测试管理 · 测试任务", readWrite, false},
	{"testing.template", "测试管理 · 预设模板", readWrite, false},
	{"report.list", "报告管理 · 报告列表（含导入导出）", readWrite, false},
	{"log.user", "日志管理 · 用户日志", readOnly, false},
	{"log.system", "日志管理 · 系统日志", readOnly, false},
	{"log.test", "日志管理 · 测试日志", readOnly, false},
	{"log.request", "日志管理 · 请求详情", readOnly, false},
	{"setting.log", "日志管理 · 日志设置（写入范围与保留策略）", readWrite, false},
	{"setting.admin", "系统设置 · 管理员管理（含角色）", writeOnly, true},
	{"setting.cert", "系统设置 · 证书管理", writeOnly, true},
	{"setting.ssh", "系统设置 · SSH 与 Host 管理", writeOnly, true},
}

// PermissionModules 返回子模块授权项元数据（副本，调用方可安全修改）。
func PermissionModules() []PermissionModule {
	out := make([]PermissionModule, len(permissionModules))
	for i, m := range permissionModules {
		perms := make([]string, len(m.Permissions))
		copy(perms, m.Permissions)
		out[i] = PermissionModule{ModuleCode: m.ModuleCode, Name: m.Name, Permissions: perms, Sensitive: m.Sensitive}
	}
	return out
}

// ModuleCodes 返回全部子模块授权码（顺序稳定）。
func ModuleCodes() []string {
	out := make([]string, len(permissionModules))
	for i, m := range permissionModules {
		out[i] = m.ModuleCode
	}
	return out
}

// IsKnownModule 判断子模块授权码是否在权限矩阵内。
func IsKnownModule(module string) bool {
	for _, m := range permissionModules {
		if m.ModuleCode == module {
			return true
		}
	}
	return false
}

// sensitiveModuleSet 返回敏感子模块集合（敏感标记来自权限矩阵元数据）。
func sensitiveModuleSet() map[string]bool {
	set := make(map[string]bool)
	for _, m := range permissionModules {
		if m.Sensitive {
			set[m.ModuleCode] = true
		}
	}
	return set
}

// ModuleAllowsPermission 判断子模块是否允许授予指定权限（read/write）。
func ModuleAllowsPermission(module, permission string) bool {
	for _, m := range permissionModules {
		if m.ModuleCode != module {
			continue
		}
		for _, p := range m.Permissions {
			if p == permission {
				return true
			}
		}
		return false
	}
	return false
}
