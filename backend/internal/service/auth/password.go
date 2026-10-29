// Package auth 认证与授权业务服务。
//
// password.go 承载口令哈希与复杂度策略、登录失败锁定策略
// （DES-S-006：bcrypt cost≥10 + 账号/IP 双维度锁定 + 防爆破）。
package auth

import (
	"strings"
	"time"
	"unicode"

	"golang.org/x/crypto/bcrypt"
)

const (
	// DefaultMaxLoginFailures 连续登录失败锁定阈值。
	DefaultMaxLoginFailures = 5
	// DefaultLoginLockDuration 登录锁定时长。
	DefaultLoginLockDuration = 15 * time.Minute

	// MinPasswordLen 口令最小长度（PRD/API 契约：≥8 位）。
	MinPasswordLen = 8
	// MaxPasswordLen bcrypt 输入上限（超过 72 字节会被静默截断，必须拒绝）。
	MaxPasswordLen = 72

	// maxTrackedLoginKeys 登录失败计数追踪键上限（防止用户名/IP 喷洒导致内存放大）。
	maxTrackedLoginKeys = 10000
)

// HashPassword 使用 bcrypt 生成口令哈希（cost=10，对齐 DES-S-006 的 `$2a$10$` 要求）。
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// ValidatePassword 校验口令复杂度：≥8 位、≤72 字节，且包含大写/小写/数字/特殊字符。
// 不合规返回 ErrPasswordWeak（20004）。
func ValidatePassword(password string) error {
	if len(password) < MinPasswordLen || len(password) > MaxPasswordLen {
		return ErrPasswordWeak
	}
	var hasLower, hasUpper, hasDigit, hasSpecial bool
	for _, r := range password {
		switch {
		case unicode.IsLower(r):
			hasLower = true
		case unicode.IsUpper(r):
			hasUpper = true
		case unicode.IsDigit(r):
			hasDigit = true
		case !unicode.IsSpace(r):
			hasSpecial = true
		}
	}
	if !hasLower || !hasUpper || !hasDigit || !hasSpecial {
		return ErrPasswordWeak
	}
	return nil
}

// checkPassword 比对明文口令与 bcrypt 哈希（哈希非法/空串一律视为不匹配）。
func checkPassword(hash, password string) bool {
	if hash == "" || password == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

// loginGuard 登录失败计数与锁定（账号 + IP 双维度，内存级，随服务实例生命周期）。
type loginGuard struct {
	maxFailures int
	lockFor     time.Duration
	failures    map[string]int
	lockedUntil map[string]time.Time
}

// newLoginGuard 构造登录锁定状态。
func newLoginGuard(maxFailures int, lockFor time.Duration) loginGuard {
	return loginGuard{
		maxFailures: maxFailures,
		lockFor:     lockFor,
		failures:    make(map[string]int),
		lockedUntil: make(map[string]time.Time),
	}
}

// loginGuardKeys 生成账号/IP 维度键（用户名小写归一，避免大小写绕过计数）。
func loginGuardKeys(username, ip string) []string {
	keys := make([]string, 0, 2)
	if u := strings.ToLower(strings.TrimSpace(username)); u != "" {
		keys = append(keys, "user:"+u)
	}
	if ip = strings.TrimSpace(ip); ip != "" {
		keys = append(keys, "ip:"+ip)
	}
	return keys
}

// locked 判断任一维度是否仍在锁定期（顺手清理已过期锁定）。
func (g *loginGuard) locked(keys []string, now time.Time) bool {
	locked := false
	for _, k := range keys {
		exp, ok := g.lockedUntil[k]
		if !ok {
			continue
		}
		if now.Before(exp) {
			locked = true
			continue
		}
		delete(g.lockedUntil, k)
	}
	return locked
}

// fail 记录一次失败；达到阈值即锁定对应维度并清零计数。
func (g *loginGuard) fail(keys []string, now time.Time) {
	if g.failures == nil {
		g.failures = make(map[string]int)
	}
	if g.lockedUntil == nil {
		g.lockedUntil = make(map[string]time.Time)
	}
	// 键空间封顶：超限时清空计数（锁定记录保留），保证内存有界且锁定仍然生效。
	if len(g.failures) > maxTrackedLoginKeys {
		g.failures = make(map[string]int)
	}
	for _, k := range keys {
		g.failures[k]++
		if g.failures[k] >= g.maxFailures {
			g.lockedUntil[k] = now.Add(g.lockFor)
			delete(g.failures, k)
		}
	}
}

// reset 登录成功后清零失败计数（锁定期内不会走到这里）。
func (g *loginGuard) reset(keys []string) {
	for _, k := range keys {
		delete(g.failures, k)
	}
}
