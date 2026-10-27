// Package setting 系统设置业务服务（管理员/角色/证书/SSH-Host）。
//
// 本文件实现证书管理（Phase 7 任务 7.1）：对齐 PRD §4.12、api-contract §2.7；
// 私钥/证书内容一律 AES-256-GCM 信封加密落库（DES-I-002/DES-I-008），响应脱敏。
package setting

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"auto_performance/internal/crypto"
	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

const (
	// CertExpiringWindow 证书「即将过期」判定窗口（30 天内到期）。
	CertExpiringWindow = 30 * 24 * time.Hour
	// builtinCertName 内置自签证书展示名。
	builtinCertName = "内置自签证书"
	// builtinCertIssuer 内置自签证书颁发者。
	builtinCertIssuer = "auto_performance self-signed"
	// builtinCertValidity 内置自签证书有效期（重新初始化时按此续期）。
	builtinCertValidity = 365 * 24 * time.Hour
	// certDateLayout 证书有效期对外格式（YYYY-MM-DD）。
	certDateLayout = "2006-01-02"
)

// CertRepo 证书仓储能力（由 repository.PGRepository 实现）。
//
// 仓储只存密文：CertEnc/KeyEnc 由服务层用 KEK 信封加密后写入，仓储不接触明文。
type CertRepo interface {
	ListCertificates(ctx context.Context) ([]domain.Certificate, error)
	GetCertificate(ctx context.Context, certID int64) (*domain.Certificate, error)
	CreateCertificate(ctx context.Context, cert *domain.Certificate) error
	SoftDeleteCertificate(ctx context.Context, certID int64) error
	// UpdateCertificateValidity 重新初始化：仅更新有效期与状态，不动证书/私钥内容。
	UpdateCertificateValidity(ctx context.Context, certID int64, validFrom, validTo time.Time, status string) error
	CountBuiltinCertificates(ctx context.Context) (int64, error)
	CertificateNameExists(ctx context.Context, name string) (bool, error)
}

// CertificateView 证书视图（脱敏：不含证书内容与私钥，仅暴露 HasKey）。
type CertificateView struct {
	ID                int64
	Name              string
	Issuer            string
	ValidFrom         *time.Time
	ValidTo           *time.Time
	AssociatedService string
	IsBuiltin         bool
	Status            string // valid | expiring | expired（按有效期推导）
	HasKey            bool
	Version           int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// CertificatePage 证书分页结果（对齐 api-contract §1.3）。
type CertificatePage struct {
	List     []CertificateView
	Total    int64
	Page     int
	PageSize int
}

// CertificateInput 证书导入输入（白名单字段；cert/key 为文件内容）。
type CertificateInput struct {
	Name              string
	Issuer            string
	ValidFrom         string // YYYY-MM-DD（缺省时从证书内容解析）
	ValidTo           string // YYYY-MM-DD（缺省时从证书内容解析）
	AssociatedService string
	CertPEM           []byte
	KeyPEM            []byte
}

// ListCertificates 证书列表（确保内置自签证书存在后返回，状态按有效期实时推导）。
func (s *Service) ListCertificates(ctx context.Context, page, pageSize int) (*CertificatePage, error) {
	if s == nil || s.certs == nil {
		return nil, errcode.ErrDependency
	}
	if err := s.EnsureBuiltinCertificate(ctx); err != nil {
		return nil, err
	}

	records, err := s.certs.ListCertificates(ctx)
	if err != nil {
		return nil, err
	}

	page, pageSize = normalizePaging(page, pageSize)
	now := s.now()
	list := make([]CertificateView, 0, len(records))
	for i := range records {
		list = append(list, s.toCertificateView(&records[i], now))
	}

	total := int64(len(list))
	start := (page - 1) * pageSize
	if start > len(list) {
		start = len(list)
	}
	end := start + pageSize
	if end > len(list) {
		end = len(list)
	}
	return &CertificatePage{List: list[start:end], Total: total, Page: page, PageSize: pageSize}, nil
}

// CreateCertificate 导入证书（证书内容与私钥信封加密落库，名称重复 → 10302）。
func (s *Service) CreateCertificate(ctx context.Context, in CertificateInput) (*CertificateView, error) {
	if s == nil || s.certs == nil {
		return nil, errcode.ErrDependency
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errcode.ErrParamMiss
	}
	if len(name) > 128 {
		return nil, errcode.ErrParamFmt
	}
	if len(in.CertPEM) == 0 {
		return nil, errcode.ErrParamMiss
	}

	// 证书内容必须是合法 X.509 证书（同时用于有效期缺省值回填，避免脏数据入库）。
	certFrom, certTo, err := parseCertificateValidity(in.CertPEM)
	if err != nil {
		return nil, errcode.ErrParamFmt
	}

	validFrom, err := parseCertDate(in.ValidFrom, certFrom)
	if err != nil {
		return nil, err
	}
	validTo, err := parseCertDate(in.ValidTo, certTo)
	if err != nil {
		return nil, err
	}
	if validTo.Before(validFrom) {
		return nil, errcode.ErrParamFmt
	}

	if in.KeyPEM != nil {
		if err := validatePrivateKeyPEM(in.KeyPEM); err != nil {
			return nil, errcode.ErrParamFmt
		}
	}

	exists, err := s.certs.CertificateNameExists(ctx, name)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, errcode.ErrConflict
	}

	certEnc, err := s.encryptSecret(in.CertPEM)
	if err != nil {
		return nil, err
	}
	var keyEnc []byte
	if len(in.KeyPEM) > 0 {
		if keyEnc, err = s.encryptSecret(in.KeyPEM); err != nil {
			return nil, err
		}
	}

	status := deriveCertStatus(&validTo, s.now())
	rec := &domain.Certificate{
		Name:              name,
		Issuer:            strings.TrimSpace(in.Issuer),
		ValidFrom:         &validFrom,
		ValidTo:           &validTo,
		CertEnc:           certEnc,
		KeyEnc:            keyEnc,
		IsBuiltin:         false, // 导入证书永远不是内置自签证书（防伪造内置标记）
		Status:            status,
		AssociatedService: strings.TrimSpace(in.AssociatedService),
	}
	if err := s.certs.CreateCertificate(ctx, rec); err != nil {
		return nil, err
	}
	view := s.toCertificateView(rec, s.now())
	return &view, nil
}

