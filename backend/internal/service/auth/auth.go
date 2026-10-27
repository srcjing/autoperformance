// Package auth 认证与授权业务服务（登录/登出/改密/权限加载）。
//
// 本文件实现认证主流程与鉴权快照（INV-1 / INV-10 / DES-S-001/003/006）：
//   - 登录：bcrypt 比对、失败统一提示（20001）、账号+IP 双维度锁定（10502）；
//   - 改密：旧口令校验（20003）、复杂度（20004）、新旧不同（20005）；
//   - 权限并集：admin → admin_role → role → role_permission，经仓储层计算；
//   - 短 TTL 内存缓存：避免每请求打库，角色变更时由 InvalidatePermCache 即时失效；
//   - 登出失效：内存 jti 黑名单（带 TTL），重放旧 token 返回 10102。
package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// Repo 认证所需的仓储能力（由 repository.PGRepository 实现）。
type Repo interface {
	// GetAdminByID 查询管理员（含 status / must_change_password）。
	GetAdminByID(ctx context.Context, adminID int64) (*domain.Admin, error)
	// GetAdminByUsername 按登录名查询管理员（登录用）。
	GetAdminByUsername(ctx context.Context, username string) (*domain.Admin, error)
	// ListPermissionsByAdmin 计算管理员最终权限并集（INV-1）。
	ListPermissionsByAdmin(ctx context.Context, adminID int64) (map[string]bool, error)
	// UpdateLastLogin 记录最后登录时间（审计字段）。
	UpdateLastLogin(ctx context.Context, adminID int64, at time.Time) error
	// UpdatePassword 更新口令哈希与首登改密标记（mustChange=false 表示已完成改密）。
	UpdatePassword(ctx context.Context, adminID int64, passwordHash string, mustChange bool) error
}

// Principal 当前请求的鉴权快照（由 AuthN 中间件注入上下文）。
type Principal struct {
	AdminID            int64
	Username           string
	Status             string
	MustChangePassword bool
	Permissions        map[string]bool
}

// LoginInput 登录输入（口令仅用于服务端 bcrypt 比对，不落日志）。
type LoginInput struct {
	Username string
	Password string
	IP       string
}

// LoginResult 登录结果（token 由接入层签发，服务层不感知 JWT 细节）。
type LoginResult struct {
	Admin       *domain.Admin
	Permissions map[string]bool
}

// DefaultPermCacheTTL 权限缓存默认 TTL（角色变更即时失效，TTL 仅兜底最终一致）。
const DefaultPermCacheTTL = 30 * time.Second

type permCacheEntry struct {
	perms map[string]bool
	exp   time.Time
}

// Service 认证业务服务。
type Service struct {
	repo    Repo
	permTTL time.Duration
	now     func() time.Time

	mu        sync.Mutex // 保护 permCache / blacklist / 登录失败计数
	permCache map[int64]permCacheEntry
	blacklist map[string]time.Time // jti → 失效时间
	guard     loginGuard
}

// New 构造认证服务。permTTL<=0 时取 DefaultPermCacheTTL。
func New(repo Repo, permTTL time.Duration) *Service {
	if permTTL <= 0 {
		permTTL = DefaultPermCacheTTL
	}
	return &Service{
		repo:      repo,
		permTTL:   permTTL,
		now:       time.Now,
		permCache: make(map[int64]permCacheEntry),
		blacklist: make(map[string]time.Time),
		guard:     newLoginGuard(DefaultMaxLoginFailures, DefaultLoginLockDuration),
	}
}

// SetLoginPolicy 覆盖登录锁定策略（便于按部署环境调整与测试）。
// maxFailures<=0 或 lockFor<=0 时保持默认。
func (s *Service) SetLoginPolicy(maxFailures int, lockFor time.Duration) {
	if s == nil {
		return
	}
	if maxFailures <= 0 {
		maxFailures = DefaultMaxLoginFailures
	}
	if lockFor <= 0 {
		lockFor = DefaultLoginLockDuration
	}
	s.mu.Lock()
	s.guard.maxFailures = maxFailures
	s.guard.lockFor = lockFor
	s.mu.Unlock()
}

// Login 校验口令并返回登录主体与权限并集。
//
// 错误语义（不泄露账号是否存在）：
//   - 账号不存在 / 口令错误 → ErrBadCredentials（20001，统一提示）；
//   - 连续失败达阈值（账号+IP 双维度）→ errcode.ErrLoginLock（10502）；
//   - 账号停用 → errcode.ErrDisabled（10202）。
func (s *Service) Login(ctx context.Context, in LoginInput) (*LoginResult, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}

	username := strings.TrimSpace(in.Username)
	keys := loginGuardKeys(username, in.IP)

	if s.isLoginLocked(keys) {
		return nil, errcode.ErrLoginLock
	}

	// 空用户名/口令同样走统一失败路径，避免与「账号不存在」提示不一致。
	if username == "" || in.Password == "" {
		s.recordLoginFailure(keys)
		return nil, ErrBadCredentials
	}

	admin, err := s.repo.GetAdminByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, errcode.ErrNotFound) {
			s.recordLoginFailure(keys)
			return nil, ErrBadCredentials
		}
		return nil, err
	}

	// 先校验口令，再判断停用：避免向未持有正确凭据的请求者泄露账号状态（10202）。
	if !checkPassword(admin.PasswordHash, in.Password) {
		s.recordLoginFailure(keys)
		return nil, ErrBadCredentials
	}
	if !admin.IsEnabled() {
		return nil, errcode.ErrDisabled
	}
	s.resetLoginFailures(keys)

	if err := s.repo.UpdateLastLogin(ctx, admin.ID, s.now()); err != nil {
		return nil, err
	}

	perms, err := s.Permissions(ctx, admin.ID)
	if err != nil {
		return nil, err
	}

	return &LoginResult{Admin: admin, Permissions: perms}, nil
}

