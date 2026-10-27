package auth

import "testing"

// TestPermissionModulesAlignWithSkeleton 校验权限矩阵元数据与 RBAC 骨架一致（1.1 验证）。
func TestPermissionModulesAlignWithSkeleton(t *testing.T) {
	mods := PermissionModules()
	if len(mods) == 0 {
		t.Fatal("权限矩阵不应为空")
	}
	if len(mods) != len(SkeletonModuleCodes) {
		t.Fatalf("权限矩阵 %d 项，骨架授权码 %d 项，不一致", len(mods), len(SkeletonModuleCodes))
	}

	seen := make(map[string]bool, len(mods))
	for i, m := range mods {
		if m.ModuleCode != SkeletonModuleCodes[i] {
			t.Fatalf("第 %d 项 module_code = %q, want %q", i, m.ModuleCode, SkeletonModuleCodes[i])
		}
		if seen[m.ModuleCode] {
			t.Fatalf("module_code 重复: %s", m.ModuleCode)
		}
		seen[m.ModuleCode] = true

		if m.Sensitive != IsSensitiveModule(m.ModuleCode) {
			t.Errorf("%s sensitive 标记不一致: matrix=%v rbac=%v", m.ModuleCode, m.Sensitive, IsSensitiveModule(m.ModuleCode))
		}
		readonly := len(m.ModuleCode) > 4 && m.ModuleCode[:4] == "log."
		if !m.Sensitive && !readonly && len(m.Permissions) != 2 {
			t.Errorf("%s 非敏感模块应可授 read/write，got %v", m.ModuleCode, m.Permissions)
		}
		if readonly && (len(m.Permissions) != 1 || m.Permissions[0] != PermissionRead) {
			t.Errorf("%s 日志子模块应仅可授 read，got %v", m.ModuleCode, m.Permissions)
		}
		if m.Sensitive && (len(m.Permissions) != 1 || m.Permissions[0] != PermissionWrite) {
			t.Errorf("%s 敏感模块应仅可授 write，got %v", m.ModuleCode, m.Permissions)
		}
	}
}

// TestModuleAllowsPermission 校验授权项白名单（敏感/只读子模块拒绝越权授权）。
func TestModuleAllowsPermission(t *testing.T) {
	cases := []struct {
		module, perm string
		want         bool
	}{
		{"env.list", PermissionRead, true},
		{"env.list", PermissionWrite, true},
		{"log.user", PermissionRead, true},
		{"log.user", PermissionWrite, false},
		{"setting.admin", PermissionWrite, true},
		{"setting.admin", PermissionRead, false},
		{"unknown.module", PermissionRead, false},
		{"env.list", "admin", false},
	}
	for _, c := range cases {
		if got := ModuleAllowsPermission(c.module, c.perm); got != c.want {
			t.Errorf("ModuleAllowsPermission(%q,%q) = %v, want %v", c.module, c.perm, got, c.want)
		}
	}
}

// TestPermissionModulesReturnsCopy 校验返回副本，调用方修改不污染全局矩阵。
func TestPermissionModulesReturnsCopy(t *testing.T) {
	mods := PermissionModules()
	mods[0].ModuleCode = "hacked"
	mods[0].Permissions[0] = "write"
	if got := PermissionModules()[0]; got.ModuleCode != "env.list" || got.Permissions[0] != PermissionRead {
		t.Fatalf("权限矩阵被外部修改污染: %+v", got)
	}
}
