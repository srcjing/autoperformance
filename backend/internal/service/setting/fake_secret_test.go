package setting

import (
	"context"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"auto_performance/internal/crypto"
	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// fakeCertRepo 内存证书仓储（验证密文落库，不接触真实 DB）。
type fakeCertRepo struct {
	certs   map[int64]*domain.Certificate
	nextID  int64
	deleted []int64

	createCalls         int
	updateValidityCalls int
	lastCreated         *domain.Certificate
}

func newFakeCertRepo() *fakeCertRepo {
	return &fakeCertRepo{certs: map[int64]*domain.Certificate{}, nextID: 500}
}

func (f *fakeCertRepo) ListCertificates(_ context.Context) ([]domain.Certificate, error) {
	out := make([]domain.Certificate, 0, len(f.certs))
	for _, c := range f.certs {
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsBuiltin != out[j].IsBuiltin {
			return out[i].IsBuiltin
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (f *fakeCertRepo) GetCertificate(_ context.Context, certID int64) (*domain.Certificate, error) {
	c, ok := f.certs[certID]
	if !ok {
		return nil, errcode.ErrNotFound
	}
	cp := *c
	return &cp, nil
}

func (f *fakeCertRepo) CreateCertificate(_ context.Context, cert *domain.Certificate) error {
	for _, c := range f.certs {
		if c.Name == cert.Name {
			return errcode.ErrConflict
		}
	}
	f.createCalls++
	f.nextID++
	now := time.Now().UTC()
	cert.ID = f.nextID
	cert.Version = 1
	cert.CreatedAt, cert.UpdatedAt = now, now

	cp := *cert
	f.certs[cert.ID] = &cp
	f.lastCreated = &cp
	return nil
}

func (f *fakeCertRepo) SoftDeleteCertificate(_ context.Context, certID int64) error {
	if _, ok := f.certs[certID]; !ok {
		return errcode.ErrNotFound
	}
	f.deleted = append(f.deleted, certID)
	delete(f.certs, certID)
	return nil
}

func (f *fakeCertRepo) UpdateCertificateValidity(_ context.Context, certID int64, validFrom, validTo time.Time, status string) error {
	c, ok := f.certs[certID]
	if !ok {
		return errcode.ErrNotFound
	}
	f.updateValidityCalls++
	c.ValidFrom, c.ValidTo, c.Status = &validFrom, &validTo, status
	c.Version++
	return nil
}

func (f *fakeCertRepo) CountBuiltinCertificates(_ context.Context) (int64, error) {
	var n int64
	for _, c := range f.certs {
		if c.IsBuiltin {
			n++
		}
	}
	return n, nil
}

func (f *fakeCertRepo) CertificateNameExists(_ context.Context, name string) (bool, error) {
	for _, c := range f.certs {
		if c.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// fakeSshHostRepo 内存 SSH/Host 仓储（模拟环境引用计数与去重）。
type fakeSshHostRepo struct {
	hosts    map[int64]*domain.SshHost
	nextID   int64
	deleted  []int64
	envRefs  map[int64]int64 // ssh_host_id → 环境引用数
	created  []*domain.SshHost
	updated  []int64
	versions map[int64]int64
}

func newFakeSshHostRepo() *fakeSshHostRepo {
	return &fakeSshHostRepo{
		hosts:    map[int64]*domain.SshHost{},
		nextID:   700,
		envRefs:  map[int64]int64{},
		versions: map[int64]int64{},
	}
}

func (f *fakeSshHostRepo) ListSshHosts(_ context.Context, keyword, typ string, page, pageSize int) ([]domain.SshHost, int64, error) {
	all := make([]domain.SshHost, 0, len(f.hosts))
	for _, h := range f.hosts {
		if keyword != "" && !strings.Contains(h.Host, keyword) && !strings.Contains(h.Description, keyword) {
			continue
		}
		if typ != "" && h.Type != typ {
			continue
		}
		all = append(all, *h)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].ID < all[j].ID })

	total := int64(len(all))
	start := (page - 1) * pageSize
	if start > len(all) {
		start = len(all)
	}
	end := start + pageSize
	if end > len(all) {
		end = len(all)
	}
	return all[start:end], total, nil
}

func (f *fakeSshHostRepo) GetSshHost(_ context.Context, sshHostID int64) (*domain.SshHost, error) {
	h, ok := f.hosts[sshHostID]
	if !ok {
		return nil, errcode.ErrNotFound
	}
	cp := *h
	cp.HostEntries = append([]string(nil), h.HostEntries...)
	cp.KeyEnc = append([]byte(nil), h.KeyEnc...)
	return &cp, nil
}

func (f *fakeSshHostRepo) CreateSshHost(_ context.Context, host *domain.SshHost) error {
	for _, h := range f.hosts {
		if h.Type == host.Type && h.Host == host.Host && h.Port == host.Port {
			return errcode.ErrConflict
		}
	}
	now := time.Now().UTC()
	f.nextID++
	host.ID = f.nextID
	host.Version = 1
	host.CreatedAt, host.UpdatedAt = now, now

	cp := *host
	cp.HostEntries = append([]string(nil), host.HostEntries...)
	cp.KeyEnc = append([]byte(nil), host.KeyEnc...)
	f.hosts[host.ID] = &cp
	f.created = append(f.created, &cp)
	return nil
}

func (f *fakeSshHostRepo) UpdateSshHost(_ context.Context, host *domain.SshHost, version int64) error {
	current, ok := f.hosts[host.ID]
	if !ok {
		return errcode.ErrNotFound
	}
	if version > 0 && current.Version != version {
		return errcode.ErrConcurrent
	}
	f.updated = append(f.updated, host.ID)

	cp := *host
	cp.HostEntries = append([]string(nil), host.HostEntries...)
	cp.KeyEnc = append([]byte(nil), host.KeyEnc...)
	cp.Version = current.Version + 1
	cp.CreatedAt = current.CreatedAt
	cp.UpdatedAt = time.Now().UTC()
	f.hosts[host.ID] = &cp
	return nil
}

func (f *fakeSshHostRepo) SoftDeleteSshHost(_ context.Context, sshHostID int64) error {
	if _, ok := f.hosts[sshHostID]; !ok {
		return errcode.ErrNotFound
	}
	f.deleted = append(f.deleted, sshHostID)
	delete(f.hosts, sshHostID)
	return nil
}

func (f *fakeSshHostRepo) CountEnvironmentsBySshHost(_ context.Context, sshHostID int64) (int64, error) {
	return f.envRefs[sshHostID], nil
}

func (f *fakeSshHostRepo) SshHostExists(_ context.Context, typ, host string, port int) (bool, error) {
	for _, h := range f.hosts {
		if h.Type == typ && h.Host == host && h.Port == port {
			return true, nil
		}
	}
	return false, nil
}

// testKEK 单元测试 KEK（32 字节，crypto/rand 生成，避免硬编码密钥）。
func testKEK(t *testing.T) []byte {
	t.Helper()
	kek, err := crypto.NewKEK()
	if err != nil {
		t.Fatalf("生成测试 KEK 失败: %v", err)
	}
	return kek
}

// writeFile0600 以 0600 权限写文件（KEK 文件权限要求）。
func writeFile0600(path string, content []byte) error {
	return os.WriteFile(path, content, 0o600)
}

// newSecretService 构造注入了证书/SSH 仓储与 KEK 的系统设置服务。
func newSecretService(t *testing.T) (*Service, *fakeCertRepo, *fakeSshHostRepo, []byte) {
	t.Helper()
	repo := newFakeRepo()
	certs := newFakeCertRepo()
	hosts := newFakeSshHostRepo()
	kek := testKEK(t)
	svc := New(repo, &fakeInvalidator{}).WithSecrets(certs, hosts, kek)
	return svc, certs, hosts, kek
}

// testCertPEM 生成测试用自签证书与私钥（含随机序列号，避免固定密钥材料）。
func testCertPEM(t *testing.T, cn string) ([]byte, []byte) {
	t.Helper()
	now := time.Now().UTC()
	certPEM, keyPEM, err := generateSelfSignedCert(cn, now, now.Add(365*24*time.Hour))
	if err != nil {
		t.Fatalf("生成测试证书失败: %v", err)
	}
	return certPEM, keyPEM
}

// mustFindBuiltinCert 返回仓储中唯一的内置证书。
func mustFindBuiltinCert(t *testing.T, repo *fakeCertRepo) *domain.Certificate {
	t.Helper()
	for _, c := range repo.certs {
		if c.IsBuiltin {
			return c
		}
	}
	t.Fatal("未找到内置自签证书")
	return nil
}
