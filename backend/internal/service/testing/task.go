// Package testing 测试管理业务服务（模板/任务编排）。
//
// 对齐 PRD §3.3/§4.7/§4.8、api-contract §2.4 与不变量：
//   - INV-3：任务状态机流转（含清理待确认），非法流转 → 23001；
//   - INV-4：同环境同刻仅一个任务压测（事务内 SELECT ... FOR UPDATE）；
//   - INV-5：任务创建时固化接口范围与阈值快照；
//   - INV-9：流程参数白名单由 domain/k6 校验，并发数非法 → 23004。
package testing

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
	"auto_performance/internal/repository"
)

// 分页默认值与上限（api-contract §1.3）。
const (
	DefaultPageSize = 20
	MaxPageSize     = 200
)

// Repo 测试管理仓储能力（由 repository.PGRepository 实现；测试可注入内存假仓储）。
type Repo interface {
	// 事务与模板
	WithTx(ctx context.Context, fn repository.TxFunc) error
	ListTemplates(ctx context.Context, q domain.TemplateQuery) ([]domain.TestTemplate, int64, error)
	GetTemplate(ctx context.Context, id int64) (*domain.TestTemplate, error)
	CreateTemplate(ctx context.Context, t *domain.TestTemplate) error
	UpdateTemplate(ctx context.Context, t *domain.TestTemplate, version int64) error
	SoftDeleteTemplate(ctx context.Context, id int64) error

	// 环境互斥（INV-4）
	LockEnvironment(ctx context.Context, tx repository.Tx, envID int64) (*domain.Environment, error)
	MarkEnvironmentTesting(ctx context.Context, tx repository.Tx, envID int64) error
	ReleaseEnvironment(ctx context.Context, tx repository.Tx, envID int64) error

	// 快照（INV-5）
	SnapshotInterfaces(ctx context.Context, tx repository.Tx, ids []int64) ([]domain.InterfaceSnapshot, error)
	SnapshotThresholds(ctx context.Context, tx repository.Tx, ids []int64) ([]domain.ThresholdSnapshot, error)

	// 任务
	CreateTask(ctx context.Context, tx repository.Tx, task *domain.TestTask) error
	GetTask(ctx context.Context, id int64) (*domain.TestTask, error)
	ListTasks(ctx context.Context, q domain.TaskQuery) ([]domain.TestTask, int64, error)
	TransitionTask(ctx context.Context, tx repository.Tx, id int64, from, to domain.TaskStatus) error
	SetTaskStarted(ctx context.Context, tx repository.Tx, id int64, at time.Time) error
	SetTaskFinished(ctx context.Context, tx repository.Tx, id int64, at time.Time) error
	ResetTaskExecution(ctx context.Context, tx repository.Tx, id int64) error
}

// Executor 编排器能力（由 orchestrator.Orchestrator 实现；nil 时执行类动作返回 10602）。
type Executor interface {
	// StartTask 异步启动执行（任务已处于 running）。
	StartTask(ctx context.Context, taskID int64) error
	// CancelTask 终止执行中的子进程（幂等）。
	CancelTask(ctx context.Context, taskID int64) error
	// CleanupTask 异步执行清理（任务已处于 cleaning）。
	CleanupTask(ctx context.Context, taskID int64) error
}

// Service 测试管理业务服务（模板 + 任务）。
type Service struct {
	repo     Repo
	executor Executor
	maxVUs   int
	now      func() time.Time
}

// NewTaskService 构造测试管理服务（maxVUs 默认 domain.DefaultMaxVUs，可用 WithMaxVUs 覆盖）。
func NewTaskService(repo Repo, executor Executor) *Service {
	return &Service{
		repo:     repo,
		executor: executor,
		maxVUs:   domain.DefaultMaxVUs,
		now:      time.Now,
	}
}

// WithMaxVUs 覆盖并发数上限（配置项 k6.max_vus）。
func (s *Service) WithMaxVUs(maxVUs int) *Service {
	if s != nil && maxVUs > 0 {
		s.maxVUs = maxVUs
	}
	return s
}

// ===================== 任务 DTO =====================

