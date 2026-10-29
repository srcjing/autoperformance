package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// fakeAdminRepo 内存认证仓储：记录调用次数以验证缓存/副作用行为。
type fakeAdminRepo struct {
	admins    map[string]*domain.Admin // username → admin
	perms     map[int64]map[string]bool
	adminErr  error
	permsErr  error
	loginErr  error
	pwdErr    error
	adminCall int
	permsCall int
	lastID    int64

	lastLoginID    int64
	lastLoginAt    time.Time
	lastLoginCalls int

	updatedHash       string
	updatedMustChange bool
	updatePwdCalls    int
	updatePwdID       int64
}

func (f *fakeAdminRepo) GetAdminByID(_ context.Context, adminID int64) (*domain.Admin, error) {
	f.adminCall++
	f.lastID = adminID
	if f.adminErr != nil {
		return nil, f.adminErr
	}
	for _, a := range f.admins {
		if a.ID == adminID {
			cp := *a
			return &cp, nil
		}
	}
	return nil, errcode.ErrNotFound
}

func (f *fakeAdminRepo) GetAdminByUsername(_ context.Context, username string) (*domain.Admin, error) {
	if f.adminErr != nil {
		return nil, f.adminErr
	}
	a, ok := f.admins[username]
	if !ok {
		return nil, errcode.ErrNotFound
	}
	cp := *a
	return &cp, nil
}

func (f *fakeAdminRepo) ListPermissionsByAdmin(_ context.Context, adminID int64) (map[string]bool, error) {
	f.permsCall++
	if f.permsErr != nil {
		return nil, f.permsErr
	}
	return f.perms[adminID], nil
}

func (f *fakeAdminRepo) UpdateLastLogin(_ context.Context, adminID int64, at time.Time) error {
	f.lastLoginCalls++
	f.lastLoginID = adminID
	f.lastLoginAt = at
	return f.loginErr
}

func (f *fakeAdminRepo) UpdatePassword(_ context.Context, adminID int64, hash string, mustChange bool) error {
	f.updatePwdCalls++
	f.updatePwdID = adminID
	f.updatedHash = hash
	f.updatedMustChange = mustChange
	return f.pwdErr
}

// mustHash 生成 bcrypt 哈希（测试固定口令）。
func mustHash(t *testing.T, password string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("生成 bcrypt 哈希失败: %v", err)
	}
	return string(h)
}

const (
	testPassword = "Secret@123"
	testIP       = "10.0.0.9"
)

// newFakeRepo 构造含指定状态 admin 的仓储（口令 testPassword，权限 env.list:read）。
func newFakeRepo(t *testing.T, status string) *fakeAdminRepo {
	t.Helper()
	return &fakeAdminRepo{
		admins: map[string]*domain.Admin{
			"alice": {
				ID: 42, Username: "alice", Name: "Alice", Status: status,
				PasswordHash: mustHash(t, testPassword),
			},
		},
		perms: map[int64]map[string]bool{42: {"env.list:read": true}},
	}
}

// TestLoginUnifiedErrorMessage 账号不存在与口令错误必须返回同一错误（防账号枚举）。
func TestLoginUnifiedErrorMessage(t *testing.T) {
	repo := newFakeRepo(t, domain.AdminStatusEnabled)
	svc := New(repo, time.Minute)
	ctx := context.Background()

	_, wrongPwdErr := svc.Login(ctx, LoginInput{Username: "alice", Password: "wrong-pass", IP: testIP})
	if !errors.Is(wrongPwdErr, ErrBadCredentials) {
		t.Fatalf("口令错误应返回 20001，got %v", wrongPwdErr)
	}
	repo2 := newFakeRepo(t, domain.AdminStatusEnabled)
	svc2 := New(repo2, time.Minute)
	_, noUserErr := svc2.Login(ctx, LoginInput{Username: "nobody", Password: "whatever", IP: testIP})
	if !errors.Is(noUserErr, ErrBadCredentials) {
		t.Fatalf("账号不存在应返回 20001，got %v", noUserErr)
	}
	if wrongPwdErr.Error() != noUserErr.Error() {
		t.Fatalf("错误提示必须一致: %q vs %q", wrongPwdErr.Error(), noUserErr.Error())
	}
}

