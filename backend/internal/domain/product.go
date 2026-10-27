// Package domain 领域模型（实体 + 枚举/状态机）。
//
// 本文件定义产品管理实体（Product/Interface/Threshold）、状态枚举、请求头敏感度
// 分流与阈值合法性校验，对齐 PRD §3.2/§4.4~§4.6/§9、api-contract §2.3 与 INV-2/INV-6/INV-7。
package domain

import (
	"errors"
	"strings"
	"time"
)

// ===================== 阈值指标 / 方向 =====================

// 阈值指标枚举（PRD §9.1 / api-contract §2.3 metric）。
const (
	// MetricAvgRT 平均响应时间（ms，越小越好）。
	MetricAvgRT = "avg_rt"
	// MetricP95 95 分位响应时间（ms，越小越好）。
	MetricP95 = "p95"
	// MetricP99 99 分位响应时间（ms，越小越好）。
	MetricP99 = "p99"
	// MetricMaxRT 最大响应时间（ms，越小越好）。
	MetricMaxRT = "max_rt"
	// MetricThroughput 吞吐量（req/s，越大越好）。
	MetricThroughput = "throughput"
	// MetricErrorRate 错误率（%，越小越好）。
	MetricErrorRate = "error_rate"
	// MetricSuccessRate 成功率（%，越大越好）。
	MetricSuccessRate = "success_rate"
	// MetricConcurrency 并发数（个，越大越好）。
	MetricConcurrency = "concurrency"
)

// 阈值方向枚举（PRD §4.6 / api-contract §2.3 operator）。
const (
	// OperatorLE ≤（越小越好）。
	OperatorLE = "le"
	// OperatorGE ≥（越大越好）。
	OperatorGE = "ge"
)

// ErrThresholdMismatch 阈值方向与指标类型不匹配（由 service 层转业务码 22003）。
var ErrThresholdMismatch = errors.New("阈值方向与指标类型不匹配")

// thresholdOperators 指标 → 允许方向白名单（PRD §9.1 唯一事实源）。
var thresholdOperators = map[string][]string{
	MetricAvgRT:       {OperatorLE},
	MetricP95:         {OperatorLE},
	MetricP99:         {OperatorLE},
	MetricMaxRT:       {OperatorLE},
	MetricErrorRate:   {OperatorLE},
	MetricThroughput:  {OperatorGE},
	MetricSuccessRate: {OperatorGE},
	MetricConcurrency: {OperatorGE},
}

// thresholdUnits 指标 → 展示单位（请求未指定单位时的默认值，PRD §9.1）。
var thresholdUnits = map[string]string{
	MetricAvgRT:       "ms",
	MetricP95:         "ms",
	MetricP99:         "ms",
	MetricMaxRT:       "ms",
	MetricErrorRate:   "%",
	MetricSuccessRate: "%",
	MetricThroughput:  "req/s",
	MetricConcurrency: "个",
}

// Metrics 返回全部合法指标（顺序固定，供前端下拉与权限矩阵使用）。
func Metrics() []string {
	return []string{
		MetricAvgRT, MetricP95, MetricP99, MetricMaxRT,
		MetricThroughput, MetricErrorRate, MetricSuccessRate, MetricConcurrency,
	}
}

// IsValidMetric 判断指标类型是否合法。
func IsValidMetric(metric string) bool {
	_, ok := thresholdOperators[metric]
	return ok
}

// IsValidOperator 判断阈值方向是否合法。
func IsValidOperator(operator string) bool {
	return operator == OperatorLE || operator == OperatorGE
}

// AllowedOperators 返回指标允许的方向列表（指标非法时返回 nil）。
func AllowedOperators(metric string) []string {
	ops, ok := thresholdOperators[metric]
	if !ok {
		return nil
	}
	return append([]string(nil), ops...)
}

// DefaultThresholdUnit 返回指标默认单位（指标非法时返回空串）。
func DefaultThresholdUnit(metric string) string { return thresholdUnits[metric] }

