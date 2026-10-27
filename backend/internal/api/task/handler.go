// Package task 测试管理 HTTP handler（模板 + 任务 + 执行控制）。
//
// 对齐 api-contract §2.4 与 §5.2：
//   - 路由权限点：模板 testing.template，任务 testing.task（read/write 分离）；
//   - 任务创建五要素校验错误码：23002（环境占用）/23003（接口范围空）/23004（并发数非法）；
//   - 状态机与清理语义：start/cancel/retry/cleanup 非法状态 → 23001；
//   - ID 一律以字符串输出（api-contract §1.7），请求体兼容字符串/数字 ID。
//   - 接口认证头只输出名称（auth_headers），绝不输出密文或明文（INV-7）。
package task

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/auth"
	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
	testsvc "auto_performance/internal/service/testing"
)

// Service 测试管理业务服务能力（*testsvc.Service 实现；便于 handler 测试注入桩）。
type Service interface {
	// 模板
	ListTemplates(ctx context.Context, q testsvc.ListTemplatesQuery) (*testsvc.TemplatePage, error)
	GetTemplate(ctx context.Context, id int64) (*testsvc.TemplateView, error)
	CreateTemplate(ctx context.Context, in testsvc.CreateTemplateInput) (*testsvc.TemplateView, error)
	UpdateTemplate(ctx context.Context, id int64, in testsvc.CreateTemplateInput) (*testsvc.TemplateView, error)
	DeleteTemplate(ctx context.Context, id int64) error

	// 任务
	Create(ctx context.Context, in testsvc.CreateTaskInput) (*testsvc.TaskView, error)
	Get(ctx context.Context, id int64) (*testsvc.TaskView, error)
	List(ctx context.Context, q testsvc.ListTasksQuery) (*testsvc.TaskPage, error)
	Start(ctx context.Context, id int64) (*testsvc.TaskView, error)
	Cancel(ctx context.Context, id int64) (*testsvc.TaskView, error)
	Retry(ctx context.Context, id int64) (*testsvc.TaskView, error)
	Cleanup(ctx context.Context, id int64) (*testsvc.TaskView, error)
}

// Handler 测试管理 HTTP handler。
type Handler struct {
	svc Service
}

// New 构造测试管理 handler。
func New(svc Service) *Handler { return &Handler{svc: svc} }

// 编译期断言：业务服务满足 handler 依赖（避免签名漂移）。
var _ Service = (*testsvc.Service)(nil)

// ===================== DTO =====================

// templateDTO 模板响应。
type templateDTO struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	EnvIDs       []string        `json:"env_ids"`
	InterfaceIDs []string        `json:"interface_ids"`
	Flow         json.RawMessage `json:"flow"`
	ReportConfig json.RawMessage `json:"report_config"`
	CleanConfig  json.RawMessage `json:"clean_config"`
	Description  string          `json:"description"`
	Version      int64           `json:"version"`
	CreatedAt    string          `json:"created_at"`
	UpdatedAt    string          `json:"updated_at"`
}

// interfaceSnapshotDTO 接口范围快照响应（不含认证头值/密文）。
type interfaceSnapshotDTO struct {
	InterfaceID string                   `json:"interface_id"`
	ProductID   string                   `json:"product_id"`
	ProductName string                   `json:"product_name,omitempty"`
	Name        string                   `json:"name"`
	Method      string                   `json:"method"`
	URL         string                   `json:"url"`
	Headers     map[string]string        `json:"headers"`
	AuthHeaders []string                 `json:"auth_headers"`
	Body        string                   `json:"body,omitempty"`
	Timeouts    domain.InterfaceTimeouts `json:"timeouts"`
	Status      string                   `json:"status"`
	CapturedAt  string                   `json:"captured_at"`
}

// thresholdSnapshotDTO 阈值快照响应。
type thresholdSnapshotDTO struct {
	InterfaceID   string  `json:"interface_id"`
	InterfaceName string  `json:"interface_name,omitempty"`
	Metric        string  `json:"metric"`
	Operator      string  `json:"operator"`
	Value         float64 `json:"value"`
	Unit          string  `json:"unit,omitempty"`
	Enabled       bool    `json:"enabled"`
}

