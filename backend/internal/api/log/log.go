// Package log 日志管理 HTTP handler（用户/系统/测试/请求详情查询 + 日志设置）。
//
// 对齐 api-contract §2.6 与 §5.2：
//   - 日志查询权限点：log.user:read / log.system:read / log.test:read / log.request:read
//     （日志默认只读，无删除/修改端点）；
//   - 日志设置（GET/PUT /log-settings）归 setting.log 子模块：查看 read、保存 write；
//   - 错误码：25001 查询时间范围非法（服务层校验）、10003 参数/设置非法；
//   - ID 一律以字符串输出（api-contract §1.7），query 兼容字符串/数字 ID；
//   - 审计日志 HMAC 链式防篡改、写入前脱敏在服务层与仓储层强制（DES-T-006 / DES-I-001）。
package log

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"auto_performance/internal/api/response"
	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
	logsvc "auto_performance/internal/service/log"
)

// Service 日志服务能力（*logsvc.Service 实现；便于 handler 测试注入桩）。
type Service interface {
	ListUserLogs(ctx context.Context, q domain.UserLogQuery) (*logsvc.UserLogPage, error)
	ListSystemLogs(ctx context.Context, q domain.SystemLogQuery) (*logsvc.SystemLogPage, error)
	ListTestLogs(ctx context.Context, q domain.TestLogQuery) (*logsvc.TestLogPage, error)
	// ListRequestLogs 请求详情查询（压测逐请求明细，可按任务/接口定位）。
	ListRequestLogs(ctx context.Context, q domain.RequestLogQuery) (*logsvc.RequestLogPage, error)
	// GetLogSetting 读取日志设置（服务开关/最大条数/存储天数/启用类型）。
	GetLogSetting(ctx context.Context) (*domain.LogSetting, error)
	// UpdateLogSetting 保存日志设置，并触发一次过期/超量清理。
	UpdateLogSetting(ctx context.Context, in domain.LogSetting) (*domain.LogSetting, error)
}

// Handler 日志管理 HTTP handler。
type Handler struct {
	svc Service
}

// New 构造日志 handler。
func New(svc Service) *Handler { return &Handler{svc: svc} }

// 编译期断言：业务服务满足 handler 依赖（避免签名漂移）。
var _ Service = (*logsvc.Service)(nil)

// ===================== DTO =====================

// userLogDTO 用户日志响应（5W1H：操作人/时间/动作/对象/结果 + 来源 IP）。
type userLogDTO struct {
	ID            string `json:"id"`
	AdminID       string `json:"admin_id,omitempty"`
	AdminUsername string `json:"admin_username,omitempty"`
	Action        string `json:"action"`
	ResourceType  string `json:"resource_type,omitempty"`
	ResourceID    string `json:"resource_id,omitempty"`
	Detail        string `json:"detail,omitempty"`
	IP            string `json:"ip,omitempty"`
	Result        string `json:"result"`
	// Level 由 result 推导（ok→info、fail→error），便于前端按级别着色（user_log 无 level 列）。
	Level     string `json:"level"`
	CreatedAt string `json:"created_at"`
}

// systemLogDTO 系统日志响应（运行状态/启动/重启/停止）。
type systemLogDTO struct {
	ID        string `json:"id"`
	Level     string `json:"level"`
	Event     string `json:"event,omitempty"`
	Message   string `json:"message,omitempty"`
	CreatedAt string `json:"created_at"`
}

// requestLogDTO 请求详情响应（任务/接口定位 + 请求/响应摘要 + 错误与耗时）。
type requestLogDTO struct {
	ID             string `json:"id"`
	TaskID         string `json:"task_id,omitempty"`
	InterfaceID    string `json:"interface_id,omitempty"`
	Seq            int64  `json:"seq"`
	OccurredAt     string `json:"occurred_at"`
	Method         string `json:"method"`
	URL            string `json:"url"`
	RequestBody     string `json:"request_body,omitempty"`
	RequestHeaders  string `json:"request_headers,omitempty"`
	ResponseStatus  int    `json:"response_status"`
	ResponseBody    string `json:"response_body,omitempty"`
	ResponseHeaders string `json:"response_headers,omitempty"`
	ErrorCode      string `json:"error_code,omitempty"`
	ErrorMessage   string `json:"error_message,omitempty"`
	DurationMs     int64  `json:"duration_ms"`
}

