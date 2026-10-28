package orchestrator

// 本文件提供端到端集成测试用的内存仓储（无 PostgreSQL 依赖）。
//
// 同时实现环境/产品/测试/报告四个业务仓储能力与编排器 Store，使 TestEndToEndTaskFlow
// 可以跑「建环境 → 建产品/接口/阈值 → 建任务（五要素 + 快照）→ 执行 → 评估 → 报告 →
// 自动清理/待确认 → 人工确认清理」的真实服务链路。
//
// 仅实现被链路实际调用的方法；其余方法由嵌入的接口类型兜底（若被误调用会 panic，
// 便于在测试中暴露链路变化）。

import (
	"context"
	"sync"
	"time"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
	"auto_performance/internal/repository"
	envsvc "auto_performance/internal/service/environment"
	productsvc "auto_performance/internal/service/product"
	reportsvc "auto_performance/internal/service/report"
	testsvc "auto_performance/internal/service/testing"
)

// 别名仅用于给嵌入字段取不冲突的名字（方法提升仍与原接口一致）。
type (
	e2eEnvRepo     = envsvc.Repo
	e2eProductRepo = productsvc.Repo
	e2eTaskRepo    = testsvc.Repo
	e2eReportRepo  = reportsvc.Repo
)

// e2eStore 内存仓储（并发安全；所有返回值均为副本，避免与执行协程共享可变状态）。
type e2eStore struct {
	e2eEnvRepo
	e2eProductRepo
	e2eTaskRepo
	e2eReportRepo

	mu         sync.Mutex
	seq        int64
	now        func() time.Time
	categories map[int64]*domain.EnvironmentCategory
	tags       map[int64]*domain.Tag
	envs       map[int64]*domain.Environment
	products   map[int64]*domain.Product
	interfaces map[int64]*domain.Interface
	thresholds map[int64][]domain.Threshold
	tasks      map[int64]*domain.TestTask
	reports    map[int64]*domain.Report
	artifacts  map[int64][]domain.ReportArtifact

	transitions []string
	testLogs    []e2eTestLog
	envReleased int
}

// e2eTestLog 测试日志记录（用于断言执行期日志已落库）。
type e2eTestLog struct {
	TaskID  int64
	Seq     int64
	Level   string
	Message string
}

func newE2EStore() *e2eStore {
	return &e2eStore{
		now:        time.Now,
		categories: map[int64]*domain.EnvironmentCategory{},
		tags:       map[int64]*domain.Tag{},
		envs:       map[int64]*domain.Environment{},
		products:   map[int64]*domain.Product{},
		interfaces: map[int64]*domain.Interface{},
		thresholds: map[int64][]domain.Threshold{},
		tasks:      map[int64]*domain.TestTask{},
		reports:    map[int64]*domain.Report{},
		artifacts:  map[int64][]domain.ReportArtifact{},
	}
}

// ===================== 事务 / 通用 =====================

// WithTx 内存储无需事务：直接执行回调（同时消解四个嵌入接口的 WithTx 歧义）。
func (s *e2eStore) WithTx(ctx context.Context, fn repository.TxFunc) error { return fn(ctx, nil) }

func (s *e2eStore) nextIDLocked() int64 {
	s.seq++
	return s.seq
}

// transitionLog 返回状态流转轨迹（如 "pending->running"）。
// envReleasedCount 返因清理而释放环境的次数。
func (s *e2eStore) envReleasedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.envReleased
}

func (s *e2eStore) transitionLog() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.transitions...)
}

// logs 返回测试日志副本。
func (s *e2eStore) logs() []e2eTestLog {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]e2eTestLog(nil), s.testLogs...)
}

// ===================== 环境分类 / 标签 / 环境 =====================

func (s *e2eStore) CreateCategory(_ context.Context, c *domain.EnvironmentCategory) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c.ID = s.nextIDLocked()
	c.Version = 1
	c.CreatedAt, c.UpdatedAt = s.now(), s.now()
	cp := *c
	s.categories[c.ID] = &cp
	return nil
}

func (s *e2eStore) GetCategory(_ context.Context, id int64) (*domain.EnvironmentCategory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.categories[id]
	if !ok {
		return nil, errcode.ErrNotFound
	}
	cp := *c
	return &cp, nil
}

func (s *e2eStore) ListCategories(_ context.Context) ([]domain.EnvironmentCategory, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.EnvironmentCategory, 0, len(s.categories))
	for _, c := range s.categories {
		out = append(out, *c)
	}
	return out, nil
}

func (s *e2eStore) CreateTag(_ context.Context, t *domain.Tag) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t.ID = s.nextIDLocked()
	t.Version = 1
	t.CreatedAt, t.UpdatedAt = s.now(), s.now()
	cp := *t
	s.tags[t.ID] = &cp
	return nil
}