// taskDTO 任务响应。
type taskDTO struct {
	ID                     string                 `json:"id"`
	Name                   string                 `json:"name"`
	TemplateID             string                 `json:"template_id,omitempty"`
	EnvID                  string                 `json:"environment_id"`
	CreatedBy              string                 `json:"created_by,omitempty"`
	ProductIDs             []string               `json:"product_ids"`
	InterfaceIDs           []string               `json:"interface_ids"`
	InterfaceRangeSnapshot []interfaceSnapshotDTO `json:"interface_range_snapshot"`
	ThresholdSnapshot      []thresholdSnapshotDTO `json:"threshold_snapshot"`
	Flow                   json.RawMessage        `json:"flow"`
	ReportConfig           json.RawMessage        `json:"report_config"`
	CleanConfig            json.RawMessage        `json:"clean_config"`
	Status                 string                 `json:"status"`
	StartedAt              string                 `json:"started_at,omitempty"`
	FinishedAt             string                 `json:"finished_at,omitempty"`
	Result                 json.RawMessage        `json:"result,omitempty"`
	Version                int64                  `json:"version"`
	CreatedAt              string                 `json:"created_at"`
	UpdatedAt              string                 `json:"updated_at"`
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

// idList 兼容 ["1","2"] 与 [1,2] 两种 ID 数组写法。
type idList []string

// UnmarshalJSON 逐项接受字符串或数字 ID。
func (l *idList) UnmarshalJSON(data []byte) error {
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("id 列表格式非法: %w", err)
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		var s string
		if err := json.Unmarshal(item, &s); err == nil {
			out = append(out, s)
			continue
		}
		var n json.Number
		if err := json.Unmarshal(item, &n); err == nil {
			out = append(out, n.String())
			continue
		}
		return fmt.Errorf("id 取值非法: %s", string(item))
	}
	*l = out
	return nil
}

// templateRequest 模板新建/修改请求体（白名单字段）。
type templateRequest struct {
	Name         string          `json:"name" binding:"required"`
	EnvIDs       idList          `json:"env_ids"`
	InterfaceIDs idList          `json:"interface_ids"`
	Flow         json.RawMessage `json:"flow"`
	ReportConfig json.RawMessage `json:"report_config"`
	CleanConfig  json.RawMessage `json:"clean_config"`
	Description  string          `json:"description"`
	Version      int64           `json:"version"`
}

// taskRequest 任务创建请求体（五要素；未给出的字段可由模板补齐）。
type taskRequest struct {
	Name          string          `json:"name" binding:"required"`
	TemplateID    flexID          `json:"template_id"`
	EnvironmentID flexID          `json:"environment_id"`
	ProductIDs    idList          `json:"product_ids"`
	InterfaceIDs  idList          `json:"interface_ids"`
	Flow          json.RawMessage `json:"flow"`
	ReportConfig  json.RawMessage `json:"report_config"`
	CleanConfig   json.RawMessage `json:"clean_config"`
	AutoStart     bool            `json:"auto_start"`
}

// ===================== 解析 / 转换 =====================

// parseID 解析路径参数 :id。
func parseID(c *gin.Context) (int64, error) {
	return parseIDValue(c.Param("id"), true)
}

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

// parseIDList 解析字符串 ID 列表为 int64（逐项校验）。
func parseIDList(raw idList, required bool) ([]int64, error) {
	if len(raw) == 0 && required {
		return nil, errcode.ErrParamFmt
	}
	out := make([]int64, 0, len(raw))
	for _, s := range raw {
		id, err := parseIDValue(s, true)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

// parseIntQuery 解析整型 query 参数；缺失/非法返回 (0,false)。
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

// parseTimeQuery 解析 RFC3339 时间参数（缺失返回 nil）。
func parseTimeQuery(c *gin.Context, key string) (*time.Time, error) {
	raw := c.Query(key)
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, errcode.ErrParamFmt
	}
	return &t, nil
}

// currentAdminID 读取 AuthN 注入的操作人 ID（未认证为 0）。
func currentAdminID(c *gin.Context) int64 {
	v, ok := c.Get(auth.CtxAdminID)
	if !ok {
		return 0
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return 0
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

// itoa int64 → 十进制字符串（DTO 的 ID 统一为字符串）。
func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// itoaList 批量转换 ID 列表（nil → 空切片）。
func itoaList(ids []int64) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, itoa(id))
	}
	return out
}

