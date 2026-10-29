package setting

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"auto_performance/internal/crypto"
	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// TestCertKeyEncrypted 证书私钥红线（DES-I-002/DES-I-008）：
// 落库内容必须是 KEK 信封密文（可用 KEK 解回明文），响应视图不含任何密钥材料。
func TestCertKeyEncrypted(t *testing.T) {
	svc, certs, _, kek := newSecretService(t)
	ctx := context.Background()

	certPEM, keyPEM := testCertPEM(t, "external.example.com")
	view, err := svc.CreateCertificate(ctx, CertificateInput{
		Name:              "外部服务证书",
		Issuer:            "Test CA",
		AssociatedService: "gateway",
		CertPEM:           certPEM,
		KeyPEM:            keyPEM,
	})
	if err != nil {
		t.Fatalf("导入证书失败: %v", err)
	}

	rec := certs.lastCreated
	if rec == nil {
		t.Fatal("证书未落库")
	}
	// 1) 私钥不得明文落库：既不等于原文，也不含 PEM 头。
	if bytes.Equal(rec.KeyEnc, keyPEM) {
		t.Fatal("私钥以明文落库（红线违规）")
	}
	if bytes.Contains(rec.KeyEnc, []byte("PRIVATE KEY")) {
		t.Fatal("私钥密文中仍可检索到 PEM 头")
	}
	// 2) 证书内容同样加密存储。
	if bytes.Equal(rec.CertEnc, certPEM) || bytes.Contains(rec.CertEnc, []byte("CERTIFICATE")) {
		t.Fatal("证书内容以明文落库")
	}
	// 3) 用 KEK 信封解密可还原明文（说明加密可用、密钥未丢失）。
	keyPlain, err := crypto.DecryptSecret(rec.KeyEnc, kek)
	if err != nil {
		t.Fatalf("用 KEK 解密私钥失败: %v", err)
	}
	if !bytes.Equal(keyPlain, keyPEM) {
		t.Fatal("解密结果与原始私钥不一致")
	}
	certPlain, err := crypto.DecryptSecret(rec.CertEnc, kek)
	if err != nil {
		t.Fatalf("用 KEK 解密证书失败: %v", err)
	}
	if !bytes.Equal(certPlain, certPEM) {
		t.Fatal("解密结果与原始证书不一致")
	}

	// 4) 视图脱敏：只暴露 HasKey，不含密文/明文。
	if !view.HasKey {
		t.Fatal("导入带私钥的证书应标记 has_key")
	}
	if view.IsBuiltin {
		t.Fatal("导入证书不得标记为内置证书")
	}
	if view.Status != domain.CertStatusValid {
		t.Fatalf("新导入证书状态 = %q, want %q", view.Status, domain.CertStatusValid)
	}
	if view.ValidTo == nil || view.ValidFrom == nil {
		t.Fatal("有效期应由证书内容回填")
	}

	// 5) 列表接口同样只返回脱敏元数据。
	page, err := svc.ListCertificates(ctx, 1, 20)
	if err != nil {
		t.Fatalf("查询证书列表失败: %v", err)
	}
	var found bool
	for _, item := range page.List {
		if item.ID != view.ID {
			continue
		}
		found = true
		if !item.HasKey || item.Name != "外部服务证书" {
			t.Fatalf("列表项异常: %+v", item)
		}
	}
	if !found {
		t.Fatal("列表未返回刚导入的证书")
	}
}

// TestReinitBuiltinCertOnly 重新初始化：仅内置证书可用（非内置 → 10401），且只更新有效期。
func TestReinitBuiltinCertOnly(t *testing.T) {
	svc, certs, _, _ := newSecretService(t)
	ctx := context.Background()

	if err := svc.EnsureBuiltinCertificate(ctx); err != nil {
		t.Fatalf("初始化内置证书失败: %v", err)
	}
	builtin := mustFindBuiltinCert(t, certs)

	// 模拟「旧的」内置证书：有效期已过，等待重新初始化。
	oldFrom := time.Now().UTC().AddDate(-2, 0, 0).Truncate(24 * time.Hour)
	oldTo := time.Now().UTC().AddDate(-1, 0, 0).Truncate(24 * time.Hour)
	builtin.ValidFrom, builtin.ValidTo = &oldFrom, &oldTo
	keyEncBefore := append([]byte(nil), builtin.KeyEnc...)

	view, err := svc.ReinitCertificate(ctx, builtin.ID)
	if err != nil {
		t.Fatalf("重新初始化内置证书失败: %v", err)
	}
	if !view.IsBuiltin {
		t.Fatal("重新初始化后仍应为内置证书")
	}
	if view.ValidTo == nil || !view.ValidTo.After(oldTo) {
		t.Fatalf("有效期未续期: valid_to=%v, old=%v", view.ValidTo, oldTo)
	}
	if certs.updateValidityCalls != 1 {
		t.Fatalf("重新初始化应只调用一次有效期更新, got %d", certs.updateValidityCalls)
	}
	// 仅更新有效期：证书内容与私钥密文保持不变（不重签、不换密钥）。
	if !bytes.Equal(certs.certs[builtin.ID].KeyEnc, keyEncBefore) {
		t.Fatal("重新初始化不得改动私钥内容")
	}
	if view.Status != domain.CertStatusValid {
		t.Fatalf("续期后状态 = %q, want valid", view.Status)
	}

	// 非内置证书调用 → 10401（仅内置自签证书可重新初始化）。
	certPEM, keyPEM := testCertPEM(t, "imported.example.com")
	imported, err := svc.CreateCertificate(ctx, CertificateInput{
		Name: "外部证书", CertPEM: certPEM, KeyPEM: keyPEM,
	})
	if err != nil {
		t.Fatalf("导入证书失败: %v", err)
	}
	if _, err := svc.ReinitCertificate(ctx, imported.ID); !errors.Is(err, errcode.ErrState) {
		t.Fatalf("非内置证书 reinit 应返回 10401，got %v", err)
	}

	// 不存在的证书 → 10301。
	if _, err := svc.ReinitCertificate(ctx, 999999); !errors.Is(err, errcode.ErrNotFound) {
		t.Fatalf("不存在证书 reinit 应返回 10301，got %v", err)
	}
}