// DeleteCertificate 删除证书；内置自签证书不可删除（26004）。
func (s *Service) DeleteCertificate(ctx context.Context, certID int64) error {
	if s == nil || s.certs == nil {
		return errcode.ErrDependency
	}
	if certID <= 0 {
		return errcode.ErrParamFmt
	}
	rec, err := s.certs.GetCertificate(ctx, certID)
	if err != nil {
		return err
	}
	if rec.IsBuiltinCert() {
		return ErrBuiltinCertProtected
	}
	return s.certs.SoftDeleteCertificate(ctx, certID)
}

// ReinitCertificate 重新初始化内置自签证书：仅更新有效期（非内置 → 10401）。
func (s *Service) ReinitCertificate(ctx context.Context, certID int64) (*CertificateView, error) {
	if s == nil || s.certs == nil {
		return nil, errcode.ErrDependency
	}
	if certID <= 0 {
		return nil, errcode.ErrParamFmt
	}

	if err := s.EnsureBuiltinCertificate(ctx); err != nil {
		return nil, err
	}
	rec, err := s.certs.GetCertificate(ctx, certID)
	if err != nil {
		return nil, err
	}
	if !rec.IsBuiltinCert() {
		return nil, errcode.ErrState
	}

	now := s.now()
	validFrom := truncateToDay(now)
	validTo := truncateToDay(now.Add(builtinCertValidity))
	status := deriveCertStatus(&validTo, now)
	if err := s.certs.UpdateCertificateValidity(ctx, certID, validFrom, validTo, status); err != nil {
		return nil, err
	}

	rec.ValidFrom, rec.ValidTo, rec.Status = &validFrom, &validTo, status
	view := s.toCertificateView(rec, now)
	return &view, nil
}

// EnsureBuiltinCertificate 确保系统内置一份自签证书存在（不可删除，可重新初始化）。
//
// 内置证书的私钥同样信封加密落库（DES-I-002），密钥材料不落日志、不出接口。
func (s *Service) EnsureBuiltinCertificate(ctx context.Context) error {
	if s == nil || s.certs == nil {
		return errcode.ErrDependency
	}
	count, err := s.certs.CountBuiltinCertificates(ctx)
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}

	now := s.now()
	certPEM, keyPEM, err := generateSelfSignedCert(builtinCertName, now, now.Add(builtinCertValidity))
	if err != nil {
		return fmt.Errorf("生成内置自签证书失败: %w", err)
	}
	certEnc, err := s.encryptSecret(certPEM)
	if err != nil {
		return err
	}
	keyEnc, err := s.encryptSecret(keyPEM)
	if err != nil {
		return err
	}

	validFrom := truncateToDay(now)
	validTo := truncateToDay(now.Add(builtinCertValidity))
	rec := &domain.Certificate{
		Name:              builtinCertName,
		Issuer:            builtinCertIssuer,
		ValidFrom:         &validFrom,
		ValidTo:           &validTo,
		CertEnc:           certEnc,
		KeyEnc:            keyEnc,
		IsBuiltin:         true,
		Status:            deriveCertStatus(&validTo, now),
		AssociatedService: "auto_performance",
	}
	return s.certs.CreateCertificate(ctx, rec)
}

// encryptSecret 用 KEK 做信封加密；KEK 不可用时失败关闭（绝不明文落库）。
func (s *Service) encryptSecret(plain []byte) ([]byte, error) {
	if len(s.kek) != crypto.KeySize {
		return nil, fmt.Errorf("证书/SSH 密钥加密不可用: %w", errcode.ErrDependency)
	}
	enc, err := crypto.EncryptSecret(plain, s.kek)
	if err != nil {
		return nil, fmt.Errorf("信封加密失败: %w", err)
	}
	return enc, nil
}

