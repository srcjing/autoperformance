package auth

import "testing"

func TestUnionPermissionsMergesRolesAndWriteImpliesRead(t *testing.T) {
	grants := []PermissionGrant{
		{Module: "env.list", Permission: "read"},
		{Module: "env.list", Permission: "write"}, // 多角色并集，write 覆盖 read
		{Module: "setting.cert", Permission: "write"},
	}
	got := UnionPermissions(grants)
	if !HasPerm(got, "env.list", "read") {
		t.Fatal("expected env.list:read")
	}
	if !HasPerm(got, "env.list", "write") {
		t.Fatal("expected env.list:write")
	}
	if !HasPerm(got, "setting.cert", "write") {
		t.Fatal("expected setting.cert:write")
	}
	if HasPerm(got, "setting.cert", "read") {
		t.Fatal("write-only 敏感子模块不应要求 read 才能访问")
	}
	if HasPerm(got, "testing.task", "read") {
		t.Fatal("unexpected permission")
	}
}

func TestUnionPermissionsEmpty(t *testing.T) {
	if got := UnionPermissions(nil); len(got) != 0 {
		t.Fatalf("空授权应返回空集合，got %v", got)
	}
}

func TestUnionPermissionsSkipsInvalid(t *testing.T) {
	grants := []PermissionGrant{
		{Module: "", Permission: "read"},
		{Module: "env.list", Permission: "admin"}, // 非法 permission 被忽略
		{Module: "env.tag", Permission: "read"},
	}
	got := UnionPermissions(grants)
	if HasPerm(got, "env.list", "read") {
		t.Fatal("非法授权不应生效")
	}
	if !HasPerm(got, "env.tag", "read") {
		t.Fatal("合法授权应生效")
	}
}