// logSettingDTO 日志设置响应。
type logSettingDTO struct {
	Enabled       bool     `json:"enabled"`
	MaxCount      int64    `json:"max_count"`
	RetentionDays int      `json:"retention_days"`
	LogTypes      []string `json:"log_types"`
	UpdatedAt     string   `json:"updated_at,omitempty"`
}

// logSettingPayload 日志设置请求体（白名单字段 + 指针支持部分更新，避免误清零）。
type logSettingPayload struct {
	Enabled       *bool     `json:"enabled"`
	MaxCount      *int64    `json:"max_count"`
	RetentionDays *int      `json:"retention_days"`
	LogTypes      *[]string `json:"log_types"`
}

// testLogDTO 测试日志响应（任务定位 + 增量 seq + 级别 + 内容）。
type testLogDTO struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id,omitempty"`
	Seq       int64  `json:"seq"`
	Level     string `json:"level"`
	Message   string `json:"message,omitempty"`
	CreatedAt string `json:"created_at"`
}

// ===================== 查询参数解析 =====================

// listFilters 三类日志共用的查询条件。
type listFilters struct {
	Level     string
	Keyword   string
	StartTime *time.Time
	EndTime   *time.Time
	Page      int
	PageSize  int
}

// parseListFilters 解析通用 query：level/keyword/start_time/end_time/page/page_size。
func parseListFilters(c *gin.Context) (listFilters, *errcode.Error) {
	start, err := parseTimeQuery(c, "start_time")
	if err != nil {
		return listFilters{}, errcode.ErrParamFmt
	}
	end, err := parseTimeQuery(c, "end_time")
	if err != nil {
		return listFilters{}, errcode.ErrParamFmt
	}
	page, _ := parseIntQuery(c, "page")
	pageSize, _ := parseIntQuery(c, "page_size")
	return listFilters{
		Level:     c.Query("level"),
		Keyword:   c.Query("keyword"),
		StartTime: start,
		EndTime:   end,
		Page:      page,
		PageSize:  pageSize,
	}, nil
}

// parseTimeQuery 解析 RFC3339 时间参数（缺失返回 nil；格式非法返回错误）。
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

// parseIntQuery 解析整型 query 参数（缺失/非法返回 0,false）。
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

// parseIDValue 解析 bigint 字符串 query（空返回 0；非数字/负数 → 10003）。
func parseIDValue(raw string) (int64, *errcode.Error) {
	if raw == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 0 {
		return 0, errcode.ErrParamFmt
	}
	return id, nil
}

// itoa int64 → 十进制字符串（ID 统一字符串输出）。
func itoa(v int64) string { return strconv.FormatInt(v, 10) }

// formatTime 统一 UTC RFC3339 输出（零值输出空字符串）。
func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02T15:04:05Z07:00")
}

// ===================== 转换 =====================

// toUserLogDTOs 用户日志视图 → 响应（level 由 result 推导）。
func toUserLogDTOs(list []domain.AuditLog) []userLogDTO {
	out := make([]userLogDTO, 0, len(list))
	for i := range list {
		entry := &list[i]
		adminID := ""
		if entry.AdminID != nil && *entry.AdminID > 0 {
			adminID = itoa(*entry.AdminID)
		}
		out = append(out, userLogDTO{
			ID:            itoa(entry.ID),
			AdminID:       adminID,
			AdminUsername: entry.AdminUsername,
			Action:        entry.Action,
			ResourceType:  entry.ResourceType,
			ResourceID:    entry.ResourceID,
			Detail:        entry.Detail,
			IP:            entry.IP,
			Result:        entry.Result,
			Level:         domain.LogLevelOfResult(entry.Result),
			CreatedAt:     formatTime(entry.CreatedAt),
		})
	}
	return out
}

// toSystemLogDTOs 系统日志视图 → 响应。
func toSystemLogDTOs(list []domain.SystemLog) []systemLogDTO {
	out := make([]systemLogDTO, 0, len(list))
	for i := range list {
		entry := &list[i]
		out = append(out, systemLogDTO{
			ID:        itoa(entry.ID),
			Level:     entry.Level,
			Event:     entry.Event,
			Message:   entry.Message,
			CreatedAt: formatTime(entry.CreatedAt),
		})
	}
	return out
}