// TestLoginSuccess 校验登录成功返回管理员、权限并集并记录最后登录时间。
func TestLoginSuccess(t *testing.T) {
	repo := newFakeRepo(t, domain.AdminStatusEnabled)
	svc := New(repo, time.Minute)
	svc.now = func() time.Time { return time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC) }

	res, err := svc.Login(context.Background(), LoginInput{Username: " alice ", Password: testPassword, IP: testIP})
	if err != nil {
		t.Fatalf("登录失败: %v", err)
	}
	if res.Admin.ID != 42 || res.Admin.Username != "alice" {
		t.Fatalf("返回管理员异常: %+v", res.Admin)
	}
	if !res.Permissions["env.list:read"] {
		t.Fatalf("权限并集缺失: %v", res.Permissions)
	}
	if repo.lastLoginCalls != 1 || repo.lastLoginID != 42 {
		t.Fatalf("应记录最后登录时间: calls=%d id=%d", repo.lastLoginCalls, repo.lastLoginID)
	}
	if res.Admin.PasswordHash == "" {
		t.Fatal("管理员快照应保留 password_hash 供服务内部使用")
	}
}

// TestLoginLockoutAfterN 连续失败达阈值后账号+IP 双维度锁定（10502），正确口令同样被拒。
func TestLoginLockoutAfterN(t *testing.T) {
	repo := newFakeRepo(t, domain.AdminStatusEnabled)
	svc := New(repo, time.Minute)
	ctx := context.Background()

	for i := 0; i < DefaultMaxLoginFailures; i++ {
		if _, err := svc.Login(ctx, LoginInput{Username: "alice", Password: "bad", IP: testIP}); !errors.Is(err, ErrBadCredentials) {
			t.Fatalf("第 %d 次失败应返回 20001，got %v", i+1, err)
		}
	}

	// 账号被锁定：即使口令正确也拒绝（10502）。
	if _, err := svc.Login(ctx, LoginInput{Username: "alice", Password: testPassword, IP: testIP}); !errors.Is(err, errcode.ErrLoginLock) {
		t.Fatalf("锁定后应返回 10502，got %v", err)
	}

	// IP 维度：同一 IP 的其他账号也被锁定。
	repo.admins["bob"] = &domain.Admin{ID: 43, Username: "bob", Status: domain.AdminStatusEnabled, PasswordHash: mustHash(t, testPassword)}
	if _, err := svc.Login(ctx, LoginInput{Username: "bob", Password: testPassword, IP: testIP}); !errors.Is(err, errcode.ErrLoginLock) {
		t.Fatalf("同 IP 其他账号应被 IP 锁定，got %v", err)
	}

	// 其他 IP 不受影响（账号维度仍按用户名隔离）。
	if _, err := svc.Login(ctx, LoginInput{Username: "bob", Password: testPassword, IP: "10.0.0.10"}); err != nil {
		t.Fatalf("其他 IP 登录不应被锁定: %v", err)
	}
}

// TestLoginLockoutExpires 锁定期满后可再次登录。
func TestLoginLockoutExpires(t *testing.T) {
	repo := newFakeRepo(t, domain.AdminStatusEnabled)
	svc := New(repo, time.Minute)
	base := time.Now()
	svc.now = func() time.Time { return base }
	ctx := context.Background()

	for i := 0; i < DefaultMaxLoginFailures; i++ {
		_, _ = svc.Login(ctx, LoginInput{Username: "alice", Password: "bad", IP: testIP})
	}
	if _, err := svc.Login(ctx, LoginInput{Username: "alice", Password: testPassword, IP: testIP}); !errors.Is(err, errcode.ErrLoginLock) {
		t.Fatalf("锁定期内应拒绝，got %v", err)
	}

	svc.now = func() time.Time { return base.Add(DefaultLoginLockDuration + time.Second) }
	if _, err := svc.Login(ctx, LoginInput{Username: "alice", Password: testPassword, IP: testIP}); err != nil {
		t.Fatalf("锁定期满后应允许登录: %v", err)
	}
}

