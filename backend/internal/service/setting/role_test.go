package setting

import (
	"context"
	"errors"
	"testing"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// TestDeleteReferencedRole 被管理员引用的角色禁止删除（26003）。
func TestDeleteReferencedRole(t *testing.T) {
	svc, repo, _ := newTestService()
	addAdmin(repo, 10, "frank", false)
	addRole(repo, 210, "只读观察员")
	repo.bindings[10] = []int64{210}

	if err := svc.DeleteRole(context.Background(), 210); !errors.Is(err, ErrRoleReferenced) {
		t.Fatalf("want 26003, got %v", err)
	}
	if len(repo.deletedRoles) != 0 {
		t.Fatal("被引用角色不得删除")
	}

	// 解除引用后可删除。
	delete(repo.bindings, 10)
	if err := svc.DeleteRole(context.Background(), 210); err != nil {
		t.Fatalf("解除引用后删除失败: %v", err)
	}
	if len(repo.deletedRoles) != 1 || repo.deletedRoles[0] != 210 {
		t.Fatalf("应删除角色 210: %v", repo.deletedRoles)
	}
}

// TestRolePermissionMatrixValidation 角色权限矩阵白名单校验（敏感/只读子模块越权授权被拒）。
func TestRolePermissionMatrixValidation(t *testing.T) {
	svc, repo, _ := newTestService()
	ctx := context.Background()

	invalid := []struct {
		name  string
		perms []PermissionGrant
	}{
		{"未知子模块", []PermissionGrant{{Module: "unknown.mod", Permission: "read"}}},
		{"敏感子模块只读", []PermissionGrant{{Module: "setting.admin", Permission: "read"}}},
		{"日志子模块可写", []PermissionGrant{{Module: "log.user", Permission: "write"}}},
		{"非法权限值", []PermissionGrant{{Module: "env.list", Permission: "admin"}}},
	}
	for _, c := range invalid {
		if _, err := svc.CreateRole(ctx, RoleInput{Name: "bad-" + c.name, Permissions: c.perms}); err == nil {
			t.Errorf("%s 应被拒绝", c.name)
		}
	}
	if len(repo.roles) != 0 {
		t.Fatal("非法角色不应落库")
	}

	r, err := svc.CreateRole(ctx, RoleInput{
		Name: "只读观察员",
		Permissions: []PermissionGrant{
			{Module: "env.list", Permission: "read"},
			{Module: "report.list", Permission: "read"},
			{Module: "env.list", Permission: "read"}, // 重复项去重
		},
	})
	if err != nil {
		t.Fatalf("创建角色失败: %v", err)
	}
	if r == nil || r.Name != "只读观察员" {
		t.Fatalf("角色视图异常: %+v", r)
	}
	if len(repo.lastRolePerms) != 2 {
		t.Fatalf("授权项应去重为 2 条，got %d", len(repo.lastRolePerms))
	}
}

// TestUpdateRoleInvalidatesBoundAdmins 角色权限变更后，绑定该角色的管理员缓存即时失效。
func TestUpdateRoleInvalidatesBoundAdmins(t *testing.T) {
	svc, repo, inv := newTestService()
	addAdmin(repo, 11, "grace", false)
	addAdmin(repo, 12, "henry", false)
	role := addRole(repo, 220, "测试操作员")
	role.Permissions = []domain.RolePermission{{RoleID: 220, ModuleCode: "env.list", Permission: "read"}}
	repo.bindings[11] = []int64{220}
	repo.bindings[12] = []int64{220}

	if _, err := svc.UpdateRole(context.Background(), 220, RoleInput{
		Name:        "测试操作员",
		Permissions: []PermissionGrant{{Module: "testing.task", Permission: "write"}},
	}); err != nil {
		t.Fatalf("更新角色失败: %v", err)
	}
	if repo.updateRoleCalls != 1 {
		t.Fatalf("应更新角色一次，got %d", repo.updateRoleCalls)
	}
	if !inv.contains(11) || !inv.contains(12) {
		t.Fatalf("绑定管理员的权限缓存应全部失效，got %v", inv.ids)
	}
}

// TestCreateRoleDuplicateName 角色名唯一（10302）。
func TestCreateRoleDuplicateName(t *testing.T) {
	svc, repo, _ := newTestService()
	addRole(repo, 230, "只读观察员")

	_, err := svc.CreateRole(context.Background(), RoleInput{Name: "只读观察员"})
	if !errors.Is(err, errcode.ErrConflict) {
		t.Fatalf("want 10302, got %v", err)
	}
}

// TestPermissionModulesExposed 权限矩阵元数据暴露给前端（含敏感标记）。
func TestPermissionModulesExposed(t *testing.T) {
	svc, _, _ := newTestService()
	mods := svc.PermissionModules()
	if len(mods) != 15 {
		t.Fatalf("权限矩阵应为 15 项，got %d", len(mods))
	}
	var sensitive int
	for _, m := range mods {
		if m.Sensitive {
			sensitive++
			if len(m.Permissions) != 1 || m.Permissions[0] != "write" {
				t.Errorf("%s 敏感模块应仅可授 write", m.ModuleCode)
			}
		}
	}
	if sensitive != 3 {
		t.Fatalf("敏感子模块应为 3 项，got %d", sensitive)
	}
}
