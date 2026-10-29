// Package auth 实现认证与授权基础能力：JWT 签发/校验、权限点模型与判定。
//
// 权限模型（对齐 architecture.md §5.3）：
//   - 权限点格式 module_code:permission（permission ∈ {read, write}）；
//   - 管理员最终权限 = 所授角色权限并集；write 蕴含 read；
//   - 权限不写入 JWT，每次请求从 DB/缓存读取。
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Claims JWT 载荷：sub=admin_id, username, jti, iat, exp。
// 权限不写入 token（DES-E-007：服务端按请求实时/缓存鉴权）。
type Claims struct {
	Username string `json:"username"`
	jwt.RegisteredClaims
}

// KeyID 返回 token 唯一标识 jti（登出失效黑名单的键）。
func (c *Claims) KeyID() string {
	if c == nil {
		return ""
	}
	return c.ID
}

// GenerateToken 签发 JWT（HS256），携带随机 jti（crypto/rand，16 字节 hex）。
func GenerateToken(adminID, username, secret string, ttl time.Duration) (string, error) {
	jti, err := newJTI()
	if err != nil {
		return "", err
	}
	now := time.Now()
	claims := Claims{
		Username: username,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Subject:   adminID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

// ParseToken 解析并校验 JWT（含过期校验）。
func ParseToken(tokenStr, secret string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

// newJTI 生成 token 唯一标识（crypto/rand，禁 math/rand，对齐 DES-I-003）。
func newJTI() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("生成 jti 失败: %w", err)
	}
	return hex.EncodeToString(b), nil
}
