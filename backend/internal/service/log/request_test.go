package log

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// fakeRequestStore 内存请求详情仓储（无 DB）：实现 RequestStore 全量方法。
// 查询口径与 repository.QueryRequestLogs 的 SQL 保持一致（时间倒序 + 分页）。
type fakeRequestStore struct {
	setting  *domain.LogSetting
	requests []domain.RequestLog

	appended  [][]domain.RequestLog
	saveCalls int
	purges    []purgeCall
	purgeErr  error
}

// purgeCall 记录一次清理调用的入参（截止时间 + 最大条数）。
type purgeCall struct {
	Cutoff   time.Time
	MaxCount int64
}

func (f *fakeRequestStore) AppendRequestLogs(_ context.Context, entries []domain.RequestLog) error {
	f.appended = append(f.appended, entries)
	f.requests = append(f.requests, entries...)
	return nil
}

func (f *fakeRequestStore) QueryRequestLogs(_ context.Context, q domain.RequestLogQuery) ([]domain.RequestLog, int64, error) {
	out := make([]domain.RequestLog, 0, len(f.requests))
	for _, e := range f.requests {
		if q.TaskID > 0 && e.TaskID != q.TaskID {
			continue
		}
		if q.InterfaceID > 0 && e.InterfaceID != q.InterfaceID {
			continue
		}
		if q.Keyword != "" {
			kw := strings.ToLower(q.Keyword)
			if !strings.Contains(strings.ToLower(e.URL), kw) &&
				!strings.Contains(strings.ToLower(e.ErrorMessage), kw) {
				continue
			}
		}
		if q.StartTime != nil && e.OccurredAt.Before(*q.StartTime) {
			continue
		}
		if q.EndTime != nil && e.OccurredAt.After(*q.EndTime) {
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].OccurredAt.Equal(out[j].OccurredAt) {
			return out[i].OccurredAt.After(out[j].OccurredAt)
		}
		return out[i].ID > out[j].ID
	})
	page, pageSize := q.Page, q.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = DefaultPageSize
	}
	start := (page - 1) * pageSize
	if start > len(out) {
		start = len(out)
	}
	end := start + pageSize
	if end > len(out) {
		end = len(out)
	}
	return out[start:end], int64(len(out)), nil
}

func (f *fakeRequestStore) GetLogSetting(_ context.Context) (*domain.LogSetting, error) {
	if f.setting == nil {
		return nil, nil
	}
	cp := *f.setting
	cp.LogTypes = append([]string(nil), f.setting.LogTypes...)
	return &cp, nil
}

func (f *fakeRequestStore) SaveLogSetting(_ context.Context, s *domain.LogSetting) error {
	f.saveCalls++
	cp := *s
	cp.LogTypes = append([]string(nil), s.LogTypes...)
	f.setting = &cp
	return nil
}

func (f *fakeRequestStore) PurgeLogs(_ context.Context, cutoff time.Time, maxCount int64) (domain.PurgeResult, error) {
	f.purges = append(f.purges, purgeCall{Cutoff: cutoff, MaxCount: maxCount})
	if f.purgeErr != nil {
		return domain.PurgeResult{}, f.purgeErr
	}
	return domain.PurgeResult{RequestLogs: 3, TestLogs: 2, SystemLogs: 1}, nil
}

// newRequestService 构造注入假请求仓储的日志服务（固定时钟，便于断言清理截止时间）。
func newRequestService(store *fakeRequestStore) *Service {
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	svc := NewService(nil).WithRequestStore(store)
	svc.now = func() time.Time { return now }
	return svc
}

// reqEntries 构造 n 条请求明细（URL 递增，便于校验采样保序）。
func reqEntries(n int) []domain.RequestLog {
	out := make([]domain.RequestLog, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, domain.RequestLog{
			InterfaceID:    int64(i%3 + 1),
			Method:         "get",
			URL:            "http://10.0.0.1:8080/api/" + strings.Repeat("x", i%5),
			ResponseStatus: 200,
			DurationMs:     int64(i),
		})
	}
	return out
}