// TestLoginFailuresResetOnSuccess 登录成功清零失败计数（非累计锁定）。
func TestLoginFailuresResetOnSuccess(t *testing.T) {
	repo := newFakeRepo(t, domain.AdminStatusEnabled)
	svc := New(repo, time.Minute)
	ctx := context.Background()

	for i := 0; i < DefaultMaxLoginFailures-1; i++ {
		_, _ = svc.Login(ctx, LoginInput{Username: "alice", Password: "bad", IP: testIP})
	}
	if _, err := svc.Login(ctx, LoginInput{Username: "alice", Password: testPassword, IP: testIP}); err != nil {
		t.Fatalf("未达阈值应允许登录: %v", err)
	}
	for i := 0; i < DefaultMaxLoginFailures-1; i++ {
		if _, err := svc.Login(ctx, LoginInput{Username: "alice", Password: "bad", IP: testIP}); !errors.Is(err, ErrBadCredentials) {
			t.Fatalf("成功后计数应清零，第 %d 次失败返回 %v", i+1, err)
		}
	}
}

// TestLoginDisabledAdmin 停用账号返回 10202。
func TestLoginDisabledAdmin(t *testing.T) {
	repo := newFakeRepo(t, domain.AdminStatusDisabled)
	svc := New(repo, time.Minute)

	_, err := svc.Login(context.Background(), LoginInput{Username: "alice", Password: testPassword, IP: testIP})
	if !errors.Is(err, errcode.ErrDisabled) {
		t.Fatalf("停用账号应返回 10202，got %v", err)
	}
}

// TestMustChangePassword 内置 admin 首登返回 must_change_password=true 与隐式全权限。
func TestMustChangePassword(t *testing.T) {
	repo := newFakeRepo(t, domain.AdminStatusEnabled)
	repo.admins["admin"] = &domain.Admin{
		ID: 1, Username: "admin", Name: "内置管理员", Status: domain.AdminStatusEnabled,
		IsBuiltin: true, MustChangePassword: true, PasswordHash: mustHash(t, testPassword),
	}
	repo.perms[1] = map[string]bool{"env.list:write": true, "env.list:read": true}
	svc := New(repo, time.Minute)

	res, err := svc.Login(context.Background(), LoginInput{Username: "admin", Password: testPassword, IP: testIP})
	if err != nil {
		t.Fatalf("内置 admin 登录失败: %v", err)
	}
	if !res.Admin.MustChangePassword || !res.Admin.IsBuiltin {
		t.Fatalf("内置 admin 首登应强制改密: %+v", res.Admin)
	}
}

// TestChangePasswordOldWrong 旧密码错误返回 20003，且不写库。
func TestChangePasswordOldWrong(t *testing.T) {
	repo := newFakeRepo(t, domain.AdminStatusEnabled)
	svc := New(repo, time.Minute)

	err := svc.ChangePassword(context.Background(), 42, "wrong-old", "NewSecret@456")
	if !errors.Is(err, ErrOldPasswordWrong) {
		t.Fatalf("want 20003, got %v", err)
	}
	if repo.updatePwdCalls != 0 {
		t.Fatal("旧密码错误时不应更新密码")
	}
}

// TestChangePasswordComplexity 新密码复杂度校验（≥8 位含大小写/数字/特殊字符）→ 20004。
func TestChangePasswordComplexity(t *testing.T) {
	weak := []string{
		"", "Ab@1", "abcdefg@1", "ABCDEFG@1", "Abcdefgh1", "Abcdefg@",
		strings.Repeat("Aa1@", 20), // 超过 bcrypt 72 字节上限
	}
	for _, pw := range weak {
		repo := newFakeRepo(t, domain.AdminStatusEnabled)
		svc := New(repo, time.Minute)
		err := svc.ChangePassword(context.Background(), 42, testPassword, pw)
		if !errors.Is(err, ErrPasswordWeak) {
			t.Errorf("密码 %q 应返回 20004，got %v", pw, err)
		}
		if repo.updatePwdCalls != 0 {
			t.Errorf("密码 %q 不合规时不应写库", pw)
		}
	}
}

