package setting

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"auto_performance/internal/crypto"
	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// TestSshKeyEncrypted SSH 私钥红线（DES-I-002/DES-I-008）：
// 私钥信封加密落库、可用 KEK 解回明文、视图只暴露 has_key。
func TestSshKeyEncrypted(t *testing.T) {
	svc, _, hosts, kek := newSecretService(t)
	ctx := context.Background()

	_, keyPEM := testCertPEM(t, "ssh-key") // PKCS#8 ECDSA 私钥，可被 ssh.ParsePrivateKey 解析
	view, err := svc.CreateSshHost(ctx, SshHostInput{
		Type:     domain.SshHostTypeSSH,
		Host:     "10.20.30.40",
		Port:     2222,
		User:     "autoperf",
		AuthType: domain.SshAuthTypeKey,
		Key:      string(keyPEM),
	})
	if err != nil {
		t.Fatalf("新增 SSH 记录失败: %v", err)
	}

	rec := hosts.created[0]
	if bytes.Equal(rec.KeyEnc, keyPEM) || bytes.Contains(rec.KeyEnc, []byte("PRIVATE KEY")) {
		t.Fatal("SSH 私钥以明文落库（红线违规）")
	}
	plain, err := crypto.DecryptSecret(rec.KeyEnc, kek)
	if err != nil {
		t.Fatalf("用 KEK 解密 SSH 私钥失败: %v", err)
	}
	if !bytes.Equal(plain, keyPEM) {
		t.Fatal("解密结果与原始 SSH 私钥不一致")
	}

	if !view.HasKey {
		t.Fatal("key 认证记录应标记 has_key")
	}
	if view.Port != 2222 || view.User != "autoperf" || view.AuthType != domain.SshAuthTypeKey {
		t.Fatalf("视图字段异常: %+v", view)
	}

	// 修改时不传私钥 → 沿用原密文（前端不回显密钥）。
	updated, err := svc.UpdateSshHost(ctx, view.ID, SshHostInput{
		Type:     domain.SshHostTypeSSH,
		Host:     "10.20.30.40",
		Port:     2222,
		User:     "autoperf",
		AuthType: domain.SshAuthTypeKey,
		Version:  view.Version,
	})
	if err != nil {
		t.Fatalf("修改 SSH 记录失败: %v", err)
	}
	if !updated.HasKey || !bytes.Equal(hosts.hosts[view.ID].KeyEnc, rec.KeyEnc) {
		t.Fatal("未传私钥时应保持原密文不变")
	}

	// password 认证不保存私钥。
	pwdView, err := svc.CreateSshHost(ctx, SshHostInput{
		Type:     domain.SshHostTypeSSH,
		Host:     "10.20.30.41",
		User:     "autoperf",
		AuthType: domain.SshAuthTypePassword,
		Key:      string(keyPEM), // 夹带私钥也应被丢弃
	})
	if err != nil {
		t.Fatalf("新增 password 认证记录失败: %v", err)
	}
	if pwdView.HasKey || len(hosts.hosts[pwdView.ID].KeyEnc) != 0 {
		t.Fatal("password 认证不得保存 SSH 私钥")
	}
}

// TestDeleteReferencedSshHost 被环境引用 → 26005，且不删除；解除引用后可删除。
func TestDeleteReferencedSshHost(t *testing.T) {
	svc, _, hosts, _ := newSecretService(t)
	ctx := context.Background()

	view, err := svc.CreateSshHost(ctx, SshHostInput{
		Type: domain.SshHostTypeHost,
		Host: "host-a.example.com",
		HostEntries: []string{
			"10.0.0.11 host-a.example.com",
		},
	})
	if err != nil {
		t.Fatalf("新增 Host 解析记录失败: %v", err)
	}

	hosts.envRefs[view.ID] = 2
	err = svc.DeleteSshHost(ctx, view.ID)
	if !errors.Is(err, ErrSshHostReferenced) {
		t.Fatalf("被引用记录删除应返回 26005，got %v", err)
	}
	if ErrSshHostReferenced.Code != 26005 || ErrSshHostReferenced.HTTP != 409 {
		t.Fatalf("错误码定义异常: %+v", ErrSshHostReferenced)
	}
	if len(hosts.deleted) != 0 {
		t.Fatal("被引用的 SSH/Host 不得删除")
	}

	delete(hosts.envRefs, view.ID)
	if err := svc.DeleteSshHost(ctx, view.ID); err != nil {
		t.Fatalf("解除引用后删除失败: %v", err)
	}
	if len(hosts.deleted) != 1 || hosts.deleted[0] != view.ID {
		t.Fatalf("逻辑删除记录异常: %v", hosts.deleted)
	}
}