// CreateTaskInput 任务创建输入（五要素 + 可选模板）。
//
// Flow/ReportConfig/CleanConfig 为 JSON 原文；nil 表示未显式给出，可沿用模板默认值。
type CreateTaskInput struct {
	Name       string
	TemplateID int64
	EnvID      int64
	// TargetHost 压测目标主机：必须属于所选环境的 hosts（PRD §4.8）。
	TargetHost   string
	ProductIDs   []int64
	InterfaceIDs []int64
	Flow         json.RawMessage
	ReportConfig json.RawMessage
	CleanConfig  json.RawMessage
	CreatedBy    int64
	AutoStart    bool
}

// TaskView 任务视图（ID 由接入层转为字符串）。
type TaskView struct {
	ID                 int64
	Name               string
	TemplateID         int64
	EnvID              int64
	TargetHost         string
	CreatedBy          int64
	ProductIDs         []int64
	InterfaceIDs       []int64
	InterfaceSnapshots []domain.InterfaceSnapshot
	ThresholdSnapshots []domain.ThresholdSnapshot
	Flow               json.RawMessage
	ReportConfig       json.RawMessage
	CleanConfig        json.RawMessage
	Status             domain.TaskStatus
	StartedAt          *time.Time
	FinishedAt         *time.Time
	Result             json.RawMessage
	Version            int64
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// TaskPage 任务分页结果。
type TaskPage struct {
	List     []TaskView
	Total    int64
	Page     int
	PageSize int
}

// ListTasksQuery 任务列表查询条件。
type ListTasksQuery struct {
	Keyword     string
	Status      string
	EnvID       int64
	ProductID   int64
	TemplateID  int64
	CreatedFrom *time.Time
	CreatedTo   *time.Time
	Page        int
	PageSize    int
}

// ===================== 任务创建（4.2） =====================

// Create 创建任务：五要素校验 → 事务内环境行锁 + 快照 → 状态 pending（INV-4 / INV-5）。
func (s *Service) Create(ctx context.Context, in CreateTaskInput) (*TaskView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}

	name := strings.TrimSpace(in.Name)
	if name == "" {
		return nil, errcode.ErrParamMiss
	}
	if len([]rune(name)) > 128 {
		return nil, errcode.ErrParamFmt
	}
	// 接口不保存 host：执行前必须显式选定压测目标主机（需求 1）。
	targetHost := strings.TrimSpace(in.TargetHost)
	if targetHost == "" {
		return nil, errcode.ErrParamMiss
	}
	// auto_start 依赖编排器；缺失时在落库前直接拒绝，避免产生无法执行的任务。
	if in.AutoStart && s.executor == nil {
		return nil, errcode.ErrDependency
	}

	// 1) 模板默认值（请求显式给出的字段优先，PRD §3.3.2「可覆盖、不影响原模板」）。
	var tpl *domain.TestTemplate
	if in.TemplateID > 0 {
		t, err := s.repo.GetTemplate(ctx, in.TemplateID)
		if err != nil {
			if errors.Is(err, errcode.ErrNotFound) {
				return nil, ErrTemplateNotFound
			}
			return nil, err
		}
		tpl = t
	}
	envID := in.EnvID
	interfaceIDs := dedupeIDs(in.InterfaceIDs)
	flow, reportConfig, cleanConfig := in.Flow, in.ReportConfig, in.CleanConfig
	if tpl != nil {
		if envID <= 0 && len(tpl.EnvIDs) > 0 {
			envID = tpl.EnvIDs[0]
		}
		if len(interfaceIDs) == 0 {
			interfaceIDs = dedupeIDs(tpl.InterfaceIDs)
		}
		if len(flow) == 0 {
			flow = tpl.Flow
		}
		if len(reportConfig) == 0 {
			reportConfig = tpl.ReportConfig
		}
		if len(cleanConfig) == 0 {
			cleanConfig = tpl.CleanConfig
		}
	}

	// 2) 五要素校验。
	if envID <= 0 {
		return nil, errcode.ErrParamMiss
	}
	if len(interfaceIDs) == 0 {
		return nil, ErrInterfaceRangeEmpty
	}
	if len(interfaceIDs) > domain.MaxInterfaceRange {
		return nil, errcode.ErrParamFmt
	}
	if len(flow) == 0 || len(reportConfig) == 0 || len(cleanConfig) == 0 {
		return nil, errcode.ErrParamMiss
	}
	if !configSizeOK(flow, reportConfig, cleanConfig) {
		return nil, errcode.ErrParamFmt
	}

	flowCfg, err := domain.ParseFlow(flow)
	if err != nil {
		return nil, errcode.ErrParamFmt
	}
	if _, err := flowCfg.Params(s.maxVUs); err != nil {
		return nil, mapFlowError(err)
	}
	normalizedReport, _, err := domain.ParseReportConfig(reportConfig)
	if err != nil {
		return nil, mapConfigError(err)
	}
	normalizedClean, _, err := domain.ParseCleanConfig(cleanConfig)
	if err != nil {
		return nil, mapConfigError(err)
	}

	// 3) 事务内：锁环境（互斥）→ 快照 → 建任务。
	task := &domain.TestTask{
		TemplateID:   in.TemplateID,
		Name:         name,
		EnvID:        envID,
		TargetHost:   targetHost,
		CreatedBy:    in.CreatedBy,
		ProductIDs:   dedupeIDs(in.ProductIDs),
		InterfaceIDs: interfaceIDs,
		Flow:         flow,
		ReportConfig: normalizedReport,
		CleanConfig:  normalizedClean,
		Status:       domain.TaskPending,
	}

	err = s.repo.WithTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		env, err := s.repo.LockEnvironment(ctx, tx, envID)
		if err != nil {
			return err
		}
		switch env.Status {
		case domain.EnvStatusTesting:
			return ErrEnvBusy
		case domain.EnvStatusDisabled:
			return errcode.ErrParamFmt // 已下线环境不可用于新任务（PRD §3.1.5）
		}
		// 目标主机必须来自所选环境的 hosts（防止跨环境打错目标）。
		if !domain.HostInList(task.TargetHost, env.Hosts) {
			return errcode.ErrParamFmt
		}
		// 环境 hosts 由人工维护：拼接前再做一次地址白名单校验（拒绝带端口/路径的写法）。
		if !domain.IsValidHostAddress(task.TargetHost) {
			return errcode.ErrParamFmt
		}

		snapshots, err := s.repo.SnapshotInterfaces(ctx, tx, interfaceIDs)
		if err != nil {
			return err
		}
		fillSnapshotURLs(snapshots, task.TargetHost)
		task.InterfaceSnapshots = snapshots

		thresholds, err := s.repo.SnapshotThresholds(ctx, tx, interfaceIDs)
		if err != nil {
			return err
		}
		task.ThresholdSnapshots = thresholds

		return s.repo.CreateTask(ctx, tx, task)
	})
	if err != nil {
		return nil, err
	}

	if in.AutoStart {
		return s.Start(ctx, task.ID)
	}
	view := toTaskView(task)
	return &view, nil
}

