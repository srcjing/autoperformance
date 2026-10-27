package crypto

import (
	"bytes"
	"testing"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key, err := NewKEK()
	if err != nil {
		t.Fatalf("NewKEK() 失败: %v", err)
	}
	if len(key) != KeySize {
		t.Fatalf("NewKEK() 长度 = %d, want %d", len(key), KeySize)
	}

	plain := []byte("sensitive-auth-token-123456")

	// 单层 GCM 往返
	ct, err := EncryptGCM(plain, key)
	if err != nil {
		t.Fatalf("EncryptGCM() 失败: %v", err)
	}
	if bytes.Equal(ct, plain) {
		t.Fatal("密文不应与明文相同")
	}
	got, err := DecryptGCM(ct, key)
	if err != nil {
		t.Fatalf("DecryptGCM() 失败: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("解密结果不一致: got %q, want %q", got, plain)
	}

	// 信封加密往返：KEK(DEK) || DEK(data)
	blob, err := EncryptSecret(plain, key)
	if err != nil {
		t.Fatalf("EncryptSecret() 失败: %v", err)
	}
	got, err = DecryptSecret(blob, key)
	if err != nil {
		t.Fatalf("DecryptSecret() 失败: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("信封解密结果不一致: got %q, want %q", got, plain)
	}
}

func TestEncryptGCMNonceIsRandom(t *testing.T) {
	key, _ := NewKEK()
	plain := []byte("same-plaintext")
	c1, _ := EncryptGCM(plain, key)
	c2, _ := EncryptGCM(plain, key)
	// 随机 nonce 保证相同明文两次加密结果不同
	if bytes.Equal(c1, c2) {
		t.Fatal("相同明文两次加密结果应不同（随机 nonce）")
	}
}

func TestDecryptGCMRejectsTamper(t *testing.T) {
	key, _ := NewKEK()
	ct, _ := EncryptGCM([]byte("secret"), key)
	ct[len(ct)-1] ^= 0xff // 篡改最后一字节
	if _, err := DecryptGCM(ct, key); err == nil {
		t.Fatal("篡改密文应解密失败")
	}
}

func TestDecryptSecretWrongKEK(t *testing.T) {
	kek1, _ := NewKEK()
	kek2, _ := NewKEK()
	blob, _ := EncryptSecret([]byte("data"), kek1)
	if _, err := DecryptSecret(blob, kek2); err == nil {
		t.Fatal("使用错误 KEK 应解密失败")
	}
}