// TestImportSkipsDuplicates 批量导入：重复项（库内已有/文件内重复）跳过，非法行计入 invalid。
func TestImportSkipsDuplicates(t *testing.T) {
	svc, _, hosts, _ := newSecretService(t)
	ctx := context.Background()

	// 预先存在一条同 type+host+port 记录。
	if _, err := svc.CreateSshHost(ctx, SshHostInput{
		Type: domain.SshHostTypeHost,
		Host: "dup.example.com",
		HostEntries: []string{
			"10.0.0.21 dup.example.com",
		},
	}); err != nil {
		t.Fatalf("预置记录失败: %v", err)
	}

	jsonPayload := `[
		{"type":"host","host":"dup.example.com","host_entries":["10.0.0.21 dup.example.com"]},
		{"type":"host","host":"new.example.com","host_entries":["10.0.0.22 new.example.com"]},
		{"type":"host","host":"new.example.com","host_entries":["10.0.0.22 new.example.com"]},
		{"type":"ssh","host":"$(reboot)","port":22,"user":"root","auth_type":"password"}
	]`
	res, err := svc.ImportSshHosts(ctx, "hosts.json", int64(len(jsonPayload)), strings.NewReader(jsonPayload))
	if err != nil {
		t.Fatalf("JSON 导入失败: %v", err)
	}
	if res.Total != 4 || res.Created != 1 || res.Skipped != 2 || res.Invalid != 1 {
		t.Fatalf("JSON 导入统计异常: %+v", res)
	}
	if len(res.Errors) != 1 || !strings.Contains(res.Errors[0], "第 4 行") {
		t.Fatalf("非法行错误样例异常: %v", res.Errors)
	}

	csvPayload := "type,host,port,user,auth_type,host_entries,description\n" +
		"host,dup.example.com,,,,\"10.0.0.21 dup.example.com\",dup\n" +
		"host,csv.example.com,,,,\"10.0.0.23 csv.example.com\",new\n"
	res, err = svc.ImportSshHosts(ctx, "hosts.csv", int64(len(csvPayload)), strings.NewReader(csvPayload))
	if err != nil {
		t.Fatalf("CSV 导入失败: %v", err)
	}
	if res.Total != 2 || res.Created != 1 || res.Skipped != 1 || res.Invalid != 0 {
		t.Fatalf("CSV 导入统计异常: %+v", res)
	}

	if got := len(hosts.hosts); got != 3 {
		t.Fatalf("导入后记录数 = %d, want 3（预置 1 + JSON 新增 1 + CSV 新增 1）", got)
	}
}