// ===================== 任务查询 =====================

// Get 任务详情（不存在 → 10301）。
func (s *Service) Get(ctx context.Context, id int64) (*TaskView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if id <= 0 {
		return nil, errcode.ErrParamFmt
	}
	task, err := s.repo.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	view := toTaskView(task)
	return &view, nil
}

// List 任务列表（筛选 + 分页）。
func (s *Service) List(ctx context.Context, q ListTasksQuery) (*TaskPage, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	status := domain.TaskStatus(strings.TrimSpace(q.Status))
	if status != "" && !domain.IsValidTaskStatus(status) {
		return nil, errcode.ErrParamFmt
	}
	if q.EnvID < 0 || q.ProductID < 0 || q.TemplateID < 0 {
		return nil, errcode.ErrParamFmt
	}
	page, pageSize := normalizePaging(q.Page, q.PageSize)

	tasks, total, err := s.repo.ListTasks(ctx, domain.TaskQuery{
		Keyword:     strings.TrimSpace(q.Keyword),
		Status:      status,
		EnvID:       q.EnvID,
		ProductID:   q.ProductID,
		TemplateID:  q.TemplateID,
		CreatedFrom: q.CreatedFrom,
		CreatedTo:   q.CreatedTo,
		Page:        page,
		PageSize:    pageSize,
	})
	if err != nil {
		return nil, err
	}
	list := make([]TaskView, 0, len(tasks))
	for i := range tasks {
		list = append(list, toTaskView(&tasks[i]))
	}
	return &TaskPage{List: list, Total: total, Page: page, PageSize: pageSize}, nil
}

