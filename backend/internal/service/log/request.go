// 请求详情日志与日志设置（PRD §3.5 / api-contract §2.6）。
//
// 职责：
//   - WriteRequestLogs：压测结束后批量写入逐请求明细。写入前检查日志设置（服务开关 +
//     request 类型），并按单任务上限做按比例采样（数据量大，避免打爆存储）；
//   - ListRequestLogs：按任务/接口/关键字/时间范围分页查询；
//   - Get/UpdateLogSetting：日志服务配置读写，更新后立即触发一次过期/超量清理；
//   - 脱敏与截断统一在本层完成（DES-I-001：请求体/响应体 4KB 上限 + logutil 过滤）。
package log

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
	"auto_performance/internal/logutil"
)

// 请求详情字段落库长度上限（列宽保护，正文另受 MaxLogBodyBytes 约束）。
const (
	maxMethodLen       = 16
	maxURLLen          = 2048
	maxErrorCodeLen    = 64
	maxErrorMessageLen = 512
)

// RequestStore 请求详情与日志设置仓储能力（由 repository.PGRepository 实现）。
type RequestStore interface {
	// AppendRequestLogs 批量写入请求详情。
	AppendRequestLogs(ctx context.Context, entries []domain.RequestLog) error
	// QueryRequestLogs 查询请求详情（任务/接口/关键字/时间范围 + 分页）。
	QueryRequestLogs(ctx context.Context, q domain.RequestLogQuery) ([]domain.RequestLog, int64, error)
	// GetLogSetting 读取日志设置（无记录返回 nil, nil）。
	GetLogSetting(ctx context.Context) (*domain.LogSetting, error)
	// SaveLogSetting 保存日志设置（单行 upsert）。
	SaveLogSetting(ctx context.Context, setting *domain.LogSetting) error
	// PurgeLogs 按保留策略清理日志，返回各表删除行数。
	PurgeLogs(ctx context.Context, cutoff time.Time, maxCount int64) (domain.PurgeResult, error)
}

// RequestLogPage 请求详情分页结果。
type RequestLogPage struct {
	List     []domain.RequestLog
	Total    int64
	Page     int
	PageSize int
}

// WithRequestStore 注入请求详情/日志设置仓储（nil 时相关能力降级为依赖异常/静默跳过）。
func (s *Service) WithRequestStore(store RequestStore) *Service {
	if s != nil {
		s.request = store
	}
	return s
}

// WriteRequestLogs 批量写入请求详情，返回实际落库条数。
//
// 生效规则（PRD §3.5：设置生效于日志写入）：
//   - 日志服务关闭或未勾选 request 类型 → 不写（返回 0，不报错）；
//   - 单任务超过上限（domain.MaxRequestLogPerTask 与 max_count 取小）→ 按比例采样保序；
//   - 请求体/响应体脱敏 + 截断，空明细（无 URL 且无错误）跳过。
func (s *Service) WriteRequestLogs(ctx context.Context, taskID int64, entries []domain.RequestLog) (int, error) {
	if s == nil || s.request == nil || len(entries) == 0 {
		return 0, nil
	}
	if taskID <= 0 {
		return 0, errcode.ErrParamFmt
	}
	setting, err := s.resolveSetting(ctx)
	if err != nil {
		return 0, err
	}
	if !setting.Enabled || !setting.HasType(domain.LogTypeRequest) {
		return 0, nil
	}

	prepared := prepareRequestLogs(taskID, entries, s.nowTime(), requestLogLimit(*setting))
	if len(prepared) == 0 {
		return 0, nil
	}
	if err := s.request.AppendRequestLogs(ctx, prepared); err != nil {
		return 0, err
	}
	return len(prepared), nil
}

// prepareRequestLogs 采样 → 补全任务/序号/时间 → 脱敏截断（保序）。
func prepareRequestLogs(taskID int64, entries []domain.RequestLog, now time.Time, limit int) []domain.RequestLog {
	valid := make([]domain.RequestLog, 0, len(entries))
	for _, e := range entries {
		if e.HasContent() {
			valid = append(valid, e)
		}
	}
	valid = sampleRequestLogs(valid, limit)

	out := make([]domain.RequestLog, 0, len(valid))
	for i := range valid {
		e := valid[i]
		e.TaskID = taskID
		e.Seq = int64(i + 1)
		if e.OccurredAt.IsZero() {
			e.OccurredAt = now
		}
		e.Method = truncateText(strings.ToUpper(strings.TrimSpace(e.Method)), maxMethodLen)
		e.URL = truncateText(logutil.SanitizeLog(e.URL), maxURLLen)
		e.RequestBody = truncateText(logutil.SanitizeLog(e.RequestBody), domain.MaxLogBodyBytes)
		e.ResponseBody = truncateText(logutil.SanitizeLog(e.ResponseBody), domain.MaxLogBodyBytes)
		e.ErrorCode = truncateText(logutil.SanitizeLog(e.ErrorCode), maxErrorCodeLen)
		e.ErrorMessage = truncateText(logutil.SanitizeLog(e.ErrorMessage), maxErrorMessageLen)
		if e.ResponseStatus < 0 {
			e.ResponseStatus = 0
		}
		if e.DurationMs < 0 {
			e.DurationMs = 0
		}
		out = append(out, e)
	}
	return out
}

