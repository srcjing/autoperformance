package domain

import (
	"errors"
	"strings"
	"time"
)

// 日志类型枚举（log_setting.log_types）：日志设置勾选启用哪些日志类型，
// 生效于日志写入（未启用类型不落库）与过期清理（PRD §3.5）。
const (
	LogTypeUser    = "user"
	LogTypeSystem  = "system"
	LogTypeTest    = "test"
	LogTypeRequest = "request"
)

// 日志设置默认值与边界（对齐 migrations/000006 与 PRD §3.5 日志设置）。
const (
	// DefaultLogMaxCount 默认最大日志条数（超过后清理最旧记录）。
	DefaultLogMaxCount int64 = 100000
	// DefaultLogRetentionDays 默认存储时间范围（天）。
	DefaultLogRetentionDays = 30
	// MinLogMaxCount / MaxLogMaxCount 最大条数边界（过小会丢有效日志，过大影响存储）。
	MinLogMaxCount int64 = 1000
	MaxLogMaxCount int64 = 10_000_000
	// MinLogRetentionDays / MaxLogRetentionDays 存储时间范围边界（天）。
	MinLogRetentionDays = 1
	MaxLogRetentionDays = 3650
	// MaxRequestLogPerTask 单任务请求详情上限（超出按比例采样，保护存储与查询）。
	MaxRequestLogPerTask = 1000
	// MaxLogBodyBytes 请求体/响应体落库截断长度（4KB，DES-I-001 数据最小化）。
	MaxLogBodyBytes = 4096
)

// ErrInvalidLogSetting 日志设置非法（服务层映射为 10003）。
var ErrInvalidLogSetting = errors.New("日志设置非法")

// allLogTypes 全部日志类型（稳定顺序，用于默认值与校验）。
var allLogTypes = []string{LogTypeUser, LogTypeSystem, LogTypeTest, LogTypeRequest}

// IsValidLogType 校验日志类型枚举（user/system/test/request）。
func IsValidLogType(t string) bool {
	for _, v := range allLogTypes {
		if v == t {
			return true
		}
	}
	return false
}

// DefaultLogTypes 默认启用的日志类型（全部启用）。
func DefaultLogTypes() []string {
	return append([]string(nil), allLogTypes...)
}

// RequestLog 压测请求详情（request_log）：对被测接口的单个请求明细。
//
// RequestBody/ResponseBody 落库前统一脱敏并截断到 MaxLogBodyBytes；
// 响应内容来自引擎采集（k6 JSON 输出不含响应体时为摘要空串，字段保留待后续采集）。
type RequestLog struct {
	ID             int64
	TaskID         int64
	InterfaceID    int64
	Seq            int64 // 任务内请求写入序号（1 起）
	OccurredAt     time.Time
	Method         string
	URL            string
	RequestBody    string
	ResponseStatus int
	ResponseBody   string
	ErrorCode      string
	ErrorMessage   string
	DurationMs     int64
}

// HasContent 是否包含有效明细（URL 或错误信息至少其一，避免落空行）。
func (r RequestLog) HasContent() bool {
	return strings.TrimSpace(r.URL) != "" || strings.TrimSpace(r.ErrorMessage) != ""
}

// RequestLogQuery 请求详情查询条件（任务/接口/关键字/时间范围 + 分页）。
type RequestLogQuery struct {
	LogQuery
	TaskID      int64
	InterfaceID int64
}

// LogSetting 日志设置（单行表 log_setting）：服务开关 + 最大条数 + 存储天数 + 启用类型。
type LogSetting struct {
	Enabled       bool
	MaxCount      int64
	RetentionDays int
	LogTypes      []string
	UpdatedAt     time.Time
}

// DefaultLogSetting 默认日志设置（服务开启、10 万条、30 天、全部类型）。
func DefaultLogSetting() LogSetting {
	return LogSetting{
		Enabled:       true,
		MaxCount:      DefaultLogMaxCount,
		RetentionDays: DefaultLogRetentionDays,
		LogTypes:      DefaultLogTypes(),
	}
}

// Normalize 归一化设置：零值边界填默认值，日志类型去空格/去重/剔除未知项（副本）。
func (s LogSetting) Normalize() LogSetting {
	out := s
	if out.MaxCount <= 0 {
		out.MaxCount = DefaultLogMaxCount
	}
	if out.RetentionDays <= 0 {
		out.RetentionDays = DefaultLogRetentionDays
	}
	seen := make(map[string]bool, len(out.LogTypes))
	types := make([]string, 0, len(out.LogTypes))
	for _, t := range out.LogTypes {
		t = strings.ToLower(strings.TrimSpace(t))
		if !IsValidLogType(t) || seen[t] {
			continue
		}
		seen[t] = true
		types = append(types, t)
	}
	out.LogTypes = types
	return out
}

// ValidateTypes 校验日志类型：仅允许枚举值；启用日志服务时至少勾选一种。
func (s LogSetting) ValidateTypes() error {
	for _, t := range s.LogTypes {
		if !IsValidLogType(strings.ToLower(strings.TrimSpace(t))) {
			return ErrInvalidLogSetting
		}
	}
	if s.Enabled && len(s.LogTypes) == 0 {
		return ErrInvalidLogSetting
	}
	return nil
}

// Validate 校验设置边界（条数/保留天数/日志类型）。
func (s LogSetting) Validate() error {
	if s.MaxCount < MinLogMaxCount || s.MaxCount > MaxLogMaxCount {
		return ErrInvalidLogSetting
	}
	if s.RetentionDays < MinLogRetentionDays || s.RetentionDays > MaxLogRetentionDays {
		return ErrInvalidLogSetting
	}
	return s.ValidateTypes()
}

// HasType 判断某日志类型是否启用（用于写入前检查）。
func (s LogSetting) HasType(logType string) bool {
	for _, t := range s.LogTypes {
		if t == logType {
			return true
		}
	}
	return false
}

// PurgeResult 日志清理结果（各表删除行数；user_log 为 append-only 审计表，不参与清理）。
type PurgeResult struct {
	RequestLogs int64
	TestLogs    int64
	SystemLogs  int64
}