func (s *e2eStore) GetTag(_ context.Context, id int64) (*domain.Tag, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tags[id]
	if !ok {
		return nil, errcode.ErrNotFound
	}
	cp := *t
	return &cp, nil
}

func (s *e2eStore) ListTags(_ context.Context) ([]domain.Tag, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Tag, 0, len(s.tags))
	for _, t := range s.tags {
		out = append(out, *t)
	}
	return out, nil
}

func (s *e2eStore) CountTagsByIDs(_ context.Context, ids []int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var n int64
	for _, id := range ids {
		if _, ok := s.tags[id]; ok {
			n++
		}
	}
	return n, nil
}

func (s *e2eStore) CreateEnvironment(_ context.Context, env *domain.Environment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	env.ID = s.nextIDLocked()
	env.Version = 1
	env.CreatedAt, env.UpdatedAt = s.now(), s.now()
	cp := *env
	cp.Hosts = append([]string(nil), env.Hosts...)
	cp.TagIDs = append([]int64(nil), env.TagIDs...)
	s.envs[env.ID] = &cp
	return nil
}

func (s *e2eStore) GetEnvironment(_ context.Context, id int64) (*domain.Environment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	env, ok := s.envs[id]
	if !ok {
		return nil, errcode.ErrNotFound
	}
	cp := *env
	cp.Hosts = append([]string(nil), env.Hosts...)
	cp.TagIDs = append([]int64(nil), env.TagIDs...)
	return &cp, nil
}

func (s *e2eStore) ReplaceEnvironmentTags(_ context.Context, envID int64, tagIDs []int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	env, ok := s.envs[envID]
	if !ok {
		return errcode.ErrNotFound
	}
	env.TagIDs = append([]int64(nil), tagIDs...)
	return nil
}

// LockEnvironment 模拟 SELECT ... FOR UPDATE（内存实现无并发写入，返回当前快照）。
func (s *e2eStore) LockEnvironment(_ context.Context, _ repository.Tx, envID int64) (*domain.Environment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	env, ok := s.envs[envID]
	if !ok {
		return nil, errcode.ErrNotFound
	}
	cp := *env
	return &cp, nil
}

func (s *e2eStore) MarkEnvironmentTesting(_ context.Context, _ repository.Tx, envID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	env, ok := s.envs[envID]
	if !ok {
		return errcode.ErrNotFound
	}
	env.Status = domain.EnvStatusTesting
	env.Version++
	return nil
}

func (s *e2eStore) ReleaseEnvironment(_ context.Context, _ repository.Tx, envID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if env, ok := s.envs[envID]; ok && env.Status == domain.EnvStatusTesting {
		env.Status = domain.EnvStatusPending
		env.Version++
		s.envReleased++
	}
	return nil
}

func (s *e2eStore) EnvironmentSshHost(_ context.Context, envID int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	env, ok := s.envs[envID]
	if !ok {
		return 0, errcode.ErrNotFound
	}
	return env.SshHostID, nil
}

// ===================== 产品 / 接口 / 阈值 =====================

func (s *e2eStore) CreateProduct(_ context.Context, p *domain.Product) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p.ID = s.nextIDLocked()
	p.Version = 1
	p.CreatedAt, p.UpdatedAt = s.now(), s.now()
	cp := *p
	s.products[p.ID] = &cp
	return nil
}

func (s *e2eStore) GetProduct(_ context.Context, id int64) (*domain.Product, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.products[id]
	if !ok {
		return nil, errcode.ErrNotFound
	}
	cp := *p
	var count int64
	for _, api := range s.interfaces {
		if api.ProductID == id {
			count++
		}
	}
	cp.InterfaceCount = count
	return &cp, nil
}

func (s *e2eStore) CreateInterface(_ context.Context, api *domain.Interface) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	api.ID = s.nextIDLocked()
	api.Version = 1
	api.CreatedAt, api.UpdatedAt = s.now(), s.now()
	cp := *api
	cp.Headers = cloneStringMap(api.Headers)
	cp.HeadersEnc = append([]byte(nil), api.HeadersEnc...)
	s.interfaces[api.ID] = &cp
	return nil
}

func (s *e2eStore) GetInterface(_ context.Context, id int64) (*domain.Interface, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	api, ok := s.interfaces[id]
	if !ok {
		return nil, errcode.ErrNotFound
	}
	cp := *api
	cp.Headers = cloneStringMap(api.Headers)
	cp.HeadersEnc = append([]byte(nil), api.HeadersEnc...)
	return &cp, nil
}