// TestWriteRequestLogsDisabledBySetting 日志服务关闭 → 一条都不写（设置生效于写入）。
func TestWriteRequestLogsDisabledBySetting(t *testing.T) {
	setting := domain.DefaultLogSetting()
	setting.Enabled = false
	store := &fakeRequestStore{setting: &setting}
	svc := newRequestService(store)

	written, err := svc.WriteRequestLogs(context.Background(), 42, reqEntries(5))
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if written != 0 || len(store.appended) != 0 {
		t.Fatalf("日志服务关闭时不应写入: written=%d appended=%d", written, len(store.appended))
	}
}

// TestWriteRequestLogsRespectsLogTypes 未勾选 request 类型 → 不写请求详情。
func TestWriteRequestLogsRespectsLogTypes(t *testing.T) {
	setting := domain.DefaultLogSetting()
	setting.LogTypes = []string{domain.LogTypeUser, domain.LogTypeTest}
	store := &fakeRequestStore{setting: &setting}
	svc := newRequestService(store)

	written, err := svc.WriteRequestLogs(context.Background(), 42, reqEntries(5))
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if written != 0 || len(store.appended) != 0 {
		t.Fatalf("未启用 request 类型时不应写入: written=%d", written)
	}
}

// TestWriteRequestLogsSamplesBeyondLimit 超过单任务上限 → 按比例采样并保序，seq 从 1 递增。
func TestWriteRequestLogsSamplesBeyondLimit(t *testing.T) {
	setting := domain.DefaultLogSetting()
	store := &fakeRequestStore{setting: &setting}
	svc := newRequestService(store)

	written, err := svc.WriteRequestLogs(context.Background(), 42, reqEntries(domain.MaxRequestLogPerTask*3))
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if written != domain.MaxRequestLogPerTask {
		t.Fatalf("采样后条数 = %d, want %d", written, domain.MaxRequestLogPerTask)
	}
	saved := store.appended[0]
	if len(saved) != domain.MaxRequestLogPerTask {
		t.Fatalf("落库条数 = %d, want %d", len(saved), domain.MaxRequestLogPerTask)
	}
	if saved[0].Seq != 1 || saved[len(saved)-1].Seq != int64(domain.MaxRequestLogPerTask) {
		t.Fatalf("seq 应从 1 递增到上限: first=%d last=%d", saved[0].Seq, saved[len(saved)-1].Seq)
	}
	for i := 1; i < len(saved); i++ {
		if saved[i].Seq <= saved[i-1].Seq {
			t.Fatalf("采样后 seq 必须递增: idx=%d", i)
		}
	}
	for _, e := range saved {
		if e.TaskID != 42 {
			t.Fatalf("写入前应固化 task_id, got %d", e.TaskID)
		}
		if e.OccurredAt.IsZero() {
			t.Fatal("缺失发生时间时应回填当前时间")
		}
	}
}

// TestWriteRequestLogsSanitizesAndTruncates 请求体/响应体脱敏 + 截断（DES-I-001）。
func TestWriteRequestLogsSanitizesAndTruncates(t *testing.T) {
	setting := domain.DefaultLogSetting()
	store := &fakeRequestStore{setting: &setting}
	svc := newRequestService(store)

	long := strings.Repeat("a", domain.MaxLogBodyBytes*2)
	entries := []domain.RequestLog{{
		URL:            "http://10.0.0.1/api/login?password=hunter2",
		RequestBody:    `{"password":"hunter2","Authorization":"Bearer abc.def"}`,
		ResponseBody:   long,
		ErrorMessage:   "Authorization: Bearer leaked-token",
		ResponseStatus: 500,
		Method:         "post",
	}}

	if _, err := svc.WriteRequestLogs(context.Background(), 7, entries); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	saved := store.appended[0][0]
	if strings.Contains(saved.RequestBody, "hunter2") || strings.Contains(saved.RequestBody, "abc.def") {
		t.Fatalf("请求体未脱敏: %s", saved.RequestBody)
	}
	if strings.Contains(saved.ErrorMessage, "leaked-token") {
		t.Fatalf("错误信息未脱敏: %s", saved.ErrorMessage)
	}
	if len(saved.ResponseBody) > domain.MaxLogBodyBytes {
		t.Fatalf("响应体未截断: len=%d", len(saved.ResponseBody))
	}
	if saved.Method != "POST" {
		t.Fatalf("方法应大写归一化, got %s", saved.Method)
	}
}

