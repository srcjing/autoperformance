// Package product 产品管理业务服务（产品/接口/阈值 CRUD）。
//
// 对齐 PRD §3.2/§4.4~§4.6/§9、api-contract §2.3 与不变量：
//   - INV-2：阈值「指标 ↔ 方向」非法组合一律拦截（22003）；
//   - INV-6：产品下存在接口时禁止删除（22002），全部删除为逻辑删除；
//   - INV-7：接口认证头（Authorization/Token 等）AES-256-GCM 加密存储，响应/日志不返回明文。
package product

import (
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// DefaultPageSize 列表默认页大小；MaxPageSize 分页上限（api-contract §1.3）。
const (
	DefaultPageSize = 20
	MaxPageSize     = 200
)

// 字段长度上限（保护请求体与展示，超限 → 10003）。
const (
	maxNameLen       = 128
	maxCodeLen       = 64
	maxVersionLen    = 32
	maxPlatformLen   = 64
	maxDescription   = 500
	maxURLLen        = 2048
	maxBodyLen       = 64 * 1024
	maxHeaderCount   = 50
	maxHeaderNameLen = 128
	maxHeaderValLen  = 4096
	maxTimeoutMS     = 600000
	maxUnitLen       = 16
)

// Repo 产品管理仓储能力（由 repository.PGRepository 实现）。
type Repo interface {
	// ===== 产品 =====
	ListProducts(ctx context.Context, q domain.ProductQuery) ([]domain.Product, int64, error)
	GetProduct(ctx context.Context, productID int64) (*domain.Product, error)
	CreateProduct(ctx context.Context, product *domain.Product) error
	UpdateProduct(ctx context.Context, product *domain.Product, version int64) error
	UpdateProductStatus(ctx context.Context, productID int64, status string) error
	SoftDeleteProduct(ctx context.Context, productID int64) error
	CountInterfacesByProduct(ctx context.Context, productID int64) (int64, error)

	// ===== 接口 =====
	ListInterfaces(ctx context.Context, q domain.InterfaceQuery) ([]domain.Interface, int64, error)
	GetInterface(ctx context.Context, interfaceID int64) (*domain.Interface, error)
	CreateInterface(ctx context.Context, api *domain.Interface) error
	UpdateInterface(ctx context.Context, api *domain.Interface, version int64) error
	UpdateInterfaceStatus(ctx context.Context, interfaceID int64, status string) error
	SoftDeleteInterface(ctx context.Context, interfaceID int64) error

	// ===== 阈值 =====
	ListThresholds(ctx context.Context, interfaceID int64) ([]domain.Threshold, error)
	ReplaceThresholds(ctx context.Context, interfaceID int64, items []domain.Threshold) error
}

// Service 产品管理业务服务。
type Service struct {
	repo Repo
	// headerKey 接口认证头加密密钥（AES-256 32 字节，DES-I-001）；为空时加密类操作返回 10602。
	headerKey []byte
}

// New 构造产品管理服务；headerKey 为认证头加密密钥（DeriveHeaderKey 派生）。
func New(repo Repo, headerKey []byte) *Service {
	return &Service{repo: repo, headerKey: headerKey}
}

// DeriveHeaderKey 派生接口认证头加密密钥（对齐 DES-I-008：密钥不硬编码、来源可轮换）。
//
// 优先级：显式密钥 > security.secret_key_env 指向的环境变量 > JWT secret 派生。
// 返回值固定 32 字节，可直接作为 AES-256-GCM 密钥。
func DeriveHeaderKey(explicit, envSecret, fallbackSecret string) []byte {
	var material string
	switch {
	case explicit != "":
		material = "explicit|" + explicit
	case envSecret != "":
		material = "env|" + envSecret
	default:
		material = "derived|" + fallbackSecret
	}
	sum := sha256.Sum256([]byte("auto_performance/interface-header/v1|" + material))
	return sum[:]
}

// ===================== 产品 =====================

// ProductInput 产品新建/修改输入（白名单字段；status 只能经状态接口变更）。
type ProductInput struct {
	Name           string
	Code           string
	ProductVersion string
	Platform       string
	Description    string
	Version        int64 // 乐观锁版本号（修改时必填）
}

// ProductView 产品视图。
type ProductView struct {
	ID             int64
	Name           string
	Code           string
	ProductVersion string
	Platform       string
	Description    string
	Status         string
	InterfaceCount int64
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ProductPage 产品分页结果（对齐 api-contract §1.3）。
type ProductPage struct {
	List     []ProductView
	Total    int64
	Page     int
	PageSize int
}

// ListProductsQuery 产品列表查询条件。
type ListProductsQuery struct {
	Keyword        string
	ProductVersion string
	Platform       string
	Status         string
	Page           int
	PageSize       int
}

// normalizePaging 归一化分页参数（默认 20，上限 200）。
func normalizePaging(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}
	return page, pageSize
}

// ListProducts 产品列表（关键字/版本/平台/状态组合筛选 + 分页）。
func (s *Service) ListProducts(ctx context.Context, q ListProductsQuery) (*ProductPage, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	status := strings.TrimSpace(q.Status)
	if status != "" && !domain.IsValidProductStatus(status) {
		return nil, errcode.ErrParamFmt
	}

	page, pageSize := normalizePaging(q.Page, q.PageSize)
	products, total, err := s.repo.ListProducts(ctx, domain.ProductQuery{
		Keyword:        strings.TrimSpace(q.Keyword),
		ProductVersion: strings.TrimSpace(q.ProductVersion),
		Platform:       strings.TrimSpace(q.Platform),
		Status:         status,
		Page:           page,
		PageSize:       pageSize,
	})
	if err != nil {
		return nil, err
	}

	list := make([]ProductView, 0, len(products))
	for i := range products {
		list = append(list, toProductView(&products[i]))
	}
	return &ProductPage{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// GetProduct 产品详情（不存在 → 10301）。
func (s *Service) GetProduct(ctx context.Context, productID int64) (*ProductView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if productID <= 0 {
		return nil, errcode.ErrParamFmt
	}
	p, err := s.repo.GetProduct(ctx, productID)
	if err != nil {
		return nil, err
	}
	view := toProductView(p)
	return &view, nil
}

// CreateProduct 创建产品（编码唯一 → 22001；初始状态 enabled）。
func (s *Service) CreateProduct(ctx context.Context, in ProductInput) (*ProductView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	p, err := validateProductInput(in)
	if err != nil {
		return nil, err
	}
	p.Status = domain.ProductStatusEnabled // 状态只能由状态接口变更，不接受请求体指定

	if err := s.repo.CreateProduct(ctx, p); err != nil {
		if errors.Is(err, errcode.ErrConflict) {
			return nil, ErrCodeExists
		}
		return nil, err
	}
	return s.GetProduct(ctx, p.ID)
}

// UpdateProduct 全量修改产品（version 乐观锁 → 10402；编码重复 → 22001）。
func (s *Service) UpdateProduct(ctx context.Context, productID int64, in ProductInput) (*ProductView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if productID <= 0 {
		return nil, errcode.ErrParamFmt
	}
	if in.Version <= 0 {
		return nil, errcode.ErrParamMiss // 全量修改必须携带 version（api-contract §2.3）
	}
	if _, err := s.repo.GetProduct(ctx, productID); err != nil {
		return nil, err
	}

	p, err := validateProductInput(in)
	if err != nil {
		return nil, err
	}
	p.ID = productID
	if err := s.repo.UpdateProduct(ctx, p, in.Version); err != nil {
		if errors.Is(err, errcode.ErrConflict) {
			return nil, ErrCodeExists
		}
		return nil, err
	}
	return s.GetProduct(ctx, productID)
}

// DeleteProduct 逻辑删除产品；产品下存在接口时禁止删除（22002，INV-6）。
func (s *Service) DeleteProduct(ctx context.Context, productID int64) error {
	if s == nil || s.repo == nil {
		return errcode.ErrDependency
	}
	if productID <= 0 {
		return errcode.ErrParamFmt
	}
	if _, err := s.repo.GetProduct(ctx, productID); err != nil {
		return err
	}
	refs, err := s.repo.CountInterfacesByProduct(ctx, productID)
	if err != nil {
		return err
	}
	if refs > 0 {
		return ErrProductHasInterfaces
	}
	if err := s.repo.SoftDeleteProduct(ctx, productID); err != nil {
		// 并发新增接口的兜底命中（DB 侧 NOT EXISTS 条件）。
		if errors.Is(err, errcode.ErrReferenced) {
			return ErrProductHasInterfaces
		}
		return err
	}
	return nil
}

// UpdateStatus 停用/启用产品（enabled ↔ disabled；同状态 → 10401）。
func (s *Service) UpdateStatus(ctx context.Context, productID int64, status string) error {
	if s == nil || s.repo == nil {
		return errcode.ErrDependency
	}
	if productID <= 0 {
		return errcode.ErrParamFmt
	}
	status = strings.TrimSpace(status)
	if !domain.IsValidProductStatus(status) {
		return errcode.ErrParamFmt
	}

	current, err := s.repo.GetProduct(ctx, productID)
	if err != nil {
		return err
	}
	if !domain.CanTransitProductStatus(current.Status, status) {
		return errcode.ErrState
	}
	return s.repo.UpdateProductStatus(ctx, productID, status)
}

// validateProductInput 校验并归一化产品输入。
func validateProductInput(in ProductInput) (*domain.Product, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errcode.ErrParamMiss
	}
	if utf8.RuneCountInString(name) > maxNameLen {
		return nil, errcode.ErrParamFmt
	}

	code := strings.TrimSpace(in.Code)
	if code == "" {
		return nil, errcode.ErrParamMiss
	}
	if utf8.RuneCountInString(code) > maxCodeLen {
		return nil, errcode.ErrParamFmt
	}

	productVersion := strings.TrimSpace(in.ProductVersion)
	if productVersion == "" {
		return nil, errcode.ErrParamMiss // 版本必填（PRD §4.4）
	}
	if utf8.RuneCountInString(productVersion) > maxVersionLen {
		return nil, errcode.ErrParamFmt
	}

	platform := strings.TrimSpace(in.Platform)
	if utf8.RuneCountInString(platform) > maxPlatformLen {
		return nil, errcode.ErrParamFmt
	}
	description := strings.TrimSpace(in.Description)
	if utf8.RuneCountInString(description) > maxDescription {
		return nil, errcode.ErrParamFmt
	}

	return &domain.Product{
		Name:           name,
		Code:           code,
		ProductVersion: productVersion,
		Platform:       platform,
		Description:    description,
	}, nil
}

// toProductView 领域实体 → 视图。
func toProductView(p *domain.Product) ProductView {
	return ProductView{
		ID:             p.ID,
		Name:           p.Name,
		Code:           p.Code,
		ProductVersion: p.ProductVersion,
		Platform:       p.Platform,
		Description:    p.Description,
		Status:         p.Status,
		InterfaceCount: p.InterfaceCount,
		Version:        p.Version,
		CreatedAt:      p.CreatedAt,
		UpdatedAt:      p.UpdatedAt,
	}
}