// ===================== 执行控制（4.7） =====================

// Start 执行任务：pending → running，事务内占用环境（INV-4），随后交给编排器异步执行。
func (s *Service) Start(ctx context.Context, id int64) (*TaskView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if s.executor == nil {
		return nil, errcode.ErrDependency
	}
	task, err := s.repo.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if !domain.CanStartTask(task.Status) {
		return nil, ErrInvalidTaskState
	}

	now := s.now()
	err = s.repo.WithTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		env, err := s.repo.LockEnvironment(ctx, tx, task.EnvID)
		if err != nil {
			return err
		}
		switch env.Status {
		case domain.EnvStatusTesting:
			return ErrEnvBusy
		case domain.EnvStatusDisabled:
			return errcode.ErrParamFmt
		}
		if err := s.repo.MarkEnvironmentTesting(ctx, tx, task.EnvID); err != nil {
			return err
		}
		if err := s.repo.TransitionTask(ctx, tx, id, domain.TaskPending, domain.TaskRunning); err != nil {
			return mapTransitionError(err)
		}
		return s.repo.SetTaskStarted(ctx, tx, id, now)
	})
	if err != nil {
		return nil, err
	}

	if err := s.executor.StartTask(ctx, id); err != nil {
		s.rollbackStart(ctx, task, now)
		return nil, err
	}
	return s.Get(ctx, id)
}

// rollbackStart 编排器启动失败时回退：running → failed → cleanup_pending，并释放环境。
//
// 回退为尽力而为（不覆盖原始错误），保证环境不被永久占用。
func (s *Service) rollbackStart(ctx context.Context, task *domain.TestTask, at time.Time) {
	_ = s.repo.WithTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		if err := s.repo.TransitionTask(ctx, tx, task.ID, domain.TaskRunning, domain.TaskFailed); err != nil {
			return err
		}
		_ = s.repo.SetTaskFinished(ctx, tx, task.ID, at)
		_ = s.repo.ReleaseEnvironment(ctx, tx, task.EnvID)
		return s.repo.TransitionTask(ctx, tx, task.ID, domain.TaskFailed, domain.TaskCleanupPending)
	})
}

// Cancel 停止执行中任务：终止子进程 → running → cancelled → cleanup_pending（保留现场）。
func (s *Service) Cancel(ctx context.Context, id int64) (*TaskView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	task, err := s.repo.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if !domain.CanCancelTask(task.Status) {
		return nil, ErrInvalidTaskState
	}

	if s.executor != nil {
		// 终止 k6 子进程（幂等；进程已退出时忽略错误）。
		_ = s.executor.CancelTask(ctx, id)
	}

	now := s.now()
	err = s.repo.WithTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		if err := s.repo.TransitionTask(ctx, tx, id, domain.TaskRunning, domain.TaskCancelled); err != nil {
			return mapTransitionError(err)
		}
		if err := s.repo.SetTaskFinished(ctx, tx, id, now); err != nil {
			return err
		}
		return s.repo.TransitionTask(ctx, tx, id, domain.TaskCancelled, domain.TaskCleanupPending)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Retry 重试失败任务：failed/cleanup_pending → pending，清空执行痕迹并释放环境（复用原快照）。
func (s *Service) Retry(ctx context.Context, id int64) (*TaskView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	task, err := s.repo.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if !domain.CanRetryTask(task.Status) {
		return nil, ErrInvalidTaskState
	}
	from := task.Status
	err = s.repo.WithTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		if err := s.repo.TransitionTask(ctx, tx, id, from, domain.TaskPending); err != nil {
			return mapTransitionError(err)
		}
		if err := s.repo.ResetTaskExecution(ctx, tx, id); err != nil {
			return err
		}
		return s.repo.ReleaseEnvironment(ctx, tx, task.EnvID)
	})
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Cleanup 人工确认清理：仅 cleanup_pending → cleaning，随后由编排器执行清理。
func (s *Service) Cleanup(ctx context.Context, id int64) (*TaskView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if s.executor == nil {
		return nil, errcode.ErrDependency
	}
	task, err := s.repo.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	if !domain.CanCleanupTask(task.Status) {
		return nil, ErrInvalidTaskState
	}
	err = s.repo.WithTx(ctx, func(ctx context.Context, tx repository.Tx) error {
		return mapTransitionError(s.repo.TransitionTask(ctx, tx, id, domain.TaskCleanupPending, domain.TaskCleaning))
	})
	if err != nil {
		return nil, err
	}
	if err := s.executor.CleanupTask(ctx, id); err != nil {
		// 清理未能启动：回到待确认，避免任务卡在 cleaning。
		_ = s.repo.WithTx(ctx, func(ctx context.Context, tx repository.Tx) error {
			return s.repo.TransitionTask(ctx, tx, id, domain.TaskCleaning, domain.TaskCleanupPending)
		})
		return nil, err
	}
	return s.Get(ctx, id)
}