// sampleRequestLogs 按比例（系统采样）截断到上限，保留首条并保序。
//
// 例：3000 条 / 上限 1000 → 每 3 条取 1 条，仍覆盖整个压测时段；
// 相比「只保留前 N 条」可避免只看到压测初期的请求。
func sampleRequestLogs(entries []domain.RequestLog, limit int) []domain.RequestLog {
	if limit <= 0 || len(entries) <= limit {
		return entries
	}
	out := make([]domain.RequestLog, 0, limit)
	total := int64(len(entries))
	for i := int64(0); i < int64(limit); i++ {
		idx := int(i * total / int64(limit))
		out = append(out, entries[idx])
	}
	return out
}

// truncateText 按字节上限截断，且不切断多字节字符（避免非法 UTF-8 落库失败）。
func truncateText(s string, maxBytes int) string {
	if maxBytes <= 0 || len(s) <= maxBytes {
		return s
	}
	var b strings.Builder
	b.Grow(maxBytes)
	for _, r := range s {
		if b.Len()+utf8.RuneLen(r) > maxBytes {
			break
		}
		b.WriteRune(r)
	}
	return b.String()
}

// requestLogLimit 单任务入库上限：min(日志设置 max_count, domain.MaxRequestLogPerTask)。
func requestLogLimit(setting domain.LogSetting) int {
	limit := domain.MaxRequestLogPerTask
	if setting.MaxCount > 0 && setting.MaxCount < int64(limit) {
		limit = int(setting.MaxCount)
	}
	return limit
}

// ListRequestLogs 请求详情查询（任务/接口/关键字/时间范围 + 分页）。
func (s *Service) ListRequestLogs(ctx context.Context, q domain.RequestLogQuery) (*RequestLogPage, error) {
	if s == nil || s.request == nil {
		return nil, errcode.ErrDependency
	}
	_, page, pageSize, err := normalizeLogQuery(&q.LogQuery)
	if err != nil {
		return nil, err
	}
	if q.TaskID < 0 || q.InterfaceID < 0 {
		return nil, errcode.ErrParamFmt
	}
	q.Page, q.PageSize = page, pageSize

	list, total, err := s.request.QueryRequestLogs(ctx, q)
	if err != nil {
		return nil, err
	}
	if list == nil {
		list = []domain.RequestLog{}
	}
	return &RequestLogPage{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// GetLogSetting 读取日志设置；仓储无记录时返回默认值（服务开启/10 万条/30 天/全部类型）。
func (s *Service) GetLogSetting(ctx context.Context) (*domain.LogSetting, error) {
	if s == nil || s.request == nil {
		return nil, errcode.ErrDependency
	}
	return s.resolveSetting(ctx)
}

// UpdateLogSetting 保存日志设置，并立即按新策略清理一次过期/超量日志。
//
// 校验失败 → 10003；清理失败不回滚设置（仅记录进程日志），避免因存储抖动丢配置。
func (s *Service) UpdateLogSetting(ctx context.Context, in domain.LogSetting) (*domain.LogSetting, error) {
	if s == nil || s.request == nil {
		return nil, errcode.ErrDependency
	}
	if err := in.ValidateTypes(); err != nil {
		return nil, errcode.ErrParamFmt
	}
	normalized := in.Normalize()
	if err := normalized.Validate(); err != nil {
		return nil, errcode.ErrParamFmt
	}
	normalized.UpdatedAt = s.nowTime()

	if err := s.request.SaveLogSetting(ctx, &normalized); err != nil {
		return nil, err
	}
	if _, err := s.purgeWith(ctx, normalized); err != nil {
		fmt.Fprintf(os.Stderr, "[log] 更新日志设置后清理失败: %v\n", err)
	}
	return &normalized, nil
}

// PurgeLogs 按当前日志设置清理日志（供设置更新、启动自检与运维手动触发）。
func (s *Service) PurgeLogs(ctx context.Context) (domain.PurgeResult, error) {
	if s == nil || s.request == nil {
		return domain.PurgeResult{}, errcode.ErrDependency
	}
	setting, err := s.resolveSetting(ctx)
	if err != nil {
		return domain.PurgeResult{}, err
	}
	return s.purgeWith(ctx, *setting)
}

// resolveSetting 读取日志设置，缺失时套用默认值（仓储不可用不算失败）。
func (s *Service) resolveSetting(ctx context.Context) (*domain.LogSetting, error) {
	stored, err := s.request.GetLogSetting(ctx)
	if err != nil {
		return nil, err
	}
	if stored == nil {
		setting := domain.DefaultLogSetting()
		return &setting, nil
	}
	setting := stored.Normalize()
	return &setting, nil
}

// purgeWith 以设置中的保留策略执行清理（截止时间 = 当前时间 - retention_days）。
func (s *Service) purgeWith(ctx context.Context, setting domain.LogSetting) (domain.PurgeResult, error) {
	if s.request == nil {
		return domain.PurgeResult{}, nil
	}
	cutoff := s.nowTime().AddDate(0, 0, -setting.RetentionDays)
	return s.request.PurgeLogs(ctx, cutoff, setting.MaxCount)
}
