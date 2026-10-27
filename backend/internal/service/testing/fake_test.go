package testing

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
	"auto_performance/internal/repository"
)

// fakeTaskRepo 内存测试管理仓储（无 DB 依赖）：实现 service/testing.Repo 全量方法，
// 用于验证五要素校验、环境互斥、快照固化、状态流转与重试/清理入口约束。
type fakeTaskRepo struct {
	envStatus string
	envHosts  []string
	envErr    error

	tasks     map[int64]*domain.TestTask
	templates map[int64]*domain.TestTemplate
	nextTask  int64
	nextTmpl  int64

	// thresholdsValue 模拟「阈值被后续修改」，用于验证任务快照隔离。
	thresholdValue float64
	interfaceMiss  bool
}

func newFakeTaskRepo() *fakeTaskRepo {
	return &fakeTaskRepo{
		envStatus:      domain.EnvStatusPending,
		tasks:          map[int64]*domain.TestTask{},
		templates:      map[int64]*domain.TestTemplate{},
		nextTask:       1000,
		nextTmpl:       500,
		thresholdValue: 100,
	}
}

func (f *fakeTaskRepo) ensure() {
	if f.tasks == nil {
		f.tasks = map[int64]*domain.TestTask{}
	}
	if f.templates == nil {
		f.templates = map[int64]*domain.TestTemplate{}
	}
	if f.nextTask == 0 {
		f.nextTask = 1000
	}
	if f.nextTmpl == 0 {
		f.nextTmpl = 500
	}
	if f.envStatus == "" {
		f.envStatus = domain.EnvStatusPending
	}
	if f.thresholdValue == 0 {
		f.thresholdValue = 100
	}
}

func (f *fakeTaskRepo) WithTx(ctx context.Context, fn repository.TxFunc) error {
	return fn(ctx, nil)
}

// ===================== 模板 =====================

func (f *fakeTaskRepo) ListTemplates(_ context.Context, q domain.TemplateQuery) ([]domain.TestTemplate, int64, error) {
	f.ensure()
	out := make([]domain.TestTemplate, 0, len(f.templates))
	for _, t := range f.templates {
		if q.Keyword != "" && !containsFold(t.Name, q.Keyword) {
			continue
		}
		out = append(out, *cloneTemplate(t))
	}
	return out, int64(len(out)), nil
}

func (f *fakeTaskRepo) GetTemplate(_ context.Context, id int64) (*domain.TestTemplate, error) {
	f.ensure()
	t, ok := f.templates[id]
	if !ok {
		return nil, ErrTemplateNotFound
	}
	return cloneTemplate(t), nil
}

func (f *fakeTaskRepo) CreateTemplate(_ context.Context, t *domain.TestTemplate) error {
	f.ensure()
	f.nextTmpl++
	t.ID = f.nextTmpl
	t.Version = 1
	t.CreatedAt = time.Now()
	t.UpdatedAt = t.CreatedAt
	f.templates[t.ID] = cloneTemplate(t)
	return nil
}

func (f *fakeTaskRepo) UpdateTemplate(_ context.Context, t *domain.TestTemplate, version int64) error {
	f.ensure()
	cur, ok := f.templates[t.ID]
	if !ok {
		return errcode.ErrNotFound
	}
	if cur.Version != version {
		return errcode.ErrConcurrent
	}
	t.Version = version + 1
	t.CreatedAt = cur.CreatedAt
	t.UpdatedAt = time.Now()
	f.templates[t.ID] = cloneTemplate(t)
	return nil
}

func (f *fakeTaskRepo) SoftDeleteTemplate(_ context.Context, id int64) error {
	f.ensure()
	if _, ok := f.templates[id]; !ok {
		return ErrTemplateNotFound
	}
	delete(f.templates, id)
	return nil
}

// ===================== 环境互斥 =====================

