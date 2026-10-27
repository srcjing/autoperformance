// Package crypto 提供密钥与敏感数据加密原语。
//
// 对齐 security-design-report.md DES-I-002 / CODE-I-002：
//   - 仅使用标准算法（AES-256-GCM），随机数一律 crypto/rand（禁 math/rand）；
//   - 信封加密：数据用 DEK 加密，DEK 再用 KEK 包裹，格式 KEK(DEK) || DEK(data)。
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	// KeySize 密钥长度（AES-256）。
	KeySize = 32
	// nonceSize GCM 标准 nonce 长度（12 字节）。
	nonceSize = 12
)

// NewKEK 生成 32 字节根密钥 KEK（crypto/rand）。
// KEK 首次安装时生成，落盘文件权限 0600，仅服务账号可读。
func NewKEK() ([]byte, error) {
	return randomBytes(KeySize)
}

// DeriveDEK 生成 32 字节数据加密密钥 DEK（crypto/rand）。
func DeriveDEK() ([]byte, error) {
	return randomBytes(KeySize)
}

// EncryptGCM 使用 AES-256-GCM 加密明文，输出 nonce || ciphertext||tag。
// key 长度必须为 32 字节（AES-256）。
func EncryptGCM(plain, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("生成 nonce 失败: %w", err)
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

// DecryptGCM 解密 EncryptGCM 的输出（nonce || ciphertext||tag）。
func DecryptGCM(blob, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(blob) < gcm.NonceSize() {
		return nil, errors.New("crypto: 密文过短")
	}
	nonce, ct := blob[:gcm.NonceSize()], blob[gcm.NonceSize():]
	return gcm.Open(nil, nonce, ct, nil)
}

// EncryptSecret 信封加密：KEK(DEK) || DEK(data)。
// 输出格式：[4 字节大端长度 len(encDek)][encDek][ct]，解密时据此切分。
func EncryptSecret(plain, kek []byte) ([]byte, error) {
	dek, err := DeriveDEK()
	if err != nil {
		return nil, err
	}
	encDek, err := EncryptGCM(dek, kek)
	if err != nil {
		return nil, err
	}
	ct, err := EncryptGCM(plain, dek)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 4+len(encDek)+len(ct))
	binary.BigEndian.PutUint32(out[:4], uint32(len(encDek)))
	copy(out[4:], encDek)
	copy(out[4+len(encDek):], ct)
	return out, nil
}

// DecryptSecret 解密 EncryptSecret 的输出。
func DecryptSecret(blob, kek []byte) ([]byte, error) {
	if len(blob) < 4 {
		return nil, errors.New("crypto: 信封密文过短")
	}
	encDekLen := int(binary.BigEndian.Uint32(blob[:4]))
	if 4+encDekLen > len(blob) {
		return nil, errors.New("crypto: 信封密文长度非法")
	}
	encDek := blob[4 : 4+encDekLen]
	ct := blob[4+encDekLen:]

	dek, err := DecryptGCM(encDek, kek)
	if err != nil {
		return nil, fmt.Errorf("解密 DEK 失败: %w", err)
	}
	return DecryptGCM(ct, dek)
}

func randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return nil, fmt.Errorf("crypto/rand 读取失败: %w", err)
	}
	return b, nil
}