// ChangePassword 修改自身口令：校验旧口令、复杂度与新旧不同，成功后清除首登改密标记。
// 旧 token 失效由接入层（登出/改密 handler）登记 jti 黑名单完成。
func (s *Service) ChangePassword(ctx context.Context, adminID int64, oldPassword, newPassword string) error {
	if s == nil || s.repo == nil {
		return errcode.ErrDependency
	}

	admin, err := s.repo.GetAdminByID(ctx, adminID)
	if err != nil {
		return err
	}

	if !checkPassword(admin.PasswordHash, oldPassword) {
		return ErrOldPasswordWrong
	}
	if err := ValidatePassword(newPassword); err != nil {
		return err
	}
	if checkPassword(admin.PasswordHash, newPassword) {
		return ErrPasswordSame
	}

	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	if err := s.repo.UpdatePassword(ctx, adminID, hash, false); err != nil {
		return err
	}

	s.InvalidatePermCache(adminID)
	return nil
}

// GetAdmin 返回管理员详情（GET /auth/me 使用，不含口令哈希输出由接入层裁剪）。
func (s *Service) GetAdmin(ctx context.Context, adminID int64) (*domain.Admin, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	return s.repo.GetAdminByID(ctx, adminID)
}

// Principal 加载当前主体的鉴权快照：账号状态每请求实时校验，权限走短 TTL 缓存。
//
// 错误语义：账号不存在 → errcode.ErrNotFound；账号非启用 → errcode.ErrDisabled。
func (s *Service) Principal(ctx context.Context, adminID int64) (*Principal, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}

	admin, err := s.repo.GetAdminByID(ctx, adminID)
	if err != nil {
		return nil, err
	}
	if !admin.IsEnabled() {
		return nil, errcode.ErrDisabled
	}

	perms, err := s.Permissions(ctx, adminID)
	if err != nil {
		return nil, err
	}

	return &Principal{
		AdminID:            admin.ID,
		Username:           admin.Username,
		Status:             admin.Status,
		MustChangePassword: admin.MustChangePassword,
		Permissions:        perms,
	}, nil
}

// Permissions 返回管理员权限并集（短 TTL 缓存）。
func (s *Service) Permissions(ctx context.Context, adminID int64) (map[string]bool, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}

	if perms, ok := s.cachedPerms(adminID); ok {
		return perms, nil
	}

	perms, err := s.repo.ListPermissionsByAdmin(ctx, adminID)
	if err != nil {
		return nil, err
	}
	if perms == nil {
		perms = map[string]bool{}
	}

	s.mu.Lock()
	s.permCache[adminID] = permCacheEntry{perms: perms, exp: s.now().Add(s.permTTL)}
	s.mu.Unlock()
	return perms, nil
}

// InvalidatePermCache 失效指定管理员的权限缓存。
// 调用点：角色授权/角色权限矩阵变更/管理员停用启用/口令重置（Phase 1 任务 1.3/1.4）。
func (s *Service) InvalidatePermCache(adminID int64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	delete(s.permCache, adminID)
	s.mu.Unlock()
}

// RevokeToken 将 jti 加入失效名单，ttl 为 token 剩余有效期（过期后自动清理）。
func (s *Service) RevokeToken(jti string, ttl time.Duration) {
	if s == nil || jti == "" || ttl <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.blacklist[jti] = s.now().Add(ttl)
	s.pruneBlacklistLocked()
}

// IsRevoked 判断 jti 是否已失效（登出/改密后旧 token 重放）。
func (s *Service) IsRevoked(jti string) bool {
	if s == nil || jti == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.blacklist[jti]
	if !ok {
		return false
	}
	if !s.now().Before(exp) {
		delete(s.blacklist, jti)
		return false
	}
	return true
}

func (s *Service) cachedPerms(adminID int64) (map[string]bool, bool) {
	s.mu.Lock()
	entry, ok := s.permCache[adminID]
	s.mu.Unlock()
	if !ok || !s.now().Before(entry.exp) {
		return nil, false
	}
	return entry.perms, true
}

// pruneBlacklistLocked 清理已过期条目（调用方需持有锁）。
func (s *Service) pruneBlacklistLocked() {
	now := s.now()
	for jti, exp := range s.blacklist {
		if !now.Before(exp) {
			delete(s.blacklist, jti)
		}
	}
}

// isLoginLocked 判断任一维度（账号/IP）是否处于锁定期。
func (s *Service) isLoginLocked(keys []string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.guard.locked(keys, s.now())
}

// recordLoginFailure 记录一次失败（账号 + IP 双维度计数）。
func (s *Service) recordLoginFailure(keys []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.guard.fail(keys, s.now())
}

// resetLoginFailures 登录成功后清零失败计数。
func (s *Service) resetLoginFailures(keys []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.guard.reset(keys)
}
