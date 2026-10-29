package setting

import (
	"context"
	"errors"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// TestCreateAdminIgnoresMassAssignment 新建管理员只接受白名单字段，默认无权限、启用、首登改密。
func TestCreateAdminIgnoresMassAssignment(t *testing.T) {
	svc, repo, _ := newTestService()

	view, err := svc.CreateAdmin(context.Background(), CreateAdminInput{
		Username: "operator", Name: "测试操作员", Phone: "13800000000",
		Email: "op@example.com", Description: "只读账号", Password: "Operator@123",
	})
	if err != nil {
		t.Fatalf("创建管理员失败: %v", err)
	}
	if repo.createAdminCalls != 1 || repo.createdAdmin == nil {
		t.Fatal("应调用仓储创建管理员")
	}
	created := repo.createdAdmin
	if created.IsBuiltin {
		t.Fatal("新建管理员不得为内置账号（防提权）")
	}
	if created.Status != domain.AdminStatusEnabled {
		t.Fatalf("新建管理员默认启用，got %s", created.Status)
	}
	if !created.MustChangePassword {
		t.Fatal("新建管理员首登应强制改密")
	}
	if n := len(repo.bindings[created.ID]); n != 0 {
		t.Fatalf("新建管理员默认无权限，实际绑定 %d 个角色", n)
	}
	if created.PasswordHash == "Operator@123" {
		t.Fatal("口令不得明文存储")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(created.PasswordHash), []byte("Operator@123")); err != nil {
		t.Fatalf("口令哈希不匹配: %v", err)
	}
	if view == nil || view.ID != created.ID || view.Username != "operator" {
		t.Fatalf("返回视图异常: %+v", view)
	}
}

// TestCreateAdminRejectsInvalidInput 必填项/弱口令/重复用户名拦截。
func TestCreateAdminRejectsInvalidInput(t *testing.T) {
	svc, repo, _ := newTestService()
	addAdmin(repo, 1, "dup", false)
	ctx := context.Background()

	cases := []struct {
		name string
		in   CreateAdminInput
		want error
	}{
		{"缺用户名", CreateAdminInput{Name: "n", Password: "Operator@123"}, errcode.ErrParamMiss},
		{"缺姓名", CreateAdminInput{Username: "u1", Password: "Operator@123"}, errcode.ErrParamMiss},
		{"缺口令", CreateAdminInput{Username: "u2", Name: "n"}, errcode.ErrParamMiss},
		{"弱口令", CreateAdminInput{Username: "u3", Name: "n", Password: "12345678"}, ErrPasswordWeak},
		{"重复用户名", CreateAdminInput{Username: "dup", Name: "n", Password: "Operator@123"}, ErrUsernameExists},
	}
	for _, c := range cases {
		if _, err := svc.CreateAdmin(ctx, c.in); !errors.Is(err, c.want) {
			t.Errorf("%s: want %v, got %v", c.name, c.want, err)
		}
	}
	if repo.createAdminCalls != 1 {
		t.Fatalf("仅重复用户名会进入仓储（用于唯一约束），实际调用 %d 次", repo.createAdminCalls)
	}
}

// TestBuiltinAdminProtected 内置 admin 禁止删除/停用（26002）。
func TestBuiltinAdminProtected(t *testing.T) {
	svc, repo, _ := newTestService()
	addAdmin(repo, 1, "admin", true)
	ctx := context.Background()

	if err := svc.DeleteAdmin(ctx, 1); !errors.Is(err, ErrBuiltinAdminProtected) {
		t.Fatalf("删除内置 admin: want 26002, got %v", err)
	}
	if len(repo.deletedAdmins) != 0 {
		t.Fatal("内置 admin 不得被删除")
	}
	if err := svc.UpdateAdminStatus(ctx, 1, domain.AdminStatusDisabled, 1); !errors.Is(err, ErrBuiltinAdminProtected) {
		t.Fatalf("停用内置 admin: want 26002, got %v", err)
	}
	if _, ok := repo.statusCalls[1]; ok {
		t.Fatal("内置 admin 状态不得被修改")
	}
}

// TestDeleteAdminInvalidatesCache 删除普通管理员并失效权限缓存。
func TestDeleteAdminInvalidatesCache(t *testing.T) {
	svc, repo, inv := newTestService()
	addAdmin(repo, 5, "alice", false)

	if err := svc.DeleteAdmin(context.Background(), 5); err != nil {
		t.Fatalf("删除管理员失败: %v", err)
	}
	if len(repo.deletedAdmins) != 1 || repo.deletedAdmins[0] != 5 {
		t.Fatalf("应软删除管理员 5: %v", repo.deletedAdmins)
	}
	if !inv.contains(5) {
		t.Fatal("删除后应失效权限缓存")
	}
}

// TestUpdateAdminStatus 校验状态枚举与缓存失效。
func TestUpdateAdminStatus(t *testing.T) {
	svc, repo, inv := newTestService()
	addAdmin(repo, 6, "bob", false)
	ctx := context.Background()

	if err := svc.UpdateAdminStatus(ctx, 6, "paused", 0); !errors.Is(err, errcode.ErrParamFmt) {
		t.Fatalf("非法状态应返回 10003，got %v", err)
	}
	if err := svc.UpdateAdminStatus(ctx, 6, domain.AdminStatusDisabled, 0); err != nil {
		t.Fatalf("停用失败: %v", err)
	}
	if repo.statusCalls[6] != domain.AdminStatusDisabled {
		t.Fatalf("状态未写入: %v", repo.statusCalls)
	}
	if !inv.contains(6) {
		t.Fatal("停用后应失效权限缓存")
	}
}

// TestResetPasswordForcesChange 重置口令：bcrypt 存储、强制下次登录改密、缓存失效。
func TestResetPasswordForcesChange(t *testing.T) {
	svc, repo, inv := newTestService()
	addAdmin(repo, 7, "carol", false)
	ctx := context.Background()

	if err := svc.ResetAdminPassword(ctx, 7, "12345678"); !errors.Is(err, ErrPasswordWeak) {
		t.Fatalf("弱口令应拒绝，got %v", err)
	}
	const newPwd = "Reset@12345"
	if err := svc.ResetAdminPassword(ctx, 7, newPwd); err != nil {
		t.Fatalf("重置口令失败: %v", err)
	}
	if repo.updatePwdCalls != 1 || !repo.updatePwdMust {
		t.Fatalf("重置后应强制改密: calls=%d must=%v", repo.updatePwdCalls, repo.updatePwdMust)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(repo.updatePwdHash), []byte(newPwd)); err != nil {
		t.Fatalf("重置口令哈希不匹配: %v", err)
	}
	if !inv.contains(7) {
		t.Fatal("重置口令后应失效权限缓存")
	}
}

// TestBindRolesInvalidatesCache 绑定角色为全量替换，且即时失效权限缓存。
func TestBindRolesInvalidatesCache(t *testing.T) {
	svc, repo, inv := newTestService()
	addAdmin(repo, 8, "dave", false)
	addRole(repo, 201, "只读观察员")
	addRole(repo, 202, "测试操作员")
	repo.bindings[8] = []int64{201}

	if err := svc.BindAdminRoles(context.Background(), 8, []int64{202, 202}); err != nil {
		t.Fatalf("绑定角色失败: %v", err)
	}
	if repo.bindCalls != 1 {
		t.Fatalf("应调用一次全量替换，got %d", repo.bindCalls)
	}
	if len(repo.boundRoles) != 1 || repo.boundRoles[0] != 202 {
		t.Fatalf("角色应去重并全量替换，got %v", repo.boundRoles)
	}
	if !inv.contains(8) {
		t.Fatal("角色变更后应失效权限缓存")
	}
}

// TestBindRolesRejectsUnknownRole 绑定不存在的角色 → 404，且不落库。
func TestBindRolesRejectsUnknownRole(t *testing.T) {
	svc, repo, _ := newTestService()
	addAdmin(repo, 9, "erin", false)
	addRole(repo, 201, "只读观察员")

	err := svc.BindAdminRoles(context.Background(), 9, []int64{201, 999})
	if !errors.Is(err, errcode.ErrNotFound) {
		t.Fatalf("want 10301, got %v", err)
	}
	if repo.bindCalls != 0 {
		t.Fatal("校验失败时不应落库")
	}
}

// TestListAdminsPagination 分页默认值与上限（page_size 上限 200）。
func TestListAdminsPagination(t *testing.T) {
	svc, repo, _ := newTestService()
	for i := int64(1); i <= 3; i++ {
		addAdmin(repo, i, "user"+string(rune('a'+i)), false)
	}

	page, err := svc.ListAdmins(context.Background(), ListAdminsQuery{Page: 0, PageSize: 0})
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if page.Page != 1 || page.PageSize != 20 || page.Total != 3 {
		t.Fatalf("分页默认值异常: %+v", page)
	}

	page, err = svc.ListAdmins(context.Background(), ListAdminsQuery{Page: 1, PageSize: 1000})
	if err != nil {
		t.Fatalf("列表失败: %v", err)
	}
	if page.PageSize != 200 {
		t.Fatalf("page_size 应截断为 200，got %d", page.PageSize)
	}
}

// TestGetAdminIncludesRoles 详情返回角色绑定（管理员管理页展示）。
func TestGetAdminIncludesRoles(t *testing.T) {
	svc, repo, _ := newTestService()
	addAdmin(repo, 13, "ivy", false)
	addRole(repo, 240, "只读观察员")
	repo.bindings[13] = []int64{240}

	view, err := svc.GetAdmin(context.Background(), 13)
	if err != nil {
		t.Fatalf("查询详情失败: %v", err)
	}
	if len(view.Roles) != 1 || view.Roles[0].ID != 240 {
		t.Fatalf("角色绑定异常: %+v", view.Roles)
	}
}
