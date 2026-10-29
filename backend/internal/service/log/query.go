// 日志查询与系统日志写入（用户/系统/测试日志，PRD §3.5 / api-contract §2.6）。
//
// 约定：
//   - 日志接口只读：本文件只提供查询与「系统运行事件」写入，不含任何 UPDATE/DELETE 入口
//     （审计表 append-only，INV-8 / DES-T-006）；
//   - 查询时间范围 start_time > end_time → 25001；级别/事件非法 → 参数格式错误；
//   - 系统日志写库前统一脱敏（DES-I-001），与用户日志/测试日志同一过滤器。
package log

import (
	"context"
	"fmt"
	"strings"
	"time"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
	"auto_performance/internal/logutil"
)

const (
	// DefaultPageSize 日志列表默认分页大小（api-contract §1.3）。
	DefaultPageSize = 20
	// MaxPageSize 日志列表分页上限（防止单次拉取过大）。
	MaxPageSize = 200
	// maxKeywordLen 关键字长度上限（超长截断，避免无效宽表扫描）。
	maxKeywordLen = 128
)

// QueryStore 日志查询与系统日志写入仓储能力（由 repository.PGRepository 实现）。
// 契约：只提供查询与追加，不提供 UPDATE/DELETE（INV-8）。
type QueryStore interface {
	// QueryUserLogs 查询用户日志（联表填充操作人登录名）。
	QueryUserLogs(ctx context.Context, q domain.UserLogQuery) ([]domain.AuditLog, int64, error)
	// QuerySystemLogs 查询系统日志。
	QuerySystemLogs(ctx context.Context, q domain.SystemLogQuery) ([]domain.SystemLog, int64, error)
	// QueryTestLogs 查询测试日志（task_id 命中时按 seq 升序，便于增量回溯）。
	QueryTestLogs(ctx context.Context, q domain.TestLogQuery) ([]domain.TestLog, int64, error)
	// AppendSystemLog 追加一条系统日志。
	AppendSystemLog(ctx context.Context, entry *domain.SystemLog) error
	// LastSystemEvent 返回最近一条系统日志事件（无记录返回 ""）；用于判定是否为重启。
	LastSystemEvent(ctx context.Context) (string, error)
}

// UserLogPage 用户日志分页结果。
type UserLogPage struct {
	List     []domain.AuditLog
	Total    int64
	Page     int
	PageSize int
}

// SystemLogPage 系统日志分页结果。
type SystemLogPage struct {
	List     []domain.SystemLog
	Total    int64
	Page     int
	PageSize int
}

// TestLogPage 测试日志分页结果。
type TestLogPage struct {
	List     []domain.TestLog
	Total    int64
	Page     int
	PageSize int
}

// WithQueryStore 注入日志查询仓储（返回自身便于链式装配；为 nil 时查询降级为依赖异常）。
func (s *Service) WithQueryStore(store QueryStore) *Service {
	if s != nil {
		s.query = store
	}
	return s
}

