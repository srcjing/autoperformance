package log

import (
	"bytes"
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"auto_performance/internal/domain"
)

// fakeQueryStore 内存日志仓储（无 DB）：实现 QueryStore 全量方法。
// 过滤/排序/分页语义与 repository.Query* 的 SQL 保持同一口径（时间倒序、task 内 seq 升序）。
type fakeQueryStore struct {
	users   []domain.AuditLog
	systems []domain.SystemLog
	tests   []domain.TestLog

	// lastEvent 模拟 system_log 链尾事件；lastEventErr 模拟仓储异常。
	lastEvent    string
	lastEventErr error
	// appended 记录 AppendSystemLog 落库的条目。
	appended []domain.SystemLog
}

func (f *fakeQueryStore) QueryUserLogs(_ context.Context, q domain.UserLogQuery) ([]domain.AuditLog, int64, error) {
	out := make([]domain.AuditLog, 0, len(f.users))
	for _, e := range f.users {
		if q.AdminID > 0 && (e.AdminID == nil || *e.AdminID != q.AdminID) {
			continue
		}
		if q.Action != "" && e.Action != q.Action {
			continue
		}
		if q.ResourceType != "" && e.ResourceType != q.ResourceType {
			continue
		}
		if q.ResourceID != "" && e.ResourceID != q.ResourceID {
			continue
		}
		if q.Result != "" && e.Result != q.Result {
			continue
		}
		if q.Keyword != "" && !strings.Contains(strings.ToLower(e.Detail), strings.ToLower(q.Keyword)) {
			continue
		}
		if !inRange(e.CreatedAt, q.StartTime, q.EndTime) {
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	return pageOf(out, q.Page, q.PageSize), int64(len(out)), nil
}

func (f *fakeQueryStore) QuerySystemLogs(_ context.Context, q domain.SystemLogQuery) ([]domain.SystemLog, int64, error) {
	out := make([]domain.SystemLog, 0, len(f.systems))
	for _, e := range f.systems {
		if q.Level != "" && e.Level != q.Level {
			continue
		}
		if q.Event != "" && e.Event != q.Event {
			continue
		}
		if q.Keyword != "" && !strings.Contains(strings.ToLower(e.Message), strings.ToLower(q.Keyword)) {
			continue
		}
		if !inRange(e.CreatedAt, q.StartTime, q.EndTime) {
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	return pageOf(out, q.Page, q.PageSize), int64(len(out)), nil
}

func (f *fakeQueryStore) QueryTestLogs(_ context.Context, q domain.TestLogQuery) ([]domain.TestLog, int64, error) {
	out := make([]domain.TestLog, 0, len(f.tests))
	for _, e := range f.tests {
		if q.TaskID > 0 && e.TaskID != q.TaskID {
			continue
		}
		if q.Level != "" && e.Level != q.Level {
			continue
		}
		if q.Keyword != "" && !strings.Contains(strings.ToLower(e.Message), strings.ToLower(q.Keyword)) {
			continue
		}
		if !inRange(e.CreatedAt, q.StartTime, q.EndTime) {
			continue
		}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if q.TaskID > 0 && out[i].Seq != out[j].Seq {
			return out[i].Seq < out[j].Seq
		}
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.After(out[j].CreatedAt)
		}
		return out[i].ID > out[j].ID
	})
	return pageOf(out, q.Page, q.PageSize), int64(len(out)), nil
}

func (f *fakeQueryStore) AppendSystemLog(_ context.Context, entry *domain.SystemLog) error {
	f.appended = append(f.appended, *entry)
	f.systems = append(f.systems, *entry)
	f.lastEvent = entry.Event
	return nil
}

func (f *fakeQueryStore) LastSystemEvent(_ context.Context) (string, error) {
	return f.lastEvent, f.lastEventErr
}

// inRange 时间范围过滤（nil 表示不限）。
func inRange(ts time.Time, from, to *time.Time) bool {
	if from != nil && ts.Before(*from) {
		return false
	}
	if to != nil && ts.After(*to) {
		return false
	}
	return true
}

// pageOf 分页切片（Page/PageSize 已由服务层归一化）。
func pageOf[T any](list []T, page, pageSize int) []T {
	if pageSize <= 0 {
		return list
	}
	start := (page - 1) * pageSize
	if start >= len(list) || start < 0 {
		return []T{}
	}
	end := start + pageSize
	if end > len(list) {
		end = len(list)
	}
	return list[start:end]
}

// TestSanitizeLog 日志写入前统一脱敏（DES-I-001）：用户日志 + 系统日志均不落明文凭据。
func TestSanitizeLog(t *testing.T) {
	ctx := context.Background()

	// 1) 用户日志（审计链）：detail 内的 Authorization / password / 私钥特征被剥离。
	auditStore := &memStore{}
	if _, err := NewService(auditStore).AppendAudit(ctx, nil, AuditEvent{
		Action: "login",
		Detail: "Authorization: Bearer eyJhbGciOi.token password=Sup3rSecret! -----BEGIN RSA PRIVATE KEY-----",
		IP:     "10.0.0.8",
		Result: domain.AuditResultOK,
	}, testKey); err != nil {
		t.Fatalf("追加用户日志失败: %v", err)
	}
	detail := auditStore.entries[0].Detail
	for _, leaked := range []string{"eyJhbGciOi.token", "Sup3rSecret!", "PRIVATE KEY"} {
		if strings.Contains(detail, leaked) {
			t.Fatalf("用户日志 detail 未脱敏（含 %q）: %s", leaked, detail)
		}
	}

	// 2) 系统日志：同样在写库前脱敏。
	queryStore := &fakeQueryStore{}
	svc := NewService(nil).WithQueryStore(queryStore)
	if err := svc.RecordSystemEvent(ctx, domain.LogLevelInfo, domain.SystemEventStart,
		`{"Authorization":"Bearer s3cr3t","password":"p@ss"}`); err != nil {
		t.Fatalf("写入系统日志失败: %v", err)
	}
	if len(queryStore.appended) != 1 {
		t.Fatalf("系统日志未落库: %+v", queryStore.appended)
	}
	message := queryStore.appended[0].Message
	for _, leaked := range []string{"s3cr3t", "p@ss"} {
		if strings.Contains(message, leaked) {
			t.Fatalf("系统日志 message 未脱敏（含 %q）: %s", leaked, message)
		}
	}
}

// TestHMACChain 审计链逐条链接且可校验：篡改任意一条即被检出（INV-8 / DES-T-006）。
func TestHMACChain(t *testing.T) {
	ctx := context.Background()
	store := &memStore{}
	svc := NewService(store)

	hashes := make([][]byte, 0, 3)
	for _, action := range []string{"login", "create", "delete"} {
		h, err := svc.AppendAudit(ctx, nil, newEvent(action, "POST /api/v1/environments status=200"), testKey)
		if err != nil {
			t.Fatalf("append %s 失败: %v", action, err)
		}
		if len(h) != 32 {
			t.Fatalf("hash 应为 32 字节 HMAC-SHA256，got %d", len(h))
		}
		hashes = append(hashes, h)
	}

	if len(store.entries) != 3 {
		t.Fatalf("应追加 3 条，got %d", len(store.entries))
	}
	if store.entries[0].PrevHash != nil {
		t.Fatalf("链首 prev_hash 应为空，got %x", store.entries[0].PrevHash)
	}
	for i := 1; i < len(store.entries); i++ {
		if !bytes.Equal(store.entries[i].PrevHash, hashes[i-1]) {
			t.Fatalf("第 %d 条 prev_hash 应链接上一条 hash", i+1)
		}
	}
	if err := VerifyAuditChain(store.entries, testKey); err != nil {
		t.Fatalf("未篡改时链校验应通过，got %v", err)
	}

	// 篡改中间一条（模拟绕过 DB 触发器的改写）→ 链校验失败。
	tampered := make([]domain.AuditLog, len(store.entries))
	copy(tampered, store.entries)
	tampered[1].Action = "update"
	if err := VerifyAuditChain(tampered, testKey); err == nil {
		t.Fatal("篡改中间条目后链校验应失败")
	}

	// 换用错误密钥同样校验失败（密钥参与 HMAC）。
	if err := VerifyAuditChain(store.entries, bytes.Repeat([]byte{0x11}, 32)); err == nil {
		t.Fatal("错误 HMAC 密钥下链校验应失败")
	}
}

// seedTestLogs 构造两个任务的测试日志（任务 1 三条、任务 2 两条）。
func seedTestLogs(base time.Time) []domain.TestLog {
	return []domain.TestLog{
		{ID: 1, TaskID: 1, Seq: 1, Level: domain.LogLevelInfo, Message: "任务开始执行", CreatedAt: base},
		{ID: 2, TaskID: 1, Seq: 2, Level: domain.LogLevelWarn, Message: "连接超时，准备重试", CreatedAt: base.Add(time.Second)},
		{ID: 3, TaskID: 1, Seq: 3, Level: domain.LogLevelError, Message: "execute failed: timeout", CreatedAt: base.Add(2 * time.Second)},
		{ID: 4, TaskID: 2, Seq: 1, Level: domain.LogLevelInfo, Message: "任务开始执行", CreatedAt: base.Add(3 * time.Second)},
		{ID: 5, TaskID: 2, Seq: 2, Level: domain.LogLevelInfo, Message: "任务执行完成", CreatedAt: base.Add(4 * time.Second)},
	}
}

// TestTestLogByTask 测试日志按 task_id 定位 + 增量 seq 排序 + 级别/关键字/分页筛选。
func TestTestLogByTask(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	store := &fakeQueryStore{tests: seedTestLogs(base)}
	svc := NewService(nil).WithQueryStore(store)

	// 1) 按任务定位：仅返回任务 1 的日志，且按 seq 升序（增量可读）。
	page, err := svc.ListTestLogs(ctx, domain.TestLogQuery{TaskID: 1})
	if err != nil {
		t.Fatalf("按任务查询失败: %v", err)
	}
	if page.Total != 3 || len(page.List) != 3 {
		t.Fatalf("任务 1 日志数 = %d/%d, want 3/3", page.Total, len(page.List))
	}
	for i, want := range []int64{1, 2, 3} {
		if page.List[i].Seq != want {
			t.Fatalf("第 %d 条 seq = %d, want %d", i+1, page.List[i].Seq, want)
		}
		if page.List[i].TaskID != 1 {
			t.Fatalf("返回了其他任务的日志: %+v", page.List[i])
		}
	}

	// 2) 级别筛选。
	warnPage, err := svc.ListTestLogs(ctx, domain.TestLogQuery{TaskID: 1, LogQuery: domain.LogQuery{Level: domain.LogLevelWarn}})
	if err != nil {
		t.Fatalf("按级别查询失败: %v", err)
	}
	if warnPage.Total != 1 || warnPage.List[0].Level != domain.LogLevelWarn {
		t.Fatalf("warn 日志筛选错误: %+v", warnPage.List)
	}

	// 3) 关键字筛选（大小写不敏感）。
	keywordPage, err := svc.ListTestLogs(ctx, domain.TestLogQuery{TaskID: 1, LogQuery: domain.LogQuery{Keyword: "TIMEOUT"}})
	if err != nil {
		t.Fatalf("按关键字查询失败: %v", err)
	}
	if keywordPage.Total != 1 || keywordPage.List[0].Seq != 3 {
		t.Fatalf("关键字筛选错误: %+v", keywordPage.List)
	}

	// 4) 分页：任务 1 按 seq 升序切成 2 条/页。
	second, err := svc.ListTestLogs(ctx, domain.TestLogQuery{TaskID: 1, LogQuery: domain.LogQuery{Page: 2, PageSize: 2}})
	if err != nil {
		t.Fatalf("分页查询失败: %v", err)
	}
	if second.Total != 3 || len(second.List) != 1 || second.List[0].Seq != 3 {
		t.Fatalf("分页结果错误: total=%d list=%+v", second.Total, second.List)
	}
	if second.Page != 2 || second.PageSize != 2 {
		t.Fatalf("分页回显错误: page=%d page_size=%d", second.Page, second.PageSize)
	}

	// 5) 跨任务查询（不传 task_id）仍可用，按时间倒序。
	all, err := svc.ListTestLogs(ctx, domain.TestLogQuery{})
	if err != nil {
		t.Fatalf("全量查询失败: %v", err)
	}
	if all.Total != 5 || all.List[0].ID != 5 {
		t.Fatalf("跨任务查询应按时间倒序: total=%d first=%+v", all.Total, all.List[0])
	}

	// 6) 时间范围筛选（含边界）。
	from := base.Add(time.Second)
	to := base.Add(3 * time.Second)
	window, err := svc.ListTestLogs(ctx, domain.TestLogQuery{LogQuery: domain.LogQuery{StartTime: &from, EndTime: &to}})
	if err != nil {
		t.Fatalf("时间范围查询失败: %v", err)
	}
	if window.Total != 3 {
		t.Fatalf("时间范围内应有 3 条，got %d", window.Total)
	}

	// 7) 非法 task_id（负数）→ 参数错误。
	if _, err := svc.ListTestLogs(ctx, domain.TestLogQuery{TaskID: -1}); err == nil {
		t.Fatal("负数 task_id 应返回参数错误")
	}
}

// TestInvalidTimeRange 查询时间范围非法 → 25001（api-contract §2.6）。
func TestInvalidTimeRange(t *testing.T) {
	ctx := context.Background()
	svc := NewService(nil).WithQueryStore(&fakeQueryStore{})

	from := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	to := from.Add(-time.Hour)

	cases := []struct {
		name string
		call func() error
	}{
		{"user", func() error {
			_, err := svc.ListUserLogs(ctx, domain.UserLogQuery{LogQuery: domain.LogQuery{StartTime: &from, EndTime: &to}})
			return err
		}},
		{"system", func() error {
			_, err := svc.ListSystemLogs(ctx, domain.SystemLogQuery{LogQuery: domain.LogQuery{StartTime: &from, EndTime: &to}})
			return err
		}},
		{"test", func() error {
			_, err := svc.ListTestLogs(ctx, domain.TestLogQuery{LogQuery: domain.LogQuery{StartTime: &from, EndTime: &to}})
			return err
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, ErrInvalidTimeRange) {
				t.Fatalf("err = %v, want ErrInvalidTimeRange(25001)", err)
			}
		})
	}
}

// TestUserLogQueryFilters 用户日志按操作人/动作/关联对象筛选，level 由 result 推导。
func TestUserLogQueryFilters(t *testing.T) {
	ctx := context.Background()
	base := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	admin7, admin8 := int64(7), int64(8)
	store := &fakeQueryStore{users: []domain.AuditLog{
		{ID: 1, AdminID: &admin7, AdminUsername: "alice", Action: "create", ResourceType: "environment", ResourceID: "12", Result: domain.AuditResultOK, Detail: "POST /api/v1/environments", CreatedAt: base},
		{ID: 2, AdminID: &admin7, AdminUsername: "alice", Action: "delete", ResourceType: "environment", ResourceID: "12", Result: domain.AuditResultOK, Detail: "DELETE /api/v1/environments/12", CreatedAt: base.Add(time.Second)},
		{ID: 3, AdminID: &admin8, AdminUsername: "bob", Action: "login", ResourceType: "auth", Result: domain.AuditResultFail, Detail: "POST /api/v1/auth/login status=401", CreatedAt: base.Add(2 * time.Second)},
	}}
	svc := NewService(nil).WithQueryStore(store)

	// 关联对象筛选：resource_type + resource_id。
	page, err := svc.ListUserLogs(ctx, domain.UserLogQuery{ResourceType: "environment", ResourceID: "12"})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("环境 12 的操作应有 2 条，got %d", page.Total)
	}

	// 操作人筛选。
	page, err = svc.ListUserLogs(ctx, domain.UserLogQuery{AdminID: 8})
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if page.Total != 1 || page.List[0].Action != "login" {
		t.Fatalf("按操作人筛选错误: %+v", page.List)
	}

	// level=info → result=ok；level=error → result=fail。
	infoLevel, err := svc.ListUserLogs(ctx, domain.UserLogQuery{LogQuery: domain.LogQuery{Level: domain.LogLevelInfo}})
	if err != nil {
		t.Fatalf("按级别查询失败: %v", err)
	}
	if infoLevel.Total != 2 || infoLevel.List[0].Result != domain.AuditResultOK {
		t.Fatalf("info 级别应映射 result=ok: %+v", infoLevel.List)
	}
	errorLevel, err := svc.ListUserLogs(ctx, domain.UserLogQuery{LogQuery: domain.LogQuery{Level: domain.LogLevelError}})
	if err != nil {
		t.Fatalf("按级别查询失败: %v", err)
	}
	if errorLevel.Total != 1 || errorLevel.List[0].Action != "login" {
		t.Fatalf("error 级别应映射 result=fail: %+v", errorLevel.List)
	}

	// level=warn：用户日志无 warn，必然无匹配（不落库查询）。
	warnLevel, err := svc.ListUserLogs(ctx, domain.UserLogQuery{LogQuery: domain.LogQuery{Level: domain.LogLevelWarn}})
	if err != nil {
		t.Fatalf("按级别查询失败: %v", err)
	}
	if warnLevel.Total != 0 || len(warnLevel.List) != 0 {
		t.Fatalf("warn 级别不应有用户日志: %+v", warnLevel.List)
	}

	// 非法级别 → 参数错误。
	if _, err := svc.ListUserLogs(ctx, domain.UserLogQuery{LogQuery: domain.LogQuery{Level: "fatal"}}); err == nil {
		t.Fatal("非法 level 应返回参数错误")
	}
}

