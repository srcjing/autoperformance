package product

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"auto_performance/internal/crypto"
	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// ===================== 接口（Interface） =====================

// InterfaceInput 接口新建/修改输入（白名单字段；status 只能经状态接口变更）。
//
// Headers 为全量替换语义：认证头值留空表示「沿用已保存的密文」，名称缺省表示删除。
type InterfaceInput struct {
	Name        string
	Method      string
	Scheme      string // http | https
	Port        int    // 1-65535
	Path        string // 以 / 开头的路径（不含 host）
	Headers     map[string]string
	Body        string
	Timeouts    domain.InterfaceTimeouts
	Description string
	Version     int64 // 乐观锁版本号（修改时必填）
}

// InterfaceView 接口视图。
//
// AuthHeaders 仅返回已配置认证头的「名称」，值不出现在任何响应中（DES-I-001）；
// Headers 只含普通请求头。
type InterfaceView struct {
	ID          int64
	ProductID   int64
	Name        string
	Method      string
	Scheme      string
	Port        int
	Path        string
	Headers     map[string]string
	AuthHeaders []string
	Body        string
	Timeouts    domain.InterfaceTimeouts
	Description string
	Status      string
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// InterfacePage 接口分页结果。
type InterfacePage struct {
	List     []InterfaceView
	Total    int64
	Page     int
	PageSize int
}

// ListInterfacesQuery 接口列表查询条件。
type ListInterfacesQuery struct {
	ProductID int64
	Keyword   string
	Status    string
	Page      int
	PageSize  int
}

// ListInterfaces 产品下接口列表（产品不存在 → 10301）。
func (s *Service) ListInterfaces(ctx context.Context, q ListInterfacesQuery) (*InterfacePage, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if q.ProductID <= 0 {
		return nil, errcode.ErrParamFmt
	}
	status := strings.TrimSpace(q.Status)
	if status != "" && !domain.IsValidInterfaceStatus(status) {
		return nil, errcode.ErrParamFmt
	}
	if _, err := s.repo.GetProduct(ctx, q.ProductID); err != nil {
		return nil, err
	}

	page, pageSize := normalizePaging(q.Page, q.PageSize)
	items, total, err := s.repo.ListInterfaces(ctx, domain.InterfaceQuery{
		ProductID: q.ProductID,
		Keyword:   strings.TrimSpace(q.Keyword),
		Status:    status,
		Page:      page,
		PageSize:  pageSize,
	})
	if err != nil {
		return nil, err
	}

	list := make([]InterfaceView, 0, len(items))
	for i := range items {
		view, err := s.buildInterfaceView(&items[i])
		if err != nil {
			return nil, err
		}
		list = append(list, *view)
	}
	return &InterfacePage{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// GetInterface 接口详情（不存在 → 10301）。
func (s *Service) GetInterface(ctx context.Context, interfaceID int64) (*InterfaceView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if interfaceID <= 0 {
		return nil, errcode.ErrParamFmt
	}
	api, err := s.repo.GetInterface(ctx, interfaceID)
	if err != nil {
		return nil, err
	}
	return s.buildInterfaceView(api)
}

// CreateInterface 新建接口（所属产品必须存在；初始状态 enabled）。
func (s *Service) CreateInterface(ctx context.Context, productID int64, in InterfaceInput) (*InterfaceView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if productID <= 0 {
		return nil, errcode.ErrParamFmt
	}
	if _, err := s.repo.GetProduct(ctx, productID); err != nil {
		return nil, err
	}

	api, err := validateInterfaceInput(in)
	if err != nil {
		return nil, err
	}
	api.ProductID = productID
	api.Status = domain.InterfaceStatusEnabled

	_, sensitive := domain.SplitHeaders(in.Headers)
	// 新建时留空的认证头无「已保存值」可沿用，直接忽略。
	merged := make(map[string]string, len(sensitive))
	for name, value := range sensitive {
		if strings.TrimSpace(value) == "" {
			continue
		}
		merged[name] = value
	}
	enc, err := s.encryptAuthHeaders(merged)
	if err != nil {
		return nil, err
	}
	api.HeadersEnc = enc

	if err := s.repo.CreateInterface(ctx, api); err != nil {
		return nil, err
	}
	return s.GetInterface(ctx, api.ID)
}

// UpdateInterface 全量修改接口（version 乐观锁 → 10402）。
//
// 认证头合并规则：值非空 → 覆盖；值留空 → 沿用已保存密文中的旧值；名称缺省 → 删除。
func (s *Service) UpdateInterface(ctx context.Context, interfaceID int64, in InterfaceInput) (*InterfaceView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if interfaceID <= 0 {
		return nil, errcode.ErrParamFmt
	}
	if in.Version <= 0 {
		return nil, errcode.ErrParamMiss // 全量修改必须携带 version（api-contract §2.3）
	}

	current, err := s.repo.GetInterface(ctx, interfaceID)
	if err != nil {
		return nil, err
	}
	api, err := validateInterfaceInput(in)
	if err != nil {
		return nil, err
	}

	existing, err := s.decryptAuthHeaders(current.HeadersEnc)
	if err != nil {
		return nil, err
	}
	_, sensitive := domain.SplitHeaders(in.Headers)
	merged := make(map[string]string, len(sensitive))
	for name, value := range sensitive {
		if strings.TrimSpace(value) == "" {
			if old, ok := lookupHeaderFold(existing, name); ok {
				merged[name] = old
			}
			continue
		}
		merged[name] = value
	}
	enc, err := s.encryptAuthHeaders(merged)
	if err != nil {
		return nil, err
	}

	api.ID = interfaceID
	api.ProductID = current.ProductID
	api.Status = current.Status // 状态仅能经 PATCH /status 变更
	api.HeadersEnc = enc
	if err := s.repo.UpdateInterface(ctx, api, in.Version); err != nil {
		return nil, err
	}
	return s.GetInterface(ctx, interfaceID)
}

// DeleteInterface 逻辑删除接口（阈值随接口保留，历史任务快照可复现）。
func (s *Service) DeleteInterface(ctx context.Context, interfaceID int64) error {
	if s == nil || s.repo == nil {
		return errcode.ErrDependency
	}
	if interfaceID <= 0 {
		return errcode.ErrParamFmt
	}
	if _, err := s.repo.GetInterface(ctx, interfaceID); err != nil {
		return err
	}
	return s.repo.SoftDeleteInterface(ctx, interfaceID)
}

// UpdateInterfaceStatus 停用/启用接口（enabled ↔ disabled；同状态 → 10401）。
func (s *Service) UpdateInterfaceStatus(ctx context.Context, interfaceID int64, status string) error {
	if s == nil || s.repo == nil {
		return errcode.ErrDependency
	}
	if interfaceID <= 0 {
		return errcode.ErrParamFmt
	}
	status = strings.TrimSpace(status)
	if !domain.IsValidInterfaceStatus(status) {
		return errcode.ErrParamFmt
	}

	current, err := s.repo.GetInterface(ctx, interfaceID)
	if err != nil {
		return err
	}
	if !domain.CanTransitInterfaceStatus(current.Status, status) {
		return errcode.ErrState
	}
	return s.repo.UpdateInterfaceStatus(ctx, interfaceID, status)
}

// validateInterfaceInput 校验并归一化接口输入（普通头留在 Headers，认证头由调用方加密）。
func validateInterfaceInput(in InterfaceInput) (*domain.Interface, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errcode.ErrParamMiss
	}
	if utf8.RuneCountInString(name) > maxNameLen {
		return nil, errcode.ErrParamFmt
	}

	// 接口地址 = 协议 + 端口 + 路径（不配置 host；host 由任务所选环境决定）。
	scheme := strings.ToLower(strings.TrimSpace(in.Scheme))
	if scheme == "" {
		return nil, errcode.ErrParamMiss
	}
	if !domain.IsValidScheme(scheme) {
		return nil, errcode.ErrParamFmt // 协议仅支持 http/https
	}
	if in.Port == 0 {
		return nil, errcode.ErrParamMiss
	}
	if !domain.IsValidPort(in.Port) {
		return nil, errcode.ErrParamFmt // 端口需在 1~65535
	}
	path := strings.TrimSpace(in.Path)
	if path == "" {
		return nil, errcode.ErrParamMiss
	}
	if !domain.IsValidInterfacePath(path) {
		return nil, errcode.ErrParamFmt // 路径必须以 / 开头且不含空白/控制字符
	}

	method := strings.ToUpper(strings.TrimSpace(in.Method))
	if method == "" {
		method = domain.MethodGet
	}
	if !domain.IsValidMethod(method) {
		return nil, errcode.ErrParamFmt
	}

	if len(in.Headers) > maxHeaderCount {
		return nil, errcode.ErrParamFmt
	}
	for headerName, value := range in.Headers {
		trimmed := strings.TrimSpace(headerName)
		if trimmed == "" {
			return nil, errcode.ErrParamFmt
		}
		if utf8.RuneCountInString(trimmed) > maxHeaderNameLen || len(value) > maxHeaderValLen {
			return nil, errcode.ErrParamFmt
		}
	}
	safeHeaders, _ := domain.SplitHeaders(in.Headers)

	body := strings.TrimSpace(in.Body)
	if len(body) > maxBodyLen {
		return nil, errcode.ErrParamFmt
	}
	if body != "" && !json.Valid([]byte(body)) {
		return nil, errcode.ErrParamFmt // 参数模板必须是合法 JSON（api-contract §2.3 body）
	}

	timeouts := in.Timeouts
	for _, ms := range []int64{timeouts.ConnectMS, timeouts.ReadMS, timeouts.TotalMS} {
		if ms < 0 || ms > maxTimeoutMS {
			return nil, errcode.ErrParamFmt
		}
	}

	description := strings.TrimSpace(in.Description)
	if utf8.RuneCountInString(description) > maxDescription {
		return nil, errcode.ErrParamFmt
	}

	return &domain.Interface{
		Name:        name,
		Method:      method,
		Scheme:      scheme,
		Port:        in.Port,
		Path:        path,
		Headers:     safeHeaders,
		Body:        body,
		Timeouts:    timeouts,
		Description: description,
	}, nil
}

// buildInterfaceView 领域实体 → 视图（认证头只暴露名称）。
func (s *Service) buildInterfaceView(api *domain.Interface) (*InterfaceView, error) {
	authHeaders, err := s.decryptAuthHeaders(api.HeadersEnc)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(authHeaders))
	for name := range authHeaders {
		names = append(names, name)
	}
	sort.Strings(names)

	headers := make(map[string]string, len(api.Headers))
	for name, value := range api.Headers {
		headers[name] = value
	}

	return &InterfaceView{
		ID:          api.ID,
		ProductID:   api.ProductID,
		Name:        api.Name,
		Method:      api.Method,
		Scheme:      api.Scheme,
		Port:        api.Port,
		Path:        api.Path,
		Headers:     headers,
		AuthHeaders: names,
		Body:        api.Body,
		Timeouts:    api.Timeouts,
		Description: api.Description,
		Status:      api.Status,
		Version:     api.Version,
		CreatedAt:   api.CreatedAt,
		UpdatedAt:   api.UpdatedAt,
	}, nil
}

// encryptAuthHeaders 加密认证头集合：JSON → AES-256-GCM → headers_enc（DES-I-001 / INV-7）。
// 空集合返回 nil（DB 侧写 NULL，避免空密文歧义）。
func (s *Service) encryptAuthHeaders(headers map[string]string) ([]byte, error) {
	if len(headers) == 0 {
		return nil, nil
	}
	if len(s.headerKey) != crypto.KeySize {
		return nil, fmt.Errorf("接口认证头加密密钥未初始化: %w", errcode.ErrDependency)
	}
	plain, err := json.Marshal(headers)
	if err != nil {
		return nil, fmt.Errorf("序列化接口认证头失败: %w", err)
	}
	enc, err := crypto.EncryptGCM(plain, s.headerKey)
	if err != nil {
		return nil, fmt.Errorf("加密接口认证头失败: %w", errcode.ErrDependency)
	}
	return enc, nil
}

// decryptAuthHeaders 解密 headers_enc；密文为空返回空集合。
// 解密失败（密钥轮换/密文损坏）返回 10602，避免静默丢弃认证头。
func (s *Service) decryptAuthHeaders(enc []byte) (map[string]string, error) {
	if len(enc) == 0 {
		return map[string]string{}, nil
	}
	if len(s.headerKey) != crypto.KeySize {
		return nil, fmt.Errorf("接口认证头加密密钥未初始化: %w", errcode.ErrDependency)
	}
	plain, err := crypto.DecryptGCM(enc, s.headerKey)
	if err != nil {
		return nil, fmt.Errorf("解密接口认证头失败: %w", errcode.ErrDependency)
	}
	out := make(map[string]string)
	if err := json.Unmarshal(plain, &out); err != nil {
		return nil, fmt.Errorf("解析接口认证头失败: %w", errcode.ErrDependency)
	}
	return out, nil
}

// lookupHeaderFold 按大小写不敏感方式查找请求头（HTTP 头名称大小写不敏感）。
func lookupHeaderFold(headers map[string]string, name string) (string, bool) {
	if value, ok := headers[name]; ok {
		return value, true
	}
	for key, value := range headers {
		if strings.EqualFold(key, name) {
			return value, true
		}
	}
	return "", false
}
