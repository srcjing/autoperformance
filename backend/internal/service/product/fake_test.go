package product

import (
	"context"
	"sort"
	"strings"
	"time"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// fakeRepo 内存产品仓储（无 DB 依赖）：实现 Repo 全量方法，
// 供 service 层单测验证校验逻辑/引用保护/乐观锁/认证头加密落库语义。
type fakeRepo struct {
	products   map[int64]*domain.Product
	interfaces map[int64]*domain.Interface
	thresholds map[int64][]domain.Threshold

	nextProduct   int64
	nextInterface int64
	nextThreshold int64

	deletedProducts   []int64
	deletedInterfaces []int64
	updateCalls       int
	statusCalls       map[int64]string
	thresholdWrite    int
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		products:      map[int64]*domain.Product{},
		interfaces:    map[int64]*domain.Interface{},
		thresholds:    map[int64][]domain.Threshold{},
		nextProduct:   100,
		nextInterface: 200,
		nextThreshold: 300,
		statusCalls:   map[int64]string{},
	}
}

// ===================== 产品 =====================

func (f *fakeRepo) ListProducts(_ context.Context, q domain.ProductQuery) ([]domain.Product, int64, error) {
	matched := make([]domain.Product, 0, len(f.products))
	for _, p := range f.products {
		if q.Keyword != "" && !strings.Contains(p.Name, q.Keyword) && !strings.Contains(p.Code, q.Keyword) {
			continue
		}
		if q.ProductVersion != "" && p.ProductVersion != q.ProductVersion {
			continue
		}
		if q.Platform != "" && p.Platform != q.Platform {
			continue
		}
		if q.Status != "" && p.Status != q.Status {
			continue
		}
		cp := *p
		cp.InterfaceCount = f.interfaceCount(p.ID)
		matched = append(matched, cp)
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].ID < matched[j].ID })
	return paginate(matched, q.Page, q.PageSize), int64(len(matched)), nil
}

func (f *fakeRepo) GetProduct(_ context.Context, productID int64) (*domain.Product, error) {
	p, ok := f.products[productID]
	if !ok {
		return nil, errcode.ErrNotFound
	}
	cp := *p
	cp.InterfaceCount = f.interfaceCount(productID)
	return &cp, nil
}

func (f *fakeRepo) CreateProduct(_ context.Context, p *domain.Product) error {
	for _, other := range f.products {
		if other.Code == p.Code {
			return errcode.ErrConflict
		}
	}
	f.nextProduct++
	p.ID = f.nextProduct
	p.Version = 1
	p.CreatedAt = time.Now()
	p.UpdatedAt = p.CreatedAt
	cp := *p
	f.products[p.ID] = &cp
	return nil
}

func (f *fakeRepo) UpdateProduct(_ context.Context, p *domain.Product, version int64) error {
	cur, ok := f.products[p.ID]
	if !ok {
		return errcode.ErrNotFound
	}
	if cur.Version != version {
		return errcode.ErrConcurrent
	}
	for id, other := range f.products {
		if id != p.ID && other.Code == p.Code {
			return errcode.ErrConflict
		}
	}
	f.updateCalls++
	cur.Name, cur.Code = p.Name, p.Code
	cur.ProductVersion, cur.Platform, cur.Description = p.ProductVersion, p.Platform, p.Description
	cur.Version++
	cur.UpdatedAt = time.Now()
	p.Version = cur.Version
	return nil
}

func (f *fakeRepo) UpdateProductStatus(_ context.Context, productID int64, status string) error {
	p, ok := f.products[productID]
	if !ok {
		return errcode.ErrNotFound
	}
	p.Status = status
	p.Version++
	p.UpdatedAt = time.Now()
	f.statusCalls[productID] = status
	return nil
}

func (f *fakeRepo) SoftDeleteProduct(_ context.Context, productID int64) error {
	if _, ok := f.products[productID]; !ok {
		return errcode.ErrNotFound
	}
	if f.interfaceCount(productID) > 0 {
		return errcode.ErrReferenced
	}
	f.deletedProducts = append(f.deletedProducts, productID)
	delete(f.products, productID)
	return nil
}

func (f *fakeRepo) CountInterfacesByProduct(_ context.Context, productID int64) (int64, error) {
	return f.interfaceCount(productID), nil
}

func (f *fakeRepo) interfaceCount(productID int64) int64 {
	var n int64
	for _, api := range f.interfaces {
		if api.ProductID == productID {
			n++
		}
	}
	return n
}

// ===================== 接口 =====================

func (f *fakeRepo) ListInterfaces(_ context.Context, q domain.InterfaceQuery) ([]domain.Interface, int64, error) {
	matched := make([]domain.Interface, 0, len(f.interfaces))
	for _, api := range f.interfaces {
		if api.ProductID != q.ProductID {
			continue
		}
		if q.Keyword != "" && !strings.Contains(api.Name, q.Keyword) && !strings.Contains(api.URL, q.Keyword) {
			continue
		}
		if q.Status != "" && api.Status != q.Status {
			continue
		}
		matched = append(matched, *api)
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].ID < matched[j].ID })
	return paginate(matched, q.Page, q.PageSize), int64(len(matched)), nil
}