func (f *fakeTaskRepo) LockEnvironment(_ context.Context, _ repository.Tx, envID int64) (*domain.Environment, error) {
	f.ensure()
	if f.envErr != nil {
		return nil, f.envErr
	}
	hosts := f.envHosts
	if hosts == nil {
		hosts = []string{"10.20.30.40", "Env-Internal.example.com"}
	}
	return &domain.Environment{ID: envID, Name: "测试环境", Status: f.envStatus, Hosts: hosts}, nil
}

func (f *fakeTaskRepo) MarkEnvironmentTesting(_ context.Context, _ repository.Tx, _ int64) error {
	f.ensure()
	f.envStatus = domain.EnvStatusTesting
	return nil
}

func (f *fakeTaskRepo) ReleaseEnvironment(_ context.Context, _ repository.Tx, _ int64) error {
	f.ensure()
	if f.envStatus == domain.EnvStatusTesting {
		f.envStatus = domain.EnvStatusPending
	}
	return nil
}

// ===================== 快照 =====================

func (f *fakeTaskRepo) SnapshotInterfaces(_ context.Context, _ repository.Tx, ids []int64) ([]domain.InterfaceSnapshot, error) {
	if f.interfaceMiss {
		return nil, errcode.ErrNotFound
	}
	out := make([]domain.InterfaceSnapshot, 0, len(ids))
	for _, id := range ids {
		out = append(out, domain.InterfaceSnapshot{
			InterfaceID: id,
			ProductID:   3,
			ProductName: "网关服务",
			Name:        "查询订单",
			Method:      "GET",
			Scheme:      domain.SchemeHTTPS,
			Port:        8443,
			Path:        "/orders",
			Status:      domain.InterfaceStatusEnabled,
			CapturedAt:  time.Now(),
		})
	}
	return out, nil
}

func (f *fakeTaskRepo) SnapshotThresholds(_ context.Context, _ repository.Tx, ids []int64) ([]domain.ThresholdSnapshot, error) {
	if f.interfaceMiss {
		return nil, errcode.ErrNotFound
	}
	out := make([]domain.ThresholdSnapshot, 0, len(ids))
	for _, id := range ids {
		out = append(out, domain.ThresholdSnapshot{
			InterfaceID:   id,
			InterfaceName: "查询订单",
			Metric:        domain.MetricP95,
			Operator:      domain.OperatorLE,
			Value:         f.thresholdValue,
			Unit:          "ms",
			Enabled:       true,
		})
	}
	return out, nil
}

// ===================== 任务 =====================

func (f *fakeTaskRepo) CreateTask(_ context.Context, _ repository.Tx, task *domain.TestTask) error {
	f.ensure()
	f.nextTask++
	task.ID = f.nextTask
	task.Version = 1
	task.Status = domain.TaskPending
	task.CreatedAt = time.Now()
	task.UpdatedAt = task.CreatedAt
	f.tasks[task.ID] = cloneTask(task)
	return nil
}

func (f *fakeTaskRepo) GetTask(_ context.Context, id int64) (*domain.TestTask, error) {
	f.ensure()
	t, ok := f.tasks[id]
	if !ok {
		return nil, errcode.ErrNotFound
	}
	return cloneTask(t), nil
}

func (f *fakeTaskRepo) ListTasks(_ context.Context, q domain.TaskQuery) ([]domain.TestTask, int64, error) {
	f.ensure()
	out := make([]domain.TestTask, 0, len(f.tasks))
	for _, t := range f.tasks {
		if q.Status != "" && t.Status != q.Status {
			continue
		}
		if q.EnvID > 0 && t.EnvID != q.EnvID {
			continue
		}
		if q.Keyword != "" && !containsFold(t.Name, q.Keyword) {
			continue
		}
		out = append(out, *cloneTask(t))
	}
	return out, int64(len(out)), nil
}

func (f *fakeTaskRepo) TransitionTask(_ context.Context, _ repository.Tx, id int64, from, to domain.TaskStatus) error {
	f.ensure()
	t, ok := f.tasks[id]
	if !ok {
		return errcode.ErrNotFound
	}
	if t.Status != from || !domain.CanTransition(from, to) {
		return errcode.ErrState
	}
	t.Status = to
	t.Version++
	t.UpdatedAt = time.Now()
	return nil
}