func (s *e2eStore) ListThresholds(_ context.Context, interfaceID int64) ([]domain.Threshold, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.interfaces[interfaceID]; !ok {
		return nil, errcode.ErrNotFound
	}
	return append([]domain.Threshold(nil), s.thresholds[interfaceID]...), nil
}

func (s *e2eStore) ReplaceThresholds(_ context.Context, interfaceID int64, items []domain.Threshold) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.interfaces[interfaceID]; !ok {
		return errcode.ErrNotFound
	}
	stored := make([]domain.Threshold, 0, len(items))
	for i, item := range items {
		item.ID = int64(i + 1)
		item.InterfaceID = interfaceID
		item.Version = 1
		item.CreatedAt, item.UpdatedAt = s.now(), s.now()
		stored = append(stored, item)
	}
	s.thresholds[interfaceID] = stored
	return nil
}

// ===================== 快照（INV-5） =====================

func (s *e2eStore) SnapshotInterfaces(_ context.Context, _ repository.Tx, ids []int64) ([]domain.InterfaceSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]domain.InterfaceSnapshot, 0, len(ids))
	for _, id := range ids {
		api, ok := s.interfaces[id]
		if !ok {
			return nil, errcode.ErrNotFound
		}
		productName := ""
		if p, ok := s.products[api.ProductID]; ok {
			productName = p.Name
		}
		out = append(out, domain.InterfaceSnapshot{
			InterfaceID:     api.ID,
			ProductID:       api.ProductID,
			ProductName:     productName,
			Name:            api.Name,
			Method:          api.Method,
			Scheme:          api.Scheme,
			Port:            api.Port,
			Path:            api.Path,
			Headers:         cloneStringMap(api.Headers),
			AuthHeaderNames: api.AuthHeaderNames(),
			AuthHeadersEnc:  append([]byte(nil), api.HeadersEnc...),
			Body:            api.Body,
			Timeouts:        api.Timeouts,
			Status:          api.Status,
			CapturedAt:      s.now().UTC(),
		})
	}
	return out, nil
}

func (s *e2eStore) SnapshotThresholds(_ context.Context, _ repository.Tx, ids []int64) ([]domain.ThresholdSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]domain.ThresholdSnapshot, 0, len(ids))
	for _, id := range ids {
		api, ok := s.interfaces[id]
		if !ok {
			return nil, errcode.ErrNotFound
		}
		for _, t := range s.thresholds[id] {
			out = append(out, domain.ThresholdSnapshot{
				InterfaceID:   id,
				InterfaceName: api.Name,
				Metric:        t.Metric,
				Operator:      t.Operator,
				Value:         t.Value,
				Unit:          t.Unit,
				Enabled:       t.Enabled,
			})
		}
	}
	return out, nil
}

// ===================== 任务 =====================

func (s *e2eStore) CreateTask(_ context.Context, _ repository.Tx, task *domain.TestTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task.ID = s.nextIDLocked()
	task.Version = 1
	task.CreatedAt, task.UpdatedAt = s.now(), s.now()
	s.tasks[task.ID] = cloneTask(task)
	return nil
}

func (s *e2eStore) GetTask(_ context.Context, id int64) (*domain.TestTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return nil, errcode.ErrNotFound
	}
	return cloneTask(task), nil
}

func (s *e2eStore) TransitionTask(_ context.Context, _ repository.Tx, id int64, from, to domain.TaskStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return errcode.ErrNotFound
	}
	if task.Status != from || !domain.CanTransition(from, to) {
		return errcode.ErrState
	}
	task.Status = to
	task.UpdatedAt = s.now()
	s.transitions = append(s.transitions, string(from)+"->"+string(to))
	return nil
}

func (s *e2eStore) SetTaskStarted(_ context.Context, _ repository.Tx, id int64, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return errcode.ErrNotFound
	}
	t := at
	task.StartedAt = &t
	return nil
}

// UpdateTaskSnapshots 刷新任务快照（e2e 假存储只记录到内存任务对象）。
func (s *e2eStore) UpdateTaskSnapshots(_ context.Context, _ repository.Tx, id int64, interfaces []domain.InterfaceSnapshot, thresholds []domain.ThresholdSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return errcode.ErrNotFound
	}
	task.InterfaceSnapshots = append([]domain.InterfaceSnapshot{}, interfaces...)
	task.ThresholdSnapshots = append([]domain.ThresholdSnapshot{}, thresholds...)
	return nil
}

func (s *e2eStore) SetTaskFinished(_ context.Context, _ repository.Tx, id int64, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return errcode.ErrNotFound
	}
	t := at
	task.FinishedAt = &t
	return nil
}

func (s *e2eStore) SetTaskResult(_ context.Context, _ repository.Tx, id int64, result []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return errcode.ErrNotFound
	}
	task.Result = append([]byte(nil), result...)
	return nil
}