// TestWriteRequestLogsSkipsEmpty 空明细（无 URL 且无错误）不落库。
func TestWriteRequestLogsSkipsEmpty(t *testing.T) {
	setting := domain.DefaultLogSetting()
	store := &fakeRequestStore{setting: &setting}
	svc := newRequestService(store)

	written, err := svc.WriteRequestLogs(context.Background(), 1, []domain.RequestLog{{}, {}})
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if written != 0 || len(store.appended) != 0 {
		t.Fatalf("空明细不应落库: written=%d", written)
	}
}

// TestListRequestLogsValidation 时间范围非法 → 25001；负数 ID → 10003；分页归一化。
func TestListRequestLogsValidation(t *testing.T) {
	store := &fakeRequestStore{setting: ptrSetting(domain.DefaultLogSetting())}
	svc := newRequestService(store)

	start := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	end := start.Add(-time.Hour)
	_, err := svc.ListRequestLogs(context.Background(), domain.RequestLogQuery{
		LogQuery: domain.LogQuery{StartTime: &start, EndTime: &end},
	})
	if !errors.Is(err, ErrInvalidTimeRange) {
		t.Fatalf("时间范围非法应返回 25001, got %v", err)
	}

	_, err = svc.ListRequestLogs(context.Background(), domain.RequestLogQuery{InterfaceID: -1})
	if !errors.Is(err, errcode.ErrParamFmt) {
		t.Fatalf("负数 interface_id 应返回参数非法, got %v", err)
	}

	page, err := svc.ListRequestLogs(context.Background(), domain.RequestLogQuery{TaskID: 42})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if page.Page != 1 || page.PageSize != DefaultPageSize {
		t.Fatalf("分页默认值异常: page=%d pageSize=%d", page.Page, page.PageSize)
	}
	if page.List == nil {
		t.Fatal("空结果应返回空数组而非 nil")
	}
}

// TestListRequestLogsFilters 按任务/接口/关键字过滤 + 时间倒序。
func TestListRequestLogsFilters(t *testing.T) {
	base := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	store := &fakeRequestStore{
		setting: ptrSetting(domain.DefaultLogSetting()),
		requests: []domain.RequestLog{
			{ID: 1, TaskID: 42, InterfaceID: 5, URL: "http://h/a", OccurredAt: base},
			{ID: 2, TaskID: 42, InterfaceID: 6, URL: "http://h/b", ErrorMessage: "timeout", OccurredAt: base.Add(time.Second)},
			{ID: 3, TaskID: 43, InterfaceID: 5, URL: "http://h/c", OccurredAt: base.Add(2 * time.Second)},
		},
	}
	svc := newRequestService(store)

	page, err := svc.ListRequestLogs(context.Background(), domain.RequestLogQuery{TaskID: 42})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if page.Total != 2 || len(page.List) != 2 || page.List[0].ID != 2 {
		t.Fatalf("按任务过滤/时间倒序异常: %+v", page.List)
	}

	page, err = svc.ListRequestLogs(context.Background(), domain.RequestLogQuery{InterfaceID: 5})
	if err != nil || page.Total != 2 {
		t.Fatalf("按接口过滤异常: total=%d err=%v", page.Total, err)
	}

	page, err = svc.ListRequestLogs(context.Background(), domain.RequestLogQuery{
		LogQuery: domain.LogQuery{Keyword: "TIMEOUT"},
	})
	if err != nil || page.Total != 1 || page.List[0].ID != 2 {
		t.Fatalf("关键字过滤（错误信息）异常: %+v err=%v", page.List, err)
	}
}