// ListUserLogs 用户日志查询（操作人/动作/关联对象/级别/关键字/时间范围 + 分页）。
//
// level 在 user_log 中无对应列，按 result 映射：info→ok、error→fail；warn 必然无匹配。
func (s *Service) ListUserLogs(ctx context.Context, q domain.UserLogQuery) (*UserLogPage, error) {
	if s == nil || s.query == nil {
		return nil, errcode.ErrDependency
	}
	level, page, pageSize, err := normalizeLogQuery(&q.LogQuery)
	if err != nil {
		return nil, err
	}
	if q.AdminID < 0 {
		return nil, errcode.ErrParamFmt
	}

	if level == domain.LogLevelWarn {
		// 用户日志只有 ok/fail 两种结果，warn 级别不可能命中：直接返回空集（不下推无意义查询）。
		return &UserLogPage{List: []domain.AuditLog{}, Page: page, PageSize: pageSize}, nil
	}
	if level != "" {
		q.Result, _ = domain.UserLogResultForLevel(level)
	}

	q.Action = strings.TrimSpace(q.Action)
	q.ResourceType = strings.TrimSpace(q.ResourceType)
	q.ResourceID = strings.TrimSpace(q.ResourceID)
	q.Page, q.PageSize = page, pageSize

	list, total, err := s.query.QueryUserLogs(ctx, q)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []domain.AuditLog{}
	}
	return &UserLogPage{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// ListSystemLogs 系统日志查询（事件/级别/关键字/时间范围 + 分页）。
func (s *Service) ListSystemLogs(ctx context.Context, q domain.SystemLogQuery) (*SystemLogPage, error) {
	if s == nil || s.query == nil {
		return nil, errcode.ErrDependency
	}
	_, page, pageSize, err := normalizeLogQuery(&q.LogQuery)
	if err != nil {
		return nil, err
	}
	event := strings.ToLower(strings.TrimSpace(q.Event))
	if event != "" && !domain.IsValidSystemEvent(event) {
		return nil, errcode.ErrParamFmt
	}
	q.Event = event
	q.Page, q.PageSize = page, pageSize

	list, total, err := s.query.QuerySystemLogs(ctx, q)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []domain.SystemLog{}
	}
	return &SystemLogPage{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// ListTestLogs 测试日志查询（task_id 定位 + 级别/关键字/时间范围 + 分页）。
func (s *Service) ListTestLogs(ctx context.Context, q domain.TestLogQuery) (*TestLogPage, error) {
	if s == nil || s.query == nil {
		return nil, errcode.ErrDependency
	}
	_, page, pageSize, err := normalizeLogQuery(&q.LogQuery)
	if err != nil {
		return nil, err
	}
	if q.TaskID < 0 {
		return nil, errcode.ErrParamFmt
	}
	q.Page, q.PageSize = page, pageSize

	list, total, err := s.query.QueryTestLogs(ctx, q)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []domain.TestLog{}
	}
	return &TestLogPage{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// RecordSystemEvent 追加一条系统日志（启动/重启/停止、运行异常）。
//
// 未接仓储时静默降级（不阻断服务启动）；message 写库前统一脱敏（DES-I-001）。
func (s *Service) RecordSystemEvent(ctx context.Context, level, event, message string) error {
	level = strings.ToLower(strings.TrimSpace(level))
	if level == "" {
		level = domain.LogLevelInfo
	}
	if !domain.IsValidLogLevel(level) {
		return errcode.ErrParamFmt
	}
	event = strings.ToLower(strings.TrimSpace(event))
	if event != "" && !domain.IsValidSystemEvent(event) {
		return errcode.ErrParamFmt
	}
	if s == nil || s.query == nil {
		return nil
	}

	entry := &domain.SystemLog{
		Level:     level,
		Event:     truncate(event, 64),
		Message:   logutil.SanitizeLog(message),
		CreatedAt: s.nowTime(),
	}
	if err := s.query.AppendSystemLog(ctx, entry); err != nil {
		return fmt.Errorf("写入系统日志失败: %w", err)
	}
	return nil
}

// RecordStartup 记录服务启动（PRD §3.5：系统日志覆盖启动/重启/异常）。
//
// 判定规则：最近一条系统日志若为 start/restart（缺少 stop 记录，说明上次进程异常退出），
// 本次按 restart + warn 记录；否则按正常 start + info 记录。
func (s *Service) RecordStartup(ctx context.Context, version string) error {
	if s == nil || s.query == nil {
		return nil
	}
	last, err := s.query.LastSystemEvent(ctx)
	if err != nil {
		return fmt.Errorf("读取系统日志链尾失败: %w", err)
	}

	event, level := domain.SystemEventStart, domain.LogLevelInfo
	message := fmt.Sprintf("服务启动 v%s", version)
	if last == domain.SystemEventStart || last == domain.SystemEventRestart {
		event, level = domain.SystemEventRestart, domain.LogLevelWarn
		message = fmt.Sprintf("检测到上次运行未正常退出（缺少 stop 记录），按重启记录 v%s", version)
	}
	return s.RecordSystemEvent(ctx, level, event, message)
}

// RecordShutdown 记录服务正常停止。
func (s *Service) RecordShutdown(ctx context.Context, version string) error {
	return s.RecordSystemEvent(ctx, domain.LogLevelInfo, domain.SystemEventStop,
		fmt.Sprintf("服务正常停止 v%s", version))
}

// normalizeLogQuery 归一化通用查询条件：级别枚举 + 时间范围 + 分页。
// 返回归一化后的 level 与分页值；时间范围非法 → 25001。
func normalizeLogQuery(q *domain.LogQuery) (level string, page, pageSize int, err error) {
	level = strings.ToLower(strings.TrimSpace(q.Level))
	if level != "" && !domain.IsValidLogLevel(level) {
		return "", 0, 0, errcode.ErrParamFmt
	}
	if q.StartTime != nil && q.EndTime != nil && q.StartTime.After(*q.EndTime) {
		return "", 0, 0, ErrInvalidTimeRange
	}
	if q.Keyword != "" {
		q.Keyword = truncate(strings.TrimSpace(q.Keyword), maxKeywordLen)
	}
	q.Level = level
	page, pageSize = normalizePaging(q.Page, q.PageSize)
	return level, page, pageSize, nil
}

// normalizePaging 分页归一化：page 最小 1，pageSize 缺省 20、上限 200。
func normalizePaging(page, pageSize int) (int, int) {
	if page < 1 {
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

// nowTime 取当前时间（测试可注入时钟）。
func (s *Service) nowTime() time.Time {
	if s != nil && s.now != nil {
		return s.now()
	}
	return time.Now()
}
