// Package product 产品管理 HTTP handler（产品/接口/阈值）。
//
// 对齐 api-contract §2.3 与 §5.2：
//   - 路由权限点：产品 product.list，接口与阈值 product.interface（read/write 分离）；
//   - 接口认证头只下发名称（auth_headers），绝不下发明文值（DES-I-001 / TEST-I-001）；
//   - ID 一律以字符串输出（api-contract §1.7），请求体兼容字符串/数字 ID。
package product

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/errcode"
	productsvc "auto_performance/internal/service/product"
)

// Service 产品管理业务服务能力（*productsvc.Service 实现；便于 handler 层测试注入桩）。
type Service interface {
	// 产品
	ListProducts(ctx context.Context, q productsvc.ListProductsQuery) (*productsvc.ProductPage, error)
	GetProduct(ctx context.Context, productID int64) (*productsvc.ProductView, error)
	CreateProduct(ctx context.Context, in productsvc.ProductInput) (*productsvc.ProductView, error)
	UpdateProduct(ctx context.Context, productID int64, in productsvc.ProductInput) (*productsvc.ProductView, error)
	DeleteProduct(ctx context.Context, productID int64) error
	UpdateStatus(ctx context.Context, productID int64, status string) error

	// 接口
	ListInterfaces(ctx context.Context, q productsvc.ListInterfacesQuery) (*productsvc.InterfacePage, error)
	GetInterface(ctx context.Context, interfaceID int64) (*productsvc.InterfaceView, error)
	CreateInterface(ctx context.Context, productID int64, in productsvc.InterfaceInput) (*productsvc.InterfaceView, error)
	UpdateInterface(ctx context.Context, interfaceID int64, in productsvc.InterfaceInput) (*productsvc.InterfaceView, error)
	DeleteInterface(ctx context.Context, interfaceID int64) error
	UpdateInterfaceStatus(ctx context.Context, interfaceID int64, status string) error

	// 阈值
	ListThresholds(ctx context.Context, interfaceID int64) ([]productsvc.ThresholdView, error)
	ReplaceThresholds(ctx context.Context, interfaceID int64, inputs []productsvc.ThresholdInput) ([]productsvc.ThresholdView, error)
}

// Handler 产品管理 HTTP handler。
type Handler struct {
	svc Service
}

// New 构造产品管理 handler。
func New(svc Service) *Handler { return &Handler{svc: svc} }

// 编译期断言：业务服务满足 handler 依赖（避免签名漂移）。
var _ Service = (*productsvc.Service)(nil)

// ===================== DTO =====================

// productDTO 产品响应。
type productDTO struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Code           string `json:"code"`
	ProductVersion string `json:"product_version"`
	Platform       string `json:"platform"`
	Description    string `json:"description"`
	Status         string `json:"status"`
	InterfaceCount int64  `json:"interface_count"`
	Version        int64  `json:"version"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

// timeoutsDTO 接口超时响应（毫秒）。
type timeoutsDTO struct {
	ConnectMS int64 `json:"connect_ms"`
	ReadMS    int64 `json:"read_ms"`
	TotalMS   int64 `json:"total_ms"`
}

// interfaceDTO 接口响应。
//
// Headers 仅含普通请求头；AuthHeaders 仅含认证头「名称」（值永不下发，DES-I-001）。
type interfaceDTO struct {
	ID          string            `json:"id"`
	ProductID   string            `json:"product_id"`
	Name        string            `json:"name"`
	Method      string            `json:"method"`
	URL         string            `json:"url"`
	Headers     map[string]string `json:"headers"`
	AuthHeaders []string          `json:"auth_headers"`
	Body        json.RawMessage   `json:"body,omitempty"`
	Timeouts    timeoutsDTO       `json:"timeouts"`
	Description string            `json:"description"`
	Status      string            `json:"status"`
	Version     int64             `json:"version"`
	CreatedAt   string            `json:"created_at"`
	UpdatedAt   string            `json:"updated_at"`
}

// thresholdDTO 阈值响应。
type thresholdDTO struct {
	ID          string  `json:"id"`
	InterfaceID string  `json:"interface_id"`
	Metric      string  `json:"metric"`
	Operator    string  `json:"operator"`
	Value       float64 `json:"value"`
	Unit        string  `json:"unit"`
	Enabled     bool    `json:"enabled"`
	Version     int64   `json:"version"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// ===================== 请求体 =====================

// flexID 兼容字符串/数字 ID（api-contract §1.7）。
type flexID string

// UnmarshalJSON 接受 "12" / 12 / null。
func (f *flexID) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*f = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*f = flexID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err == nil {
		*f = flexID(n.String())
		return nil
	}
	return fmt.Errorf("id 取值非法: %s", string(data))
}

// productRequest 产品新建/修改请求体（白名单字段）。
//
// 故意不声明 status：产品状态只能经 PATCH /products/{id}/status 变更。
type productRequest struct {
	Name           string `json:"name" binding:"required"`
	Code           string `json:"code" binding:"required"`
	ProductVersion string `json:"product_version" binding:"required"`
	Platform       string `json:"platform"`
	Description    string `json:"description"`
	Version        int64  `json:"version"`
}

// productStatusRequest 产品停用/启用请求体。
type productStatusRequest struct {
	Status string `json:"status" binding:"required"`
}

// timeoutsRequest 接口超时请求体（毫秒）。
type timeoutsRequest struct {
	ConnectMS int64 `json:"connect_ms"`
	ReadMS    int64 `json:"read_ms"`
	TotalMS   int64 `json:"total_ms"`
}