func (f *fakeRepo) GetInterface(_ context.Context, interfaceID int64) (*domain.Interface, error) {
	api, ok := f.interfaces[interfaceID]
	if !ok {
		return nil, errcode.ErrNotFound
	}
	cp := *api
	cp.Headers = copyHeaders(api.Headers)
	cp.HeadersEnc = append([]byte(nil), api.HeadersEnc...)
	return &cp, nil
}

func (f *fakeRepo) CreateInterface(_ context.Context, api *domain.Interface) error {
	if _, ok := f.products[api.ProductID]; !ok {
		return errcode.ErrNotFound
	}
	f.nextInterface++
	api.ID = f.nextInterface
	api.Version = 1
	api.CreatedAt = time.Now()
	api.UpdatedAt = api.CreatedAt
	cp := *api
	cp.Headers = copyHeaders(api.Headers)
	cp.HeadersEnc = append([]byte(nil), api.HeadersEnc...)
	f.interfaces[api.ID] = &cp
	return nil
}

func (f *fakeRepo) UpdateInterface(_ context.Context, api *domain.Interface, version int64) error {
	cur, ok := f.interfaces[api.ID]
	if !ok {
		return errcode.ErrNotFound
	}
	if cur.Version != version {
		return errcode.ErrConcurrent
	}
	f.updateCalls++
	cur.Name, cur.Method, cur.URL = api.Name, api.Method, api.URL
	cur.Headers, cur.HeadersEnc = copyHeaders(api.Headers), append([]byte(nil), api.HeadersEnc...)
	cur.Body, cur.Timeouts, cur.Description = api.Body, api.Timeouts, api.Description
	cur.Version++
	cur.UpdatedAt = time.Now()
	api.Version = cur.Version
	return nil
}

func (f *fakeRepo) UpdateInterfaceStatus(_ context.Context, interfaceID int64, status string) error {
	api, ok := f.interfaces[interfaceID]
	if !ok {
		return errcode.ErrNotFound
	}
	api.Status = status
	api.Version++
	api.UpdatedAt = time.Now()
	f.statusCalls[interfaceID] = status
	return nil
}

func (f *fakeRepo) SoftDeleteInterface(_ context.Context, interfaceID int64) error {
	if _, ok := f.interfaces[interfaceID]; !ok {
		return errcode.ErrNotFound
	}
	f.deletedInterfaces = append(f.deletedInterfaces, interfaceID)
	delete(f.interfaces, interfaceID)
	return nil
}

// ===================== 阈值 =====================

func (f *fakeRepo) ListThresholds(_ context.Context, interfaceID int64) ([]domain.Threshold, error) {
	return append([]domain.Threshold(nil), f.thresholds[interfaceID]...), nil
}

func (f *fakeRepo) ReplaceThresholds(_ context.Context, interfaceID int64, items []domain.Threshold) error {
	if _, ok := f.interfaces[interfaceID]; !ok {
		return errcode.ErrNotFound
	}
	f.thresholdWrite++
	stored := make([]domain.Threshold, 0, len(items))
	for _, item := range items {
		f.nextThreshold++
		item.ID = f.nextThreshold
		item.InterfaceID = interfaceID
		item.Version = 1
		item.CreatedAt = time.Now()
		item.UpdatedAt = item.CreatedAt
		stored = append(stored, item)
	}
	f.thresholds[interfaceID] = stored
	return nil
}

// ===================== 测试辅助 =====================

// newTestService 构造带假仓储与固定加密密钥的 service。
func newTestService() (*Service, *fakeRepo) {
	repo := newFakeRepo()
	return New(repo, testHeaderKey()), repo
}

// testHeaderKey 单元测试用 32 字节密钥（DeriveHeaderKey 派生，避免硬编码随机值）。
func testHeaderKey() []byte { return DeriveHeaderKey("", "", "unit-test-secret") }

// addProduct 写入产品。
func addProduct(repo *fakeRepo, id int64, name, code string, status string) *domain.Product {
	p := &domain.Product{
		ID: id, Name: name, Code: code, ProductVersion: "v1.0.0",
		Status: status, Version: 1,
	}
	repo.products[id] = p
	return p
}

// addInterface 写入接口（可带认证头密文）。
func addInterface(repo *fakeRepo, id, productID int64, name string, status string, enc []byte) *domain.Interface {
	api := &domain.Interface{
		ID: id, ProductID: productID, Name: name, Method: domain.MethodGet,
		URL: "https://api.example.com/health", Headers: map[string]string{"Accept": "*/*"},
		HeadersEnc: enc, Status: status, Version: 1,
	}
	repo.interfaces[id] = api
	return api
}

// copyHeaders 复制请求头集合。
func copyHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(headers))
	for k, v := range headers {
		out[k] = v
	}
	return out
}

// paginate 按页切片（页码从 1 开始）。
func paginate[T any](list []T, page, pageSize int) []T {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	start := (page - 1) * pageSize
	if start >= len(list) {
		return []T{}
	}
	end := start + pageSize
	if end > len(list) {
		end = len(list)
	}
	return list[start:end]
}
