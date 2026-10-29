package domain

import "time"

// 日志结果枚举（user_log.result）。
const (
	AuditResultOK   = "ok"
	AuditResultFail = "fail"
)

// 日志级别枚举（system_log.level / test_log.level；user_log 无 level 列，由 result 推导）。
const (
	LogLevelInfo  = "info"
	LogLevelWarn  = "warn"
	LogLevelError = "error"
)

// 系统日志事件枚举（system_log.event）：服务启动/重启/停止。
const (
	SystemEventStart   = "start"
	SystemEventRestart = "restart"
	SystemEventStop    = "stop"
)

// Admin 状态枚举（admin.status）。
const (
	AdminStatusEnabled  = "enabled"
	AdminStatusDisabled = "disabled"
)

// AuditLog 用户日志（user_log）条目，承载 5W1H 审计字段与 HMAC 链（INV-8 / DES-T-006）。
//
// 审计表为 append-only：应用层只追加，DB 触发器禁止 UPDATE/DELETE（000002_audit_trigger）。
// Hash = HMAC-SHA256(PrevHash || entryJSON)，详见 internal/service/log.AppendAudit。
type AuditLog struct {
	ID           int64
	AdminID      *int64 // 操作人（未认证请求为 nil）
	Action       string // 动作枚举，如 login/create/update/delete/start
	ResourceType string // 资源类型，如 environment/task
	ResourceID   string
	// AdminUsername 操作人登录名：仅查询时联表 admin 填充，不落 user_log（审计表只存 admin_id）。
	AdminUsername string
	Detail        string // 已脱敏的 5W1H 细节（method/path/status/cost）
	IP            string
	Result        string // ok | fail
	PrevHash      []byte // 上一条审计 hash（链式签名输入）
	Hash          []byte // 本条 HMAC-SHA256
	CreatedAt     time.Time
}

// TestLog 测试执行日志（test_log）条目：按任务定位，seq 为任务内递增序号。
type TestLog struct {
	ID        int64
	TaskID    int64
	Seq       int64
	Level     string
	Message   string
	CreatedAt time.Time
}

// SystemLog 系统日志（system_log）条目：服务运行状态（启动/重启/停止）。
type SystemLog struct {
	ID        int64
	Level     string // info | warn | error
	Event     string // start | restart | stop
	Message   string
	CreatedAt time.Time
}

// LogQuery 三类日志共用的查询条件（时间范围/关键字/级别 + 分页）。
type LogQuery struct {
	Level     string
	Keyword   string
	StartTime *time.Time
	EndTime   *time.Time
	Page      int
	PageSize  int
}

// UserLogQuery 用户日志查询条件（通用条件 + 操作人/动作/关联对象）。
type UserLogQuery struct {
	LogQuery
	AdminID      int64
	Action       string
	ResourceType string
	ResourceID   string
	// Result 由 Level 映射而来（user_log 无 level 列）：info→ok、error→fail。
	// 为空表示不按级别过滤；warn 在用户日志中无对应结果，由服务层直接返回空集。
	Result string
}

// SystemLogQuery 系统日志查询条件（通用条件 + 事件）。
type SystemLogQuery struct {
	LogQuery
	Event string
}

// TestLogQuery 测试日志查询条件（通用条件 + 任务定位）。
type TestLogQuery struct {
	LogQuery
	TaskID int64
}

// IsValidLogLevel 校验日志级别枚举（info/warn/error）。
func IsValidLogLevel(level string) bool {
	switch level {
	case LogLevelInfo, LogLevelWarn, LogLevelError:
		return true
	default:
		return false
	}
}

// IsValidSystemEvent 校验系统日志事件枚举（start/restart/stop）。
func IsValidSystemEvent(event string) bool {
	switch event {
	case SystemEventStart, SystemEventRestart, SystemEventStop:
		return true
	default:
		return false
	}
}

// LogLevelOfResult 用户日志 result → 对外 level（ok→info，fail→error）。
func LogLevelOfResult(result string) string {
	if result == AuditResultFail {
		return LogLevelError
	}
	return LogLevelInfo
}

// UserLogResultForLevel 用户日志 level → result 过滤值。
//
// 第二个返回值为 false 表示该级别在用户日志中必然无匹配（warn：用户日志只有 ok/fail）。
func UserLogResultForLevel(level string) (string, bool) {
	switch level {
	case LogLevelInfo:
		return AuditResultOK, true
	case LogLevelError:
		return AuditResultFail, true
	default:
		return "", false
	}
}