// formatTime 统一 UTC RFC3339 输出（零值输出空字符串）。
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05Z07:00")
}

// formatTimePtr 可空时间。
func formatTimePtr(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return formatTime(*t)
}

// toTemplateDTO 模板视图 → 响应。
func toTemplateDTO(v *testsvc.TemplateView) templateDTO {
	return templateDTO{
		ID:           itoa(v.ID),
		Name:         v.Name,
		EnvIDs:       itoaList(v.EnvIDs),
		InterfaceIDs: itoaList(v.InterfaceIDs),
		Flow:         v.Flow,
		ReportConfig: v.ReportConfig,
		CleanConfig:  v.CleanConfig,
		Description:  v.Description,
		Version:      v.Version,
		CreatedAt:    formatTime(v.CreatedAt),
		UpdatedAt:    formatTime(v.UpdatedAt),
	}
}

// toTemplateListDTO 批量转换模板视图。
func toTemplateListDTO(views []testsvc.TemplateView) []templateDTO {
	out := make([]templateDTO, 0, len(views))
	for i := range views {
		out = append(out, toTemplateDTO(&views[i]))
	}
	return out
}

// toTaskDTO 任务视图 → 响应（认证头仅输出名称）。
func toTaskDTO(v *testsvc.TaskView) taskDTO {
	snapshots := make([]interfaceSnapshotDTO, 0, len(v.InterfaceSnapshots))
	for _, s := range v.InterfaceSnapshots {
		headers := s.Headers
		if headers == nil {
			headers = map[string]string{}
		}
		snapshots = append(snapshots, interfaceSnapshotDTO{
			InterfaceID: itoa(s.InterfaceID),
			ProductID:   itoa(s.ProductID),
			ProductName: s.ProductName,
			Name:        s.Name,
			Method:      s.Method,
			URL:         s.URL,
			Headers:     headers,
			AuthHeaders: append([]string{}, s.AuthHeaderNames...),
			Body:        s.Body,
			Timeouts:    s.Timeouts,
			Status:      s.Status,
			CapturedAt:  formatTime(s.CapturedAt),
		})
	}
	thresholds := make([]thresholdSnapshotDTO, 0, len(v.ThresholdSnapshots))
	for _, s := range v.ThresholdSnapshots {
		thresholds = append(thresholds, thresholdSnapshotDTO{
			InterfaceID:   itoa(s.InterfaceID),
			InterfaceName: s.InterfaceName,
			Metric:        s.Metric,
			Operator:      s.Operator,
			Value:         s.Value,
			Unit:          s.Unit,
			Enabled:       s.Enabled,
		})
	}
	templateID := ""
	if v.TemplateID > 0 {
		templateID = itoa(v.TemplateID)
	}
	createdBy := ""
	if v.CreatedBy > 0 {
		createdBy = itoa(v.CreatedBy)
	}
	return taskDTO{
		ID:                     itoa(v.ID),
		Name:                   v.Name,
		TemplateID:             templateID,
		EnvID:                  itoa(v.EnvID),
		CreatedBy:              createdBy,
		ProductIDs:             itoaList(v.ProductIDs),
		InterfaceIDs:           itoaList(v.InterfaceIDs),
		InterfaceRangeSnapshot: snapshots,
		ThresholdSnapshot:      thresholds,
		Flow:                   v.Flow,
		ReportConfig:           v.ReportConfig,
		CleanConfig:            v.CleanConfig,
		Status:                 string(v.Status),
		StartedAt:              formatTimePtr(v.StartedAt),
		FinishedAt:             formatTimePtr(v.FinishedAt),
		Result:                 v.Result,
		Version:                v.Version,
		CreatedAt:              formatTime(v.CreatedAt),
		UpdatedAt:              formatTime(v.UpdatedAt),
	}
}

// toTaskListDTO 批量转换任务视图。
func toTaskListDTO(views []testsvc.TaskView) []taskDTO {
	out := make([]taskDTO, 0, len(views))
	for i := range views {
		out = append(out, toTaskDTO(&views[i]))
	}
	return out
}