// TestChangePasswordSame 新旧密码相同返回 20005。
func TestChangePasswordSame(t *testing.T) {
	repo := newFakeRepo(t, domain.AdminStatusEnabled)
	svc := New(repo, time.Minute)

	err := svc.ChangePassword(context.Background(), 42, testPassword, testPassword)
	if !errors.Is(err, ErrPasswordSame) {
		t.Fatalf("want 20005, got %v", err)
	}
	if repo.updatePwdCalls != 0 {
		t.Fatal("同口令不应写库")
	}
}

// TestChangePasswordSuccess 改密成功：bcrypt 存储、清除首登标记、缓存失效。
func TestChangePasswordSuccess(t *testing.T) {
	repo := newFakeRepo(t, domain.AdminStatusEnabled)
	repo.admins["alice"].MustChangePassword = true
	svc := New(repo, time.Minute)
	ctx := context.Background()

	// 预热权限缓存，改密后应失效。
	if _, err := svc.Permissions(ctx, 42); err != nil {
		t.Fatalf("预热缓存失败: %v", err)
	}

	const newPwd = "NewSecret@456"
	if err := svc.ChangePassword(ctx, 42, testPassword, newPwd); err != nil {
		t.Fatalf("改密失败: %v", err)
	}
	if repo.updatePwdCalls != 1 || repo.updatePwdID != 42 {
		t.Fatalf("应更新密码: calls=%d id=%d", repo.updatePwdCalls, repo.updatePwdID)
	}
	if repo.updatedMustChange {
		t.Fatal("改密成功后 must_change_password 应为 false")
	}
	if repo.updatedHash == newPwd {
		t.Fatal("口令不得明文存储")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(repo.updatedHash), []byte(newPwd)); err != nil {
		t.Fatalf("存储的哈希无法匹配新口令: %v", err)
	}

	// 缓存失效：再次读取权限应重新查库。
	before := repo.permsCall
	if _, err := svc.Permissions(ctx, 42); err != nil {
		t.Fatalf("读取权限失败: %v", err)
	}
	if repo.permsCall == before {
		t.Fatal("改密后应失效权限缓存")
	}
}

// TestHashPasswordUsesBcryptCostAtLeast10 口令哈希必须 bcrypt cost≥10（DES-S-006）。
func TestHashPasswordUsesBcryptCostAtLeast10(t *testing.T) {
	hash, err := HashPassword(testPassword)
	if err != nil {
		t.Fatalf("哈希失败: %v", err)
	}
	cost, err := bcrypt.Cost([]byte(hash))
	if err != nil {
		t.Fatalf("解析 bcrypt cost 失败: %v", err)
	}
	if cost < 10 {
		t.Fatalf("bcrypt cost = %d, want >= 10", cost)
	}
	if strings.Contains(hash, testPassword) {
		t.Fatal("哈希不应包含明文口令")
	}
}

// --- Phase 0 既有能力回归（主体快照/缓存/黑名单） ---

// TestPrincipalRejectsDisabledAdmin 验证停用账号返回 10202（停用无法登录/继续访问）。
func TestPrincipalRejectsDisabledAdmin(t *testing.T) {
	svc := New(newFakeRepo(t, domain.AdminStatusDisabled), time.Minute)

	_, err := svc.Principal(context.Background(), 42)
	if !errors.Is(err, errcode.ErrDisabled) {
		t.Fatalf("want ErrDisabled, got %v", err)
	}
}