func (s *e2eStore) ResetTaskExecution(_ context.Context, _ repository.Tx, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return errcode.ErrNotFound
	}
	task.StartedAt, task.FinishedAt, task.Result = nil, nil, nil
	return nil
}

func (s *e2eStore) AppendTestLog(_ context.Context, taskID, seq int64, level, message string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.testLogs = append(s.testLogs, e2eTestLog{TaskID: taskID, Seq: seq, Level: level, Message: message})
	return nil
}

// ===================== 报告 =====================

// ListReports 按任务过滤（端到端用例只用 TaskID 定位报告）。
func (s *e2eStore) ListReports(_ context.Context, q domain.ReportQuery) ([]domain.Report, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.Report, 0, len(s.reports))
	for _, rep := range s.reports {
		if q.TaskID > 0 && rep.TaskID != q.TaskID {
			continue
		}
		if q.Status != "" && rep.Status != q.Status {
			continue
		}
		cp := *rep
		cp.Artifacts = append([]domain.ReportArtifact(nil), s.artifacts[rep.ID]...)
		out = append(out, cp)
	}
	return out, int64(len(out)), nil
}

func (s *e2eStore) CreateReport(_ context.Context, _ repository.Tx, rep *domain.Report) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rep.ID = s.nextIDLocked()
	rep.Version = 1
	rep.CreatedAt, rep.UpdatedAt = s.now(), s.now()
	cp := *rep
	s.reports[rep.ID] = &cp
	return nil
}

func (s *e2eStore) GetReport(_ context.Context, id int64) (*domain.Report, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rep, ok := s.reports[id]
	if !ok {
		return nil, errcode.ErrNotFound
	}
	cp := *rep
	cp.Metrics = append([]byte(nil), rep.Metrics...)
	if rep.Summary != nil {
		summary := *rep.Summary
		cp.Summary = &summary
	}
	cp.Artifacts = append([]domain.ReportArtifact(nil), s.artifacts[id]...)
	return &cp, nil
}

func (s *e2eStore) UpdateReport(_ context.Context, _ repository.Tx, id int64, status string,
	summary *domain.ReportSummary, metrics []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rep, ok := s.reports[id]
	if !ok {
		return errcode.ErrNotFound
	}
	rep.Status = status
	rep.UpdatedAt = s.now()
	if summary != nil {
		cp := *summary
		cp.Items = append([]domain.ReportSummaryItem(nil), summary.Items...)
		rep.Summary = &cp
	}
	if metrics != nil {
		rep.Metrics = append([]byte(nil), metrics...)
	}
	return nil
}

func (s *e2eStore) ReplaceArtifacts(_ context.Context, _ repository.Tx, reportID int64, artifacts []domain.ReportArtifact) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.reports[reportID]; !ok {
		return errcode.ErrNotFound
	}
	stored := make([]domain.ReportArtifact, 0, len(artifacts))
	for i, a := range artifacts {
		a.ID = int64(i + 1)
		a.ReportID = reportID
		if a.CreatedAt.IsZero() {
			a.CreatedAt = s.now()
		}
		stored = append(stored, a)
	}
	s.artifacts[reportID] = stored
	return nil
}

// ===================== helpers =====================

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneTask(task *domain.TestTask) *domain.TestTask {
	if task == nil {
		return nil
	}
	cp := *task
	cp.ProductIDs = append([]int64(nil), task.ProductIDs...)
	cp.InterfaceIDs = append([]int64(nil), task.InterfaceIDs...)
	cp.Flow = append([]byte(nil), task.Flow...)
	cp.ReportConfig = append([]byte(nil), task.ReportConfig...)
	cp.CleanConfig = append([]byte(nil), task.CleanConfig...)
	cp.Result = append([]byte(nil), task.Result...)
	cp.InterfaceSnapshots = make([]domain.InterfaceSnapshot, 0, len(task.InterfaceSnapshots))
	for _, snap := range task.InterfaceSnapshots {
		s := snap
		s.Headers = cloneStringMap(snap.Headers)
		s.AuthHeadersEnc = append([]byte(nil), snap.AuthHeadersEnc...)
		s.AuthHeaderNames = append([]string(nil), snap.AuthHeaderNames...)
		cp.InterfaceSnapshots = append(cp.InterfaceSnapshots, s)
	}
	cp.ThresholdSnapshots = append([]domain.ThresholdSnapshot(nil), task.ThresholdSnapshots...)
	if task.StartedAt != nil {
		t := *task.StartedAt
		cp.StartedAt = &t
	}
	if task.FinishedAt != nil {
		t := *task.FinishedAt
		cp.FinishedAt = &t
	}
	return &cp
}