// interfaceRequest 接口新建/修改请求体（全量字段 + version 乐观锁）。
//
// headers 为全量替换：认证头值留空表示沿用已保存密文，名称缺省表示删除（DES-I-001）。
type interfaceRequest struct {
	Name        string            `json:"name" binding:"required"`
	Method      string            `json:"method"`
	URL         string            `json:"url" binding:"required"`
	Headers     map[string]string `json:"headers"`
	Body        json.RawMessage   `json:"body"`
	Timeouts    *timeoutsRequest  `json:"timeouts"`
	Description string            `json:"description"`
	Version     int64             `json:"version"`
}

// interfaceStatusRequest 接口停用/启用请求体。
type interfaceStatusRequest struct {
	Status string `json:"status" binding:"required"`
}

// thresholdItem 单条阈值请求项。
type thresholdItem struct {
	Metric   string  `json:"metric" binding:"required"`
	Operator string  `json:"operator" binding:"required"`
	Value    float64 `json:"value"`
	Unit     string  `json:"unit"`
	Enabled  *bool   `json:"enabled"`
}

// thresholdsRequest 阈值全量替换请求体。
type thresholdsRequest struct {
	Thresholds []thresholdItem `json:"thresholds"`
}

// ===================== 解析 / 转换 =====================

// parseIntQuery 解析整型 query 参数；缺失或非法返回 (0,false)（由服务层取默认值）。
func parseIntQuery(c *gin.Context, key string) (int, bool) {
	raw := c.Query(key)
	if raw == "" {
		return 0, false
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return v, true
}

// parseID 解析路径参数 :id。
func parseID(c *gin.Context) (int64, error) { return parseIDValue(c.Param("id"), true) }

// parseIDValue 解析 bigint 字符串；required 为 true 时 0 视为非法。
func parseIDValue(raw string, required bool) (int64, error) {
	if raw == "" {
		if required {
			return 0, errcode.ErrParamFmt
		}
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 0 || (required && id == 0) {
		return 0, errcode.ErrParamFmt
	}
	return id, nil
}

// itoa int64 → 十进制字符串（DTO 的 ID 统一为字符串）。
func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// rfc3339 UTC RFC3339 输出（api-contract §1.6）。
func rfc3339(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05Z07:00") }

// toProductDTO 产品视图 → 响应。
func toProductDTO(v *productsvc.ProductView) productDTO {
	return productDTO{
		ID:             itoa(v.ID),
		Name:           v.Name,
		Code:           v.Code,
		ProductVersion: v.ProductVersion,
		Platform:       v.Platform,
		Description:    v.Description,
		Status:         v.Status,
		InterfaceCount: v.InterfaceCount,
		Version:        v.Version,
		CreatedAt:      rfc3339(v.CreatedAt),
		UpdatedAt:      rfc3339(v.UpdatedAt),
	}
}

// toProductListDTO 批量转换产品视图。
func toProductListDTO(views []productsvc.ProductView) []productDTO {
	out := make([]productDTO, 0, len(views))
	for i := range views {
		out = append(out, toProductDTO(&views[i]))
	}
	return out
}

// toInterfaceDTO 接口视图 → 响应（认证头只给名称）。
func toInterfaceDTO(v *productsvc.InterfaceView) interfaceDTO {
	headers := make(map[string]string, len(v.Headers))
	for name, value := range v.Headers {
		headers[name] = value
	}
	authHeaders := make([]string, 0, len(v.AuthHeaders))
	authHeaders = append(authHeaders, v.AuthHeaders...)

	dto := interfaceDTO{
		ID:          itoa(v.ID),
		ProductID:   itoa(v.ProductID),
		Name:        v.Name,
		Method:      v.Method,
		URL:         v.URL,
		Headers:     headers,
		AuthHeaders: authHeaders,
		Timeouts: timeoutsDTO{
			ConnectMS: v.Timeouts.ConnectMS,
			ReadMS:    v.Timeouts.ReadMS,
			TotalMS:   v.Timeouts.TotalMS,
		},
		Description: v.Description,
		Status:      v.Status,
		Version:     v.Version,
		CreatedAt:   rfc3339(v.CreatedAt),
		UpdatedAt:   rfc3339(v.UpdatedAt),
	}
	if v.Body != "" {
		dto.Body = json.RawMessage(v.Body)
	}
	return dto
}

// toInterfaceListDTO 批量转换接口视图。
func toInterfaceListDTO(views []productsvc.InterfaceView) []interfaceDTO {
	out := make([]interfaceDTO, 0, len(views))
	for i := range views {
		out = append(out, toInterfaceDTO(&views[i]))
	}
	return out
}

// toThresholdDTO 阈值视图 → 响应。
func toThresholdDTO(v *productsvc.ThresholdView) thresholdDTO {
	return thresholdDTO{
		ID:          itoa(v.ID),
		InterfaceID: itoa(v.InterfaceID),
		Metric:      v.Metric,
		Operator:    v.Operator,
		Value:       v.Value,
		Unit:        v.Unit,
		Enabled:     v.Enabled,
		Version:     v.Version,
		CreatedAt:   rfc3339(v.CreatedAt),
		UpdatedAt:   rfc3339(v.UpdatedAt),
	}
}

// toThresholdListDTO 批量转换阈值视图。
func toThresholdListDTO(views []productsvc.ThresholdView) []thresholdDTO {
	out := make([]thresholdDTO, 0, len(views))
	for i := range views {
		out = append(out, toThresholdDTO(&views[i]))
	}
	return out
}