func (f *fakeTaskRepo) SetTaskStarted(_ context.Context, _ repository.Tx, id int64, at time.Time) error {
	f.ensure()
	t, ok := f.tasks[id]
	if !ok {
		return errcode.ErrNotFound
	}
	started := at
	t.StartedAt = &started
	return nil
}

func (f *fakeTaskRepo) SetTaskFinished(_ context.Context, _ repository.Tx, id int64, at time.Time) error {
	f.ensure()
	t, ok := f.tasks[id]
	if !ok {
		return errcode.ErrNotFound
	}
	finished := at
	t.FinishedAt = &finished
	return nil
}

func (f *fakeTaskRepo) ResetTaskExecution(_ context.Context, _ repository.Tx, id int64) error {
	f.ensure()
	t, ok := f.tasks[id]
	if !ok {
		return errcode.ErrNotFound
	}
	t.StartedAt = nil
	t.FinishedAt = nil
	t.Result = nil
	return nil
}

// mustTransition 测试辅助：直接驱动 fake 仓储状态（模拟编排器流转）。
func (f *fakeTaskRepo) mustTransition(t *testing.T, id int64, from, to domain.TaskStatus) {
	t.Helper()
	if err := f.TransitionTask(context.Background(), nil, id, from, to); err != nil {
		t.Fatalf("模拟状态流转 %s→%s 失败: %v", from, to, err)
	}
}

// ===================== fakeExecutor =====================

// fakeExecutor 假编排器：记录 start/cancel/cleanup 调用，不真正拉起子进程。
type fakeExecutor struct {
	started   []int64
	cancelled []int64
	cleaned   []int64
	startErr  error
}

func (f *fakeExecutor) StartTask(_ context.Context, taskID int64) error {
	if f.startErr != nil {
		return f.startErr
	}
	f.started = append(f.started, taskID)
	return nil
}

func (f *fakeExecutor) CancelTask(_ context.Context, taskID int64) error {
	f.cancelled = append(f.cancelled, taskID)
	return nil
}

func (f *fakeExecutor) CleanupTask(_ context.Context, taskID int64) error {
	f.cleaned = append(f.cleaned, taskID)
	return nil
}

// ===================== helpers =====================

func cloneTask(t *domain.TestTask) *domain.TestTask {
	cp := *t
	cp.InterfaceIDs = append([]int64(nil), t.InterfaceIDs...)
	cp.ProductIDs = append([]int64(nil), t.ProductIDs...)
	cp.InterfaceSnapshots = append([]domain.InterfaceSnapshot(nil), t.InterfaceSnapshots...)
	cp.ThresholdSnapshots = append([]domain.ThresholdSnapshot(nil), t.ThresholdSnapshots...)
	cp.Flow = append(json.RawMessage(nil), t.Flow...)
	cp.ReportConfig = append(json.RawMessage(nil), t.ReportConfig...)
	cp.CleanConfig = append(json.RawMessage(nil), t.CleanConfig...)
	cp.Result = append(json.RawMessage(nil), t.Result...)
	return &cp
}

func cloneTemplate(t *domain.TestTemplate) *domain.TestTemplate {
	cp := *t
	cp.EnvIDs = append([]int64(nil), t.EnvIDs...)
	cp.InterfaceIDs = append([]int64(nil), t.InterfaceIDs...)
	cp.Flow = append(json.RawMessage(nil), t.Flow...)
	cp.ReportConfig = append(json.RawMessage(nil), t.ReportConfig...)
	cp.CleanConfig = append(json.RawMessage(nil), t.CleanConfig...)
	return &cp
}

func containsFold(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexFold(s, sub) >= 0)
}

func indexFold(s, sub string) int {
	n := len(sub)
	for i := 0; i+n <= len(s); i++ {
		if equalFold(s[i:i+n], sub) {
			return i
		}
	}
	return -1
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}