// TestPrincipalLoadsPermissionsUnion 验证主体快照包含权限并集与改密标记。
func TestPrincipalLoadsPermissionsUnion(t *testing.T) {
	repo := newFakeRepo(t, domain.AdminStatusEnabled)
	repo.admins["alice"].MustChangePassword = true
	svc := New(repo, time.Minute)

	p, err := svc.Principal(context.Background(), 42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.AdminID != 42 || p.Username != "alice" {
		t.Fatalf("主体信息异常: %+v", p)
	}
	if !p.MustChangePassword {
		t.Fatal("应继承 must_change_password")
	}
	if !p.Permissions["env.list:read"] {
		t.Fatalf("权限并集缺失: %v", p.Permissions)
	}
	if repo.lastID != 42 {
		t.Fatalf("应以 adminID 查询仓储, got %d", repo.lastID)
	}
}

// TestPermCacheTTLAndInvalidate 验证权限缓存命中、TTL 过期与主动失效。
func TestPermCacheTTLAndInvalidate(t *testing.T) {
	repo := newFakeRepo(t, domain.AdminStatusEnabled)
	svc := New(repo, 30*time.Second)

	base := time.Now()
	svc.now = func() time.Time { return base }

	if _, err := svc.Permissions(context.Background(), 42); err != nil {
		t.Fatalf("首次加载失败: %v", err)
	}
	if _, err := svc.Permissions(context.Background(), 42); err != nil {
		t.Fatalf("缓存读取失败: %v", err)
	}
	if repo.permsCall != 1 {
		t.Fatalf("TTL 内应命中缓存，实际查询 DB %d 次", repo.permsCall)
	}

	svc.InvalidatePermCache(42)
	if _, err := svc.Permissions(context.Background(), 42); err != nil {
		t.Fatalf("失效后加载失败: %v", err)
	}
	if repo.permsCall != 2 {
		t.Fatalf("InvalidatePermCache 后应重新查询 DB，实际 %d 次", repo.permsCall)
	}

	svc.now = func() time.Time { return base.Add(31 * time.Second) }
	if _, err := svc.Permissions(context.Background(), 42); err != nil {
		t.Fatalf("TTL 过期后加载失败: %v", err)
	}
	if repo.permsCall != 3 {
		t.Fatalf("TTL 过期后应重新查询 DB，实际 %d 次", repo.permsCall)
	}
}

// TestPermCacheNotCachedOnError 验证仓储错误不写缓存。
func TestPermCacheNotCachedOnError(t *testing.T) {
	repo := newFakeRepo(t, domain.AdminStatusEnabled)
	repo.permsErr = errcode.ErrDependency
	svc := New(repo, time.Minute)

	if _, err := svc.Permissions(context.Background(), 42); err == nil {
		t.Fatal("期望仓储错误")
	}
	repo.permsErr = nil
	if _, err := svc.Permissions(context.Background(), 42); err != nil {
		t.Fatalf("错误不应被缓存，重试应成功: %v", err)
	}
	if repo.permsCall != 2 {
		t.Fatalf("permsCall = %d, want 2", repo.permsCall)
	}
}

// TestTokenBlacklist 验证登出失效名单及其 TTL 过期回收。
func TestTokenBlacklist(t *testing.T) {
	svc := New(newFakeRepo(t, domain.AdminStatusEnabled), time.Minute)
	base := time.Now()
	svc.now = func() time.Time { return base }

	if svc.IsRevoked("jti-1") {
		t.Fatal("未登出的 token 不应失效")
	}
	svc.RevokeToken("jti-1", 10*time.Minute)
	if !svc.IsRevoked("jti-1") {
		t.Fatal("登出后 token 应失效")
	}
	if svc.IsRevoked("") {
		t.Fatal("空 jti 不应判定失效")
	}

	svc.now = func() time.Time { return base.Add(11 * time.Minute) }
	if svc.IsRevoked("jti-1") {
		t.Fatal("黑名单 TTL 过期后应自动回收")
	}
}

// TestPermissionsRequiresRepo 验证未注入仓储时返回依赖异常而非 panic。
func TestPermissionsRequiresRepo(t *testing.T) {
	svc := New(nil, time.Minute)
	if _, err := svc.Permissions(context.Background(), 1); !errors.Is(err, errcode.ErrDependency) {
		t.Fatalf("want ErrDependency, got %v", err)
	}
	if _, err := svc.Principal(context.Background(), 1); !errors.Is(err, errcode.ErrDependency) {
		t.Fatalf("want ErrDependency, got %v", err)
	}
	var nilSvc *Service
	nilSvc.InvalidatePermCache(1) // 不应 panic
	if nilSvc.IsRevoked("x") {
		t.Fatal("nil service 不应判定失效")
	}
}