// ===================== helpers =====================

// toTaskView 领域任务 → 视图。
func toTaskView(t *domain.TestTask) TaskView {
	return TaskView{
		ID:                 t.ID,
		Name:               t.Name,
		TemplateID:         t.TemplateID,
		EnvID:              t.EnvID,
		TargetHost:         t.TargetHost,
		CreatedBy:          t.CreatedBy,
		ProductIDs:         append([]int64{}, t.ProductIDs...),
		InterfaceIDs:       append([]int64{}, t.InterfaceIDs...),
		InterfaceSnapshots: append([]domain.InterfaceSnapshot{}, t.InterfaceSnapshots...),
		ThresholdSnapshots: append([]domain.ThresholdSnapshot{}, t.ThresholdSnapshots...),
		Flow:               t.Flow,
		ReportConfig:       t.ReportConfig,
		CleanConfig:        t.CleanConfig,
		Status:             t.Status,
		StartedAt:          t.StartedAt,
		FinishedAt:         t.FinishedAt,
		Result:             t.Result,
		Version:            t.Version,
		CreatedAt:          t.CreatedAt,
		UpdatedAt:          t.UpdatedAt,
	}
}

// fillSnapshotURLs 用所选 target_host 回填快照的展示地址（{scheme}://{host}:{port}{path}）。
//
// 执行期地址仍由引擎按 scheme/host/port/path 现场拼接；此处仅用于响应与报告展示，
// 解析失败（历史脏数据）时保留原 url 不动，不阻断任务创建。
func fillSnapshotURLs(snapshots []domain.InterfaceSnapshot, targetHost string) {
	for i := range snapshots {
		snap := &snapshots[i]
		if strings.TrimSpace(snap.Path) == "" {
			continue // 迁移前的历史快照（仅有 url）保持原值
		}
		url, err := domain.InterfaceURL(snap.Scheme, targetHost, snap.Port, snap.Path)
		if err != nil {
			continue
		}
		snap.URL = url
	}
}

// normalizePaging 归一化分页（默认 20，上限 200）。
func normalizePaging(page, pageSize int) (int, int) {
	if page <= 0 {
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

// dedupeIDs 去重并过滤非正 ID（保持顺序）。
func dedupeIDs(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// maxConfigBytes 单个配置 JSON（flow/report/clean）体积上限，保护请求体与快照体积。
const maxConfigBytes = 64 * 1024

// configSizeOK 校验配置 JSON 体积。
func configSizeOK(configs ...json.RawMessage) bool {
	for _, cfg := range configs {
		if len(cfg) > maxConfigBytes {
			return false
		}
	}
	return true
}

// mapFlowError 流程错误 → 业务码（并发数 23004，其余参数格式 10003）。
func mapFlowError(err error) error {
	if errors.Is(err, domain.ErrInvalidConcurrency) {
		return ErrConcurrencyInvalid
	}
	return errcode.ErrParamFmt
}

// mapConfigError 报告/清理配置错误 → 10003。
func mapConfigError(err error) error {
	_ = err
	return errcode.ErrParamFmt
}

// mapTransitionError 仓储状态冲突（10401）→ 任务状态不允许（23001）。
func mapTransitionError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errcode.ErrState) {
		return ErrInvalidTaskState
	}
	return err
}
