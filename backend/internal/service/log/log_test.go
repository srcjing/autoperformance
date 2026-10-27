package log

import (
	"bytes"
	"context"
	"testing"
	"time"

	"auto_performance/internal/domain"
)

// memStore 内存审计仓储，模拟 user_log（append-only）。
type memStore struct {
	entries []domain.AuditLog
}

func (m *memStore) LastAuditHash(_ context.Context) ([]byte, error) {
	if len(m.entries) == 0 {
		return nil, nil
	}
	return m.entries[len(m.entries)-1].Hash, nil
}

func (m *memStore) AppendUserLog(_ context.Context, entry *domain.AuditLog) error {
	m.entries = append(m.entries, *entry)
	return nil
}

var testKey = []byte("0123456789abcdef0123456789abcdef")

func newEvent(action, detail string) AuditEvent {
	adminID := int64(7)
	return AuditEvent{
		AdminID:      &adminID,
		Action:       action,
		ResourceType: "environment",
		ResourceID:   "12",
		Detail:       detail,
		IP:           "10.0.0.8",
		Result:       domain.AuditResultOK,
	}
}

// TestHMACChainDetectsTamper 验证写入 2 条后篡改第 1 条（含重算链）均能被检测（INV-8）。
func TestHMACChainDetectsTamper(t *testing.T) {
	ctx := context.Background()
	store := &memStore{}
	svc := NewService(store)

	h1, err := svc.AppendAudit(ctx, nil, newEvent("create", "POST /api/v1/environments status=200"), testKey)
	if err != nil {
		t.Fatalf("append #1 失败: %v", err)
	}
	h2, err := svc.AppendAudit(ctx, nil, newEvent("delete", "DELETE /api/v1/environments/12 status=200"), testKey)
	if err != nil {
		t.Fatalf("append #2 失败: %v", err)
	}
	if len(h1) != 32 || len(h2) != 32 {
		t.Fatalf("hash 应为 32 字节 HMAC-SHA256，got %d/%d", len(h1), len(h2))
	}
	if bytes.Equal(h1, h2) {
		t.Fatal("不同条目的 hash 不应相同")
	}
	if !bytes.Equal(store.entries[1].PrevHash, h1) {
		t.Fatal("第 2 条的 prev_hash 应链接第 1 条的 hash")
	}
	if err := VerifyAuditChain(store.entries, testKey); err != nil {
		t.Fatalf("未篡改时链校验应通过，got %v", err)
	}

	// 场景 1：直接篡改第 1 条内容（模拟绕过触发器的 DB 改写）。
	store.entries[0].Detail = "POST /api/v1/admins status=200"
	if err := VerifyAuditChain(store.entries, testKey); err == nil {
		t.Fatal("篡改第 1 条后链校验应失败")
	}

	// 场景 2：攻击者用篡改后的内容重算第 1 条 hash，链条其余部分仍未对齐。
	recomputed, err := ChainHash(nil, AuditEvent{
		AdminID:      store.entries[0].AdminID,
		Action:       store.entries[0].Action,
		ResourceType: store.entries[0].ResourceType,
		ResourceID:   store.entries[0].ResourceID,
		Detail:       store.entries[0].Detail,
		IP:           store.entries[0].IP,
		Result:       store.entries[0].Result,
	}, store.entries[0].CreatedAt, testKey)
	if err != nil {
		t.Fatalf("重算 hash 失败: %v", err)
	}
	if bytes.Equal(recomputed, h1) {
		t.Fatal("篡改内容重算的 hash 不应等于原 hash")
	}
	store.entries[0].Hash = recomputed
	if err := VerifyAuditChain(store.entries, testKey); err == nil {
		t.Fatal("重算第 1 条 hash 后链校验仍应失败（第 2 条 prev_hash 不再匹配）")
	}
}

// TestAppendAuditUsesPrevHash 验证显式传入 prevHash 时以其作为链输入（分布式追加场景）。
func TestAppendAuditUsesPrevHash(t *testing.T) {
	ctx := context.Background()
	store := &memStore{}
	svc := NewService(store)

	prev := bytes.Repeat([]byte{0xab}, 32)
	h, err := svc.AppendAudit(ctx, prev, newEvent("create", "detail"), testKey)
	if err != nil {
		t.Fatalf("append 失败: %v", err)
	}
	if !bytes.Equal(store.entries[0].PrevHash, prev) {
		t.Fatal("应使用调用方传入的 prevHash")
	}
	if !bytes.Equal(store.entries[0].Hash, h) {
		t.Fatal("返回值应为落库的 hash")
	}
}

// TestAppendAuditSanitizesDetail 验证写库前统一脱敏（DES-I-001：日志禁止明文凭据）。
func TestAppendAuditSanitizesDetail(t *testing.T) {
	ctx := context.Background()
	store := &memStore{}
	svc := NewService(store)

	if _, err := svc.AppendAudit(ctx, nil,
		newEvent("login", "Authorization: Bearer secret-token password=TestPwd!42"), testKey); err != nil {
		t.Fatalf("append 失败: %v", err)
	}
	got := store.entries[0].Detail
	if bytes.Contains([]byte(got), []byte("secret-token")) || bytes.Contains([]byte(got), []byte("TestPwd!42")) {
		t.Fatalf("detail 未脱敏: %s", got)
	}
}

// TestAppendAuditDefaultsCreatedAt 验证未指定时间时自动填充（保证 payload 可复算）。
func TestAppendAuditDefaultsCreatedAt(t *testing.T) {
	ctx := context.Background()
	store := &memStore{}
	svc := NewService(store)

	before := time.Now().Add(-time.Second)
	if _, err := svc.AppendAudit(ctx, nil, newEvent("create", "detail"), testKey); err != nil {
		t.Fatalf("append 失败: %v", err)
	}
	got := store.entries[0].CreatedAt
	if got.Before(before) {
		t.Fatalf("created_at 未填充: %v", got)
	}
}

// TestVerifyAuditChainEmpty 空链视为通过。
func TestVerifyAuditChainEmpty(t *testing.T) {
	if err := VerifyAuditChain(nil, testKey); err != nil {
		t.Fatalf("空链应通过, got %v", err)
	}
}

// TestDeriveAuditKey 验证密钥优先级与长度（显式 > 环境秘密 > 派生兜底）。
func TestDeriveAuditKey(t *testing.T) {
	a := DeriveAuditKey("explicit-key", "env-secret", "jwt-secret")
	b := DeriveAuditKey("explicit-key", "env-secret", "jwt-secret")
	if len(a) != 32 {
		t.Fatalf("审计密钥应为 32 字节, got %d", len(a))
	}
	if !bytes.Equal(a, b) {
		t.Fatal("相同输入应派生出相同密钥")
	}
	if bytes.Equal(a, DeriveAuditKey("", "env-secret", "jwt-secret")) {
		t.Fatal("显式密钥应优先于环境秘密")
	}
	if bytes.Equal(DeriveAuditKey("", "env-secret", "jwt-secret"), DeriveAuditKey("", "", "jwt-secret")) {
		t.Fatal("环境秘密应优先于 JWT 派生兜底")
	}
}