// TestRecordSystemEvents 系统日志记录启动/重启/停止，并对上次异常退出判定为重启（warn）。
func TestRecordSystemEvents(t *testing.T) {
	ctx := context.Background()

	// 1) 首次启动（无历史）→ info/start。
	store := &fakeQueryStore{}
	svc := NewService(nil).WithQueryStore(store)
	if err := svc.RecordStartup(ctx, "0.1.0"); err != nil {
		t.Fatalf("记录启动日志失败: %v", err)
	}
	if len(store.appended) != 1 {
		t.Fatalf("应写入 1 条系统日志: %+v", store.appended)
	}
	if store.appended[0].Event != domain.SystemEventStart || store.appended[0].Level != domain.LogLevelInfo {
		t.Fatalf("首次启动应记 info/start: %+v", store.appended[0])
	}

	// 2) 上次为 start（缺少 stop 记录）→ warn/restart。
	store2 := &fakeQueryStore{lastEvent: domain.SystemEventStart}
	svc2 := NewService(nil).WithQueryStore(store2)
	if err := svc2.RecordStartup(ctx, "0.1.0"); err != nil {
		t.Fatalf("记录重启日志失败: %v", err)
	}
	if store2.appended[0].Event != domain.SystemEventRestart || store2.appended[0].Level != domain.LogLevelWarn {
		t.Fatalf("非正常退出后应记 warn/restart: %+v", store2.appended[0])
	}

	// 3) 上次为 stop → 视为正常启动 info/start。
	store3 := &fakeQueryStore{lastEvent: domain.SystemEventStop}
	svc3 := NewService(nil).WithQueryStore(store3)
	if err := svc3.RecordStartup(ctx, "0.1.1"); err != nil {
		t.Fatalf("记录启动日志失败: %v", err)
	}
	if store3.appended[0].Event != domain.SystemEventStart {
		t.Fatalf("正常退出后应记 info/start: %+v", store3.appended[0])
	}

	// 4) 优雅退出 → info/stop。
	if err := svc3.RecordShutdown(ctx, "0.1.1"); err != nil {
		t.Fatalf("记录停止日志失败: %v", err)
	}
	if got := store3.appended[1]; got.Event != domain.SystemEventStop || got.Level != domain.LogLevelInfo {
		t.Fatalf("退出应记 info/stop: %+v", got)
	}

	// 5) 系统日志可按事件/级别筛选。
	page, err := svc3.ListSystemLogs(ctx, domain.SystemLogQuery{Event: domain.SystemEventStop})
	if err != nil {
		t.Fatalf("查询系统日志失败: %v", err)
	}
	if page.Total != 1 || page.List[0].Event != domain.SystemEventStop {
		t.Fatalf("事件筛选错误: %+v", page.List)
	}

	// 6) 非法事件/级别 → 参数错误（不落库）。
	if err := svc3.RecordSystemEvent(ctx, domain.LogLevelInfo, "boom", "x"); err == nil {
		t.Fatal("非法系统事件应返回参数错误")
	}
	if err := svc3.RecordSystemEvent(ctx, "trace", domain.SystemEventStart, "x"); err == nil {
		t.Fatal("非法日志级别应返回参数错误")
	}
}

// TestQueryWithoutStore 未接仓储时查询返回依赖异常而非 panic（DB 不可用降级）。
func TestQueryWithoutStore(t *testing.T) {
	ctx := context.Background()
	svc := NewService(nil)
	if _, err := svc.ListUserLogs(ctx, domain.UserLogQuery{}); err == nil {
		t.Fatal("未接仓储应返回依赖异常")
	}
	if _, err := svc.ListSystemLogs(ctx, domain.SystemLogQuery{}); err == nil {
		t.Fatal("未接仓储应返回依赖异常")
	}
	if _, err := svc.ListTestLogs(ctx, domain.TestLogQuery{}); err == nil {
		t.Fatal("未接仓储应返回依赖异常")
	}
	// 系统日志写入在未接仓储时静默降级，不阻断服务启动。
	if err := svc.RecordSystemEvent(ctx, domain.LogLevelInfo, domain.SystemEventStart, "x"); err != nil {
		t.Fatalf("未接仓储时系统日志应静默降级, got %v", err)
	}
}