// ValidateThreshold 校验「指标 + 方向」组合是否合法（INV-2 / PRD §9.3）。
//
// 合法映射：avg_rt/p95/p99/max_rt/error_rate → le；throughput/success_rate/concurrency → ge。
// 指标未知或方向未知同样视为非法组合（统一由 service 层映射为 22003）。
func ValidateThreshold(metric, operator string) error {
	allowed, ok := thresholdOperators[metric]
	if !ok {
		return ErrThresholdMismatch
	}
	for _, op := range allowed {
		if op == operator {
			return nil
		}
	}
	return ErrThresholdMismatch
}

// ===================== 产品 / 接口 =====================

// 产品状态枚举（PRD §3.2.4：启用 ↔ 停用）。
const (
	// ProductStatusEnabled 启用（可被新任务选择）。
	ProductStatusEnabled = "enabled"
	// ProductStatusDisabled 停用（保留历史引用，不出现在新任务可选列表）。
	ProductStatusDisabled = "disabled"
)

// 接口状态枚举（PRD §3.2.4：启用 ↔ 停用）。
const (
	// InterfaceStatusEnabled 启用。
	InterfaceStatusEnabled = "enabled"
	// InterfaceStatusDisabled 停用。
	InterfaceStatusDisabled = "disabled"
)

// 接口请求方法枚举（PRD §4.5）。
const (
	MethodGet    = "GET"
	MethodPost   = "POST"
	MethodPut    = "PUT"
	MethodDelete = "DELETE"
	MethodPatch  = "PATCH"
)

// MaskedHeaderValue 认证头脱敏后的展示值（响应/日志中仅保留掩码，DES-I-001）。
const MaskedHeaderValue = "******"

// sensitiveHeaderNames 认证/凭据类请求头白名单（小写，比较时大小写不敏感）。
var sensitiveHeaderNames = map[string]bool{
	"authorization":       true,
	"proxy-authorization": true,
	"cookie":              true,
	"set-cookie":          true,
	"token":               true,
	"x-token":             true,
	"x-auth-token":        true,
	"auth-token":          true,
	"x-access-token":      true,
	"x-api-key":           true,
	"api-key":             true,
	"x-csrf-token":        true,
	"x-xsrf-token":        true,
	"client-secret":       true,
	"x-client-secret":     true,
}

// IsValidProductStatus 判断产品状态取值是否合法。
func IsValidProductStatus(status string) bool {
	return status == ProductStatusEnabled || status == ProductStatusDisabled
}

// IsValidInterfaceStatus 判断接口状态取值是否合法。
func IsValidInterfaceStatus(status string) bool {
	return status == InterfaceStatusEnabled || status == InterfaceStatusDisabled
}

// CanTransitProductStatus 判断产品手动状态流转是否合法（enabled ↔ disabled；同状态视为冲突）。
func CanTransitProductStatus(from, to string) bool {
	if !IsValidProductStatus(from) || !IsValidProductStatus(to) || from == to {
		return false
	}
	return true
}

// CanTransitInterfaceStatus 判断接口手动状态流转是否合法（enabled ↔ disabled；同状态视为冲突）。
func CanTransitInterfaceStatus(from, to string) bool {
	if !IsValidInterfaceStatus(from) || !IsValidInterfaceStatus(to) || from == to {
		return false
	}
	return true
}

// IsValidMethod 判断接口请求方法是否合法（去空白后按大写比较）。
func IsValidMethod(method string) bool {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case MethodGet, MethodPost, MethodPut, MethodDelete, MethodPatch:
		return true
	default:
		return false
	}
}

// IsSensitiveHeader 判断请求头是否为认证/凭据类（大小写与首尾空白不敏感）。
// 命中者必须加密存储到 headers_enc，禁止明文落库/下发（DES-I-001 / INV-7）。
func IsSensitiveHeader(name string) bool {
	return sensitiveHeaderNames[normalizeHeaderName(name)]
}