// TestSshHostRejectsInjection 主机/用户名/Host 解析行一律走白名单校验（DES-T-003）。
func TestSshHostRejectsInjection(t *testing.T) {
	svc, _, _, _ := newSecretService(t)
	ctx := context.Background()

	bad := []SshHostInput{
		{Type: domain.SshHostTypeHost, Host: "10.0.0.1; rm -rf /", HostEntries: []string{"10.0.0.1 a"}},
		{Type: domain.SshHostTypeHost, Host: "ok.example.com", HostEntries: []string{"10.0.0.1 a; reboot"}},
		{Type: domain.SshHostTypeSSH, Host: "ok.example.com", User: "root && shutdown", AuthType: domain.SshAuthTypePassword},
		{Type: domain.SshHostTypeSSH, Host: "ok.example.com", User: "root", AuthType: "kerberos"},
		{Type: "unknown", Host: "ok.example.com"},
	}
	for i, in := range bad {
		if _, err := svc.CreateSshHost(ctx, in); !errors.Is(err, errcode.ErrParamFmt) && !errors.Is(err, errcode.ErrParamMiss) {
			t.Fatalf("用例 %d 应被拒绝（10002/10003），got %v", i, err)
		}
	}

	// 非法私钥同样拒绝（避免脏数据导致清理阶段解析失败）。
	if _, err := svc.CreateSshHost(ctx, SshHostInput{
		Type: domain.SshHostTypeSSH, Host: "ok.example.com", User: "root",
		AuthType: domain.SshAuthTypeKey, Key: "-----BEGIN PRIVATE KEY-----\nnot-base64\n-----END PRIVATE KEY-----",
	}); !errors.Is(err, errcode.ErrParamFmt) {
		t.Fatalf("非法私钥应返回 10003，got %v", err)
	}

	// 文件类型不允许 → 24002（契约 §2.7 沿用报告模块码值）。
	if _, err := svc.ImportSshHosts(ctx, "hosts.txt", 8, strings.NewReader("plain txt")); !errors.Is(err, ErrUploadTypeNotAllowed) {
		t.Fatalf("不支持的导入文件类型应返回 24002，got %v", err)
	}
}

// TestEnsureBuiltinCertificateIdempotent 内置证书初始化幂等（重复调用不产生重复记录）。
func TestEnsureBuiltinCertificateIdempotent(t *testing.T) {
	svc, certs, _, _ := newSecretService(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if err := svc.EnsureBuiltinCertificate(ctx); err != nil {
			t.Fatalf("第 %d 次初始化内置证书失败: %v", i+1, err)
		}
	}
	if len(certs.certs) != 1 {
		t.Fatalf("内置证书记录数 = %d, want 1", len(certs.certs))
	}
	builtin := mustFindBuiltinCert(t, certs)
	if !builtin.IsBuiltin || builtin.Name != builtinCertName {
		t.Fatalf("内置证书字段异常: %+v", builtin)
	}
	if len(builtin.KeyEnc) == 0 || len(builtin.CertEnc) == 0 {
		t.Fatal("内置证书应包含（加密的）证书与私钥")
	}
}

// TestLoadKEKFromFileAndFallback KEK 加载：文件优先、权限/内容校验、缺失时派生兜底。
func TestLoadKEKFromFileAndFallback(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/kek.key"

	raw, err := crypto.NewKEK()
	if err != nil {
		t.Fatalf("生成 KEK 失败: %v", err)
	}
	if err := writeFile0600(path, raw); err != nil {
		t.Fatalf("写入 KEK 文件失败: %v", err)
	}

	key, source, err := LoadKEK(path, "ignored", "fallback")
	if err != nil {
		t.Fatalf("从文件加载 KEK 失败: %v", err)
	}
	if !bytes.Equal(key, raw) || !strings.HasPrefix(source, "file:") {
		t.Fatalf("KEK 文件加载结果异常: source=%s", source)
	}

	// 文件内容非法 → 失败关闭（不静默降级）。
	if err := writeFile0600(path, []byte("too-short")); err != nil {
		t.Fatalf("写入非法 KEK 失败: %v", err)
	}
	if _, _, err := LoadKEK(path, "env-secret", "fallback"); err == nil {
		t.Fatal("非法 KEK 文件应返回错误")
	}

	// 文件不存在 → 派生兜底（环境变量优先，其次 JWT secret）。
	missing := dir + "/missing.key"
	envKey, envSource, err := LoadKEK(missing, "env-secret", "fallback")
	if err != nil || envSource != "env" || len(envKey) != crypto.KeySize {
		t.Fatalf("环境变量兜底异常: source=%s err=%v", envSource, err)
	}
	derived, derivedSource, err := LoadKEK(missing, "", "fallback")
	if err != nil || derivedSource != "derived-from-jwt-secret" || len(derived) != crypto.KeySize {
		t.Fatalf("派生兜底异常: source=%s err=%v", derivedSource, err)
	}
	if bytes.Equal(envKey, derived) {
		t.Fatal("不同来源不应派生出相同 KEK")
	}
}