// TestDeleteBuiltinCertForbidden 内置自签证书不可删除（26004）；导入证书可删除。
func TestDeleteBuiltinCertForbidden(t *testing.T) {
	svc, certs, _, _ := newSecretService(t)
	ctx := context.Background()

	if err := svc.EnsureBuiltinCertificate(ctx); err != nil {
		t.Fatalf("初始化内置证书失败: %v", err)
	}
	builtin := mustFindBuiltinCert(t, certs)

	err := svc.DeleteCertificate(ctx, builtin.ID)
	if !errors.Is(err, ErrBuiltinCertProtected) {
		t.Fatalf("删除内置证书应返回 26004，got %v", err)
	}
	if ErrBuiltinCertProtected.Code != 26004 || ErrBuiltinCertProtected.HTTP != 403 {
		t.Fatalf("错误码定义异常: %+v", ErrBuiltinCertProtected)
	}
	if len(certs.deleted) != 0 {
		t.Fatal("内置证书不得被删除")
	}

	certPEM, keyPEM := testCertPEM(t, "removable.example.com")
	imported, err := svc.CreateCertificate(ctx, CertificateInput{
		Name: "可删除证书", CertPEM: certPEM, KeyPEM: keyPEM,
	})
	if err != nil {
		t.Fatalf("导入证书失败: %v", err)
	}
	if err := svc.DeleteCertificate(ctx, imported.ID); err != nil {
		t.Fatalf("删除导入证书失败: %v", err)
	}
	if len(certs.deleted) != 1 || certs.deleted[0] != imported.ID {
		t.Fatalf("逻辑删除记录异常: %v", certs.deleted)
	}
}

// TestCertStatusDerived 证书状态按有效期推导（valid / expiring / expired）。
func TestCertStatusDerived(t *testing.T) {
	svc, certs, _, _ := newSecretService(t)
	ctx := context.Background()

	fixed := time.Date(2026, 10, 26, 9, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return fixed }

	cases := []struct {
		name      string
		validTo   string
		wantState string
	}{
		{"有效证书", "2027-10-26", domain.CertStatusValid},
		{"即将过期", "2026-11-10", domain.CertStatusExpiring},
		{"已过期", "2026-09-01", domain.CertStatusExpired},
	}
	for _, tc := range cases {
		certPEM, _ := testCertPEM(t, tc.name)
		view, err := svc.CreateCertificate(ctx, CertificateInput{
			Name:      tc.name,
			ValidFrom: "2025-01-01",
			ValidTo:   tc.validTo,
			CertPEM:   certPEM,
		})
		if err != nil {
			t.Fatalf("%s: 导入失败: %v", tc.name, err)
		}
		if view.Status != tc.wantState {
			t.Fatalf("%s: status = %q, want %q", tc.name, view.Status, tc.wantState)
		}
		if view.HasKey {
			t.Fatalf("%s: 未提供私钥时 has_key 应为 false", tc.name)
		}
	}

	// 名称重复 → 10302。
	certPEM, _ := testCertPEM(t, "dup.example.com")
	if _, err := svc.CreateCertificate(ctx, CertificateInput{Name: "有效证书", CertPEM: certPEM}); !errors.Is(err, errcode.ErrConflict) {
		t.Fatalf("证书重名应返回 10302，got %v", err)
	}

	// 非法证书内容 → 10003（避免脏数据入库）。
	if _, err := svc.CreateCertificate(ctx, CertificateInput{Name: "非法证书", CertPEM: []byte("not-a-pem")}); !errors.Is(err, errcode.ErrParamFmt) {
		t.Fatalf("非法证书内容应返回 10003，got %v", err)
	}
	if len(certs.certs) != len(cases)+0 {
		// 内置证书此时未创建（未调用 List/Ensure），仅应存在上面导入成功的 3 条。
		t.Fatalf("证书条数 = %d, want %d", len(certs.certs), len(cases))
	}
}