// SplitHeaders 将请求头拆分为「普通头」与「认证头」两组，供仓储分别落 headers / headers_enc。
// 入参不会被修改；返回值非 nil（便于序列化为 JSON）。
func SplitHeaders(headers map[string]string) (safe, sensitive map[string]string) {
	safe = make(map[string]string, len(headers))
	sensitive = make(map[string]string)
	for name, value := range headers {
		key := strings.TrimSpace(name)
		if key == "" {
			continue
		}
		if IsSensitiveHeader(key) {
			sensitive[key] = value
			continue
		}
		safe[key] = value
	}
	return safe, sensitive
}

// RedactHeaderValues 返回「同名同结构、值被掩码」的请求头副本（日志/异常详情使用）。
func RedactHeaderValues(headers map[string]string) map[string]string {
	out := make(map[string]string, len(headers))
	for name := range headers {
		out[name] = MaskedHeaderValue
	}
	return out
}

// normalizeHeaderName 请求头名称归一化（小写 + 去首尾空白）。
func normalizeHeaderName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// Product 产品（PRD §4.4）。
//
// ProductVersion 为业务软件版本（如 v1.2.3），与乐观锁 Version 区分，对应 DB product_version 列。
type Product struct {
	ID             int64
	Name           string
	Code           string
	ProductVersion string
	Platform       string
	Description    string
	Status         string
	Version        int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
	// InterfaceCount 该产品下未删除接口数（列表/详情统计，不落库）。
	InterfaceCount int64
}

// IsDisabled 产品是否已停用。
func (p *Product) IsDisabled() bool { return p != nil && p.Status == ProductStatusDisabled }

// ProductQuery 产品列表查询条件（关键字覆盖名称/编码）。
type ProductQuery struct {
	Keyword        string
	ProductVersion string
	Platform       string
	Status         string
	Page           int
	PageSize       int
}

// InterfaceTimeouts 接口超时配置（毫秒；JSONB 存储）。
type InterfaceTimeouts struct {
	ConnectMS int64 `json:"connect_ms"`
	ReadMS    int64 `json:"read_ms"`
	TotalMS   int64 `json:"total_ms"`
}

// IsZero 是否未配置任何超时。
func (t InterfaceTimeouts) IsZero() bool { return t.ConnectMS == 0 && t.ReadMS == 0 && t.TotalMS == 0 }

// Interface 接口（PRD §4.5）。
//
// Headers 仅存放普通请求头；认证头明文只存在于服务层内存（SensitiveHeaders），
// 落库时写入 HeadersEnc（AES-256-GCM 密文），响应/日志一律脱敏（DES-I-001）。
type Interface struct {
	ID               int64
	ProductID        int64
	Name             string
	Method           string
	URL              string
	Headers          map[string]string
	SensitiveHeaders map[string]string
	HeadersEnc       []byte
	Body             string
	Timeouts         InterfaceTimeouts
	Description      string
	Status           string
	Version          int64
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// AuthHeaderNames 返回已配置认证头的名称列表（用于响应脱敏展示，不含值）。
func (i *Interface) AuthHeaderNames() []string {
	names := make([]string, 0, len(i.SensitiveHeaders))
	for name := range i.SensitiveHeaders {
		names = append(names, name)
	}
	return names
}

// IsDisabled 接口是否已停用。
func (i *Interface) IsDisabled() bool { return i != nil && i.Status == InterfaceStatusDisabled }

// InterfaceQuery 接口列表查询条件（产品内检索）。
type InterfaceQuery struct {
	ProductID int64
	Keyword   string
	Status    string
	Page      int
	PageSize  int
}

// Threshold 指标阈值（PRD §4.6）。
type Threshold struct {
	ID          int64
	InterfaceID int64
	Metric      string
	Operator    string
	Value       float64
	Unit        string
	Enabled     bool
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}