// toCertificateView 领域实体 → 脱敏视图（不含证书内容/私钥）。
func (s *Service) toCertificateView(rec *domain.Certificate, now time.Time) CertificateView {
	return CertificateView{
		ID:                rec.ID,
		Name:              rec.Name,
		Issuer:            rec.Issuer,
		ValidFrom:         rec.ValidFrom,
		ValidTo:           rec.ValidTo,
		AssociatedService: rec.AssociatedService,
		IsBuiltin:         rec.IsBuiltin,
		Status:            deriveCertStatus(rec.ValidTo, now),
		HasKey:            len(rec.KeyEnc) > 0,
		Version:           rec.Version,
		CreatedAt:         rec.CreatedAt,
		UpdatedAt:         rec.UpdatedAt,
	}
}

// deriveCertStatus 按有效期推导证书状态（PRD §3.6.4，单一事实源）。
func deriveCertStatus(validTo *time.Time, now time.Time) string {
	if validTo == nil {
		return domain.CertStatusValid
	}
	today := truncateToDay(now)
	end := truncateToDay(*validTo)
	switch {
	case end.Before(today):
		return domain.CertStatusExpired
	case end.Sub(today) <= CertExpiringWindow:
		return domain.CertStatusExpiring
	default:
		return domain.CertStatusValid
	}
}

// truncateToDay 截断到当天 00:00（UTC 语义，避免时区让「今天到期」判定漂移）。
func truncateToDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// parseCertDate 解析 YYYY-MM-DD 有效期；入参为空时使用 fallback（证书内有效期）。
func parseCertDate(raw string, fallback time.Time) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if fallback.IsZero() {
			return time.Time{}, errcode.ErrParamMiss
		}
		return truncateToDay(fallback), nil
	}
	parsed, err := time.Parse(certDateLayout, raw)
	if err != nil {
		return time.Time{}, errcode.ErrParamFmt
	}
	return parsed, nil
}

// parseCertificateValidity 解析证书 PEM 的有效期（用于校验文件合法性与缺省值回填）。
func parseCertificateValidity(certPEM []byte) (time.Time, time.Time, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return time.Time{}, time.Time{}, errors.New("证书文件不是合法 PEM")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("解析证书失败: %w", err)
	}
	return cert.NotBefore, cert.NotAfter, nil
}

// validatePrivateKeyPEM 校验私钥 PEM 结构（仅接受不加密的 PKCS#8/PKCS#1/EC 私钥）。
func validatePrivateKeyPEM(keyPEM []byte) error {
	block, _ := pem.Decode(keyPEM)
	if block == nil || !strings.Contains(block.Type, "PRIVATE KEY") {
		return errors.New("私钥文件不是合法 PEM")
	}
	// 拒绝口令保护的 PEM（平台不保存口令，无法安全解密使用）。
	if strings.Contains(strings.ToUpper(block.Headers["Proc-Type"]), "ENCRYPTED") {
		return errors.New("不支持口令保护的私钥")
	}
	if _, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		return nil
	}
	if _, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return nil
	}
	if _, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return nil
	}
	return errors.New("私钥格式不受支持")
}

// generateSelfSignedCert 生成自签证书与私钥（ECDSA P-256，crypto/rand 取随机序列号）。
func generateSelfSignedCert(cn string, notBefore, notAfter time.Time) ([]byte, []byte, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("生成私钥失败: %w", err)
	}
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("生成证书序列号失败: %w", err)
	}

	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: cn, Organization: []string{"auto_performance"}},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		DNSNames:              []string{"auto_performance", "localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, fmt.Errorf("签发自签证书失败: %w", err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, nil, fmt.Errorf("序列化私钥失败: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM, nil
}

// Activate 将指定证书（certID<=0 表示内置自签证书）解密并写入服务 TLS 证书/私钥文件。
// 供 `auto_performance tls-init` 子命令与「设为服务证书」接口调用；重启服务后生效。
// 私钥落地权限 0600、目录 0700（DES-I-008）。
func (s *Service) Activate(ctx context.Context, certID int64, certFile, keyFile string) (string, string, error) {
	var rec *domain.Certificate
	var err error
	if certID <= 0 {
		var certs []domain.Certificate
		certs, err = s.certs.ListCertificates(ctx)
		if err != nil {
			return "", "", err
		}
		for i := range certs {
			if certs[i].IsBuiltin {
				rec = &certs[i]
				break
			}
		}
		if rec == nil {
			return "", "", errcode.ErrNotFound
		}
	} else {
		rec, err = s.certs.GetCertificate(ctx, certID)
		if err != nil {
			return "", "", err
		}
	}
	if len(rec.CertEnc) == 0 || len(rec.KeyEnc) == 0 {
		// 无配对私钥的证书不能用于 TLS。
		return "", "", errcode.ErrParamMiss
	}
	certPEM, err := crypto.DecryptSecret(rec.CertEnc, s.kek)
	if err != nil {
		return "", "", fmt.Errorf("解密证书失败: %w", err)
	}
	keyPEM, err := crypto.DecryptSecret(rec.KeyEnc, s.kek)
	if err != nil {
		return "", "", fmt.Errorf("解密私钥失败: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(certFile), 0o700); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(keyFile), 0o700); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		return "", "", err
	}
	return certFile, keyFile, nil
}