// TestGetLogSettingDefaults 仓储无记录 → 返回默认设置（不报错）。
func TestGetLogSettingDefaults(t *testing.T) {
	svc := newRequestService(&fakeRequestStore{})

	got, err := svc.GetLogSetting(context.Background())
	if err != nil {
		t.Fatalf("读取设置失败: %v", err)
	}
	want := domain.DefaultLogSetting()
	if got.Enabled != want.Enabled || got.MaxCount != want.MaxCount ||
		got.RetentionDays != want.RetentionDays || len(got.LogTypes) != len(want.LogTypes) {
		t.Fatalf("默认设置不一致: %+v", got)
	}
}

// TestUpdateLogSettingRejectsInvalid 非法设置 → 10003 且不落库、不清理。
func TestUpdateLogSettingRejectsInvalid(t *testing.T) {
	store := &fakeRequestStore{setting: ptrSetting(domain.DefaultLogSetting())}
	svc := newRequestService(store)

	bad := domain.DefaultLogSetting()
	bad.MaxCount = 10
	if _, err := svc.UpdateLogSetting(context.Background(), bad); !errors.Is(err, errcode.ErrParamFmt) {
		t.Fatalf("非法设置应返回参数非法, got %v", err)
	}
	if store.saveCalls != 0 || len(store.purges) != 0 {
		t.Fatalf("非法设置不应落库/清理: save=%d purge=%d", store.saveCalls, len(store.purges))
	}

	badTypes := domain.DefaultLogSetting()
	badTypes.LogTypes = []string{"unknown"}
	if _, err := svc.UpdateLogSetting(context.Background(), badTypes); !errors.Is(err, errcode.ErrParamFmt) {
		t.Fatalf("未知日志类型应返回参数非法, got %v", err)
	}
}

// TestUpdateLogSettingSavesAndPurges 更新设置：落库 + 立即触发一次清理（按新保留天数/条数）。
func TestUpdateLogSettingSavesAndPurges(t *testing.T) {
	store := &fakeRequestStore{setting: ptrSetting(domain.DefaultLogSetting())}
	svc := newRequestService(store)

	in := domain.LogSetting{Enabled: true, MaxCount: 5000, RetentionDays: 7,
		LogTypes: []string{domain.LogTypeRequest, domain.LogTypeTest, "request"}}
	got, err := svc.UpdateLogSetting(context.Background(), in)
	if err != nil {
		t.Fatalf("更新失败: %v", err)
	}
	if store.saveCalls != 1 {
		t.Fatalf("应落库一次, got %d", store.saveCalls)
	}
	if len(got.LogTypes) != 2 {
		t.Fatalf("日志类型应去重: %v", got.LogTypes)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("应写入更新时间")
	}
	if len(store.purges) != 1 {
		t.Fatalf("更新后应触发一次清理, got %d", len(store.purges))
	}
	wantCutoff := got.UpdatedAt.AddDate(0, 0, -7)
	if !store.purges[0].Cutoff.Equal(wantCutoff) {
		t.Fatalf("清理截止时间 = %v, want %v", store.purges[0].Cutoff, wantCutoff)
	}
	if store.purges[0].MaxCount != 5000 {
		t.Fatalf("清理最大条数 = %d, want 5000", store.purges[0].MaxCount)
	}
}

// TestPurgeLogsUsesSetting 手动清理沿用当前设置的保留天数与最大条数。
func TestPurgeLogsUsesSetting(t *testing.T) {
	setting := domain.LogSetting{Enabled: true, MaxCount: 2000, RetentionDays: 3,
		LogTypes: []string{domain.LogTypeRequest}}
	store := &fakeRequestStore{setting: &setting}
	svc := newRequestService(store)

	res, err := svc.PurgeLogs(context.Background())
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if len(store.purges) != 1 {
		t.Fatalf("应调用一次清理, got %d", len(store.purges))
	}
	if res.RequestLogs != 3 || res.TestLogs != 2 || res.SystemLogs != 1 {
		t.Fatalf("清理结果未透传: %+v", res)
	}
}

// ptrSetting 返回设置指针（避免共享同一份可变结构）。
func ptrSetting(s domain.LogSetting) *domain.LogSetting { return &s }