// toTestLogDTOs 测试日志视图 → 响应。
func toTestLogDTOs(list []domain.TestLog) []testLogDTO {
	out := make([]testLogDTO, 0, len(list))
	for i := range list {
		entry := &list[i]
		taskID := ""
		if entry.TaskID > 0 {
			taskID = itoa(entry.TaskID)
		}
		out = append(out, testLogDTO{
			ID:        itoa(entry.ID),
			TaskID:    taskID,
			Seq:       entry.Seq,
			Level:     entry.Level,
			Message:   entry.Message,
			CreatedAt: formatTime(entry.CreatedAt),
		})
	}
	return out
}

// ===================== handler =====================

// ListUserLogs GET /api/v1/logs/user（log.user:read）
//
// 筛选：admin_id / action / resource_type / resource_id / level / keyword /
// start_time|end_time（RFC3339）+ 分页。level 在用户日志中按 result 映射（info→ok、error→fail）。
func (h *Handler) ListUserLogs(c *gin.Context) {
	filters, e := parseListFilters(c)
	if e != nil {
		response.Error(c, e)
		return
	}
	adminID, e := parseIDValue(c.Query("admin_id"))
	if e != nil {
		response.Error(c, e)
		return
	}

	res, err := h.svc.ListUserLogs(c.Request.Context(), domain.UserLogQuery{
		LogQuery: domain.LogQuery{
			Level:     filters.Level,
			Keyword:   filters.Keyword,
			StartTime: filters.StartTime,
			EndTime:   filters.EndTime,
			Page:      filters.Page,
			PageSize:  filters.PageSize,
		},
		AdminID:      adminID,
		Action:       c.Query("action"),
		ResourceType: c.Query("resource_type"),
		ResourceID:   c.Query("resource_id"),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, gin.H{
		"list":      toUserLogDTOs(res.List),
		"total":     res.Total,
		"page":      res.Page,
		"page_size": res.PageSize,
	})
}

// ListSystemLogs GET /api/v1/logs/system（log.system:read）
//
// 筛选：event(start|restart|stop) / level / keyword / start_time|end_time + 分页。
func (h *Handler) ListSystemLogs(c *gin.Context) {
	filters, e := parseListFilters(c)
	if e != nil {
		response.Error(c, e)
		return
	}

	res, err := h.svc.ListSystemLogs(c.Request.Context(), domain.SystemLogQuery{
		LogQuery: domain.LogQuery{
			Level:     filters.Level,
			Keyword:   filters.Keyword,
			StartTime: filters.StartTime,
			EndTime:   filters.EndTime,
			Page:      filters.Page,
			PageSize:  filters.PageSize,
		},
		Event: c.Query("event"),
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, gin.H{
		"list":      toSystemLogDTOs(res.List),
		"total":     res.Total,
		"page":      res.Page,
		"page_size": res.PageSize,
	})
}

// ListTestLogs GET /api/v1/logs/test（log.test:read）
//
// 筛选：task_id（按任务定位）/ level / keyword / start_time|end_time（25001）+ 分页。
// 带 task_id 时按 seq 升序（执行顺序），跨任务时按时间倒序。
func (h *Handler) ListTestLogs(c *gin.Context) {
	filters, e := parseListFilters(c)
	if e != nil {
		response.Error(c, e)
		return
	}
	taskID, e := parseIDValue(c.Query("task_id"))
	if e != nil {
		response.Error(c, e)
		return
	}

	res, err := h.svc.ListTestLogs(c.Request.Context(), domain.TestLogQuery{
		LogQuery: domain.LogQuery{
			Level:     filters.Level,
			Keyword:   filters.Keyword,
			StartTime: filters.StartTime,
			EndTime:   filters.EndTime,
			Page:      filters.Page,
			PageSize:  filters.PageSize,
		},
		TaskID: taskID,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.OK(c, gin.H{
		"list":      toTestLogDTOs(res.List),
		"total":     res.Total,
		"page":      res.Page,
		"page_size": res.PageSize,
	})
}
