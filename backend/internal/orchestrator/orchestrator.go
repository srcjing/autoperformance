// Package orchestrator 任务编排器。
//
// 对齐 architecture.md §2.2/§2.3 与 PRD §3.3：驱动任务状态机（pending → running →
// collecting → evaluating → generating → finished → cleaning → finished），
// 调用 k6 引擎执行、流式推送脱敏日志、阈值评估、报告生成（Phase 5 注入）与清理编排。
package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"auto_performance/internal/cleaner"
	"auto_performance/internal/domain"
	"auto_performance/internal/engine/k6"
	"auto_performance/internal/errcode"
	"auto_performance/internal/evaluator"
	"auto_performance/internal/logutil"
	"auto_performance/internal/repository"
)

// 状态更新超时（取消/异常路径使用独立于请求生命周期的上下文）。
const statusUpdateTimeout = 10 * time.Second

// Store 编排器所需仓储能力（由 repository.PGRepository 实现）。
type Store interface {
	GetTask(ctx context.Context, id int64) (*domain.TestTask, error)
	TransitionTask(ctx context.Context, tx repository.Tx, id int64, from, to domain.TaskStatus) error
	SetTaskStarted(ctx context.Context, tx repository.Tx, id int64, at time.Time) error
	SetTaskFinished(ctx context.Context, tx repository.Tx, id int64, at time.Time) error
	SetTaskResult(ctx context.Context, tx repository.Tx, id int64, result []byte) error
	ReleaseEnvironment(ctx context.Context, tx repository.Tx, envID int64) error
	EnvironmentSshHost(ctx context.Context, envID int64) (int64, error)
	WithTx(ctx context.Context, fn repository.TxFunc) error
	AppendTestLog(ctx context.Context, taskID, seq int64, level, message string) error
}

// EngineRunner 压测引擎执行能力（由 k6.Runner 实现；测试可注入假引擎）。
//
// 只暴露编排实际使用的方法（工作目录、脚本/结果路径、执行），保持与 k6 参数白名单、
// 无 shell 子进程、路径沙箱隔离等安全约束无关的实现细节可替换。
type EngineRunner interface {
	// WorkDir 脚本/结果落盘工作目录。
	WorkDir() string
	// ScriptPath 任务脚本路径（仅由任务 ID 派生）。
	ScriptPath(taskID int64) string
	// ResultPath 任务结果路径（仅由任务 ID 派生）。
	ResultPath(taskID int64) string
	// Run 执行压测，逐行回调输出（回调方负责脱敏后推送/落库）。
	Run(ctx context.Context, scriptPath, resultPath string, onLine func(line string)) error
}

// RequestLogWriter 请求详情写入能力（由 service/log.Service 实现；nil 时不记录）。
//
// 实测开关/类型勾选/采样限量均由日志服务根据日志设置判定，编排器只负责
// 把 k6 逐请求明细转换成统一实体。
type RequestLogWriter interface {
	WriteRequestLogs(ctx context.Context, taskID int64, entries []domain.RequestLog) error
}

// RequestLogWriterFunc 函数适配器：用于把返回「实际落库条数」的服务方法适配为写入能力。
type RequestLogWriterFunc func(ctx context.Context, taskID int64, entries []domain.RequestLog) error

// WriteRequestLogs 实现 RequestLogWriter。
func (f RequestLogWriterFunc) WriteRequestLogs(ctx context.Context, taskID int64, entries []domain.RequestLog) error {
	return f(ctx, taskID, entries)
}

// Publisher 实时推送能力（由 ws.Hub 实现；nil 时不推送）。
type Publisher interface {
	Publish(channel, event string, payload any)
}

// ReportGenerator 报告生成能力（Phase 5 注入；nil 时跳过生成步骤）。
type ReportGenerator interface {
	Generate(ctx context.Context, task *domain.TestTask, evaluation evaluator.Evaluation) error
}

// SecretDecoder 解密接口认证头密文（INV-7：快照只存密文，执行期解密注入脚本）。
type SecretDecoder func(enc []byte) (map[string]string, error)

// Deps 编排器依赖。
type Deps struct {
	Store   Store
	Runner  EngineRunner
	Cleaner *cleaner.Cleaner
	Hub     Publisher
	Report  ReportGenerator
	// SecretDecoder 认证头解密器（nil 时脚本仅携带普通请求头）。
	SecretDecoder SecretDecoder
	// RequestLogs 请求详情写入能力（nil 时不记录请求详情）。
	RequestLogs RequestLogWriter
	// MaxVUs 并发数上限（配置项 k6.max_vus）。
	MaxVUs int
	// CleanPlanResolver 清理计划解析器（默认 cleaner.ParsePlan；测试可注入）。
	CleanPlanResolver func(raw json.RawMessage, hosts []int64) (*cleaner.Plan, error)
	// Clock 时钟（测试可注入）。
	Clock func() time.Time
}

// Orchestrator 任务编排器。
type Orchestrator struct {
	store       Store
	runner      EngineRunner
	cleaner     *cleaner.Cleaner
	hub         Publisher
	report      ReportGenerator
	decoder     SecretDecoder
	requestLogs RequestLogWriter
	maxVUs      int
	resolve     func(raw json.RawMessage, hosts []int64) (*cleaner.Plan, error)
	now         func() time.Time

	runs *registry
	seq  *seqCounter
	wg   sync.WaitGroup
}

// New 构造编排器。
func New(deps Deps) *Orchestrator {
	resolve := deps.CleanPlanResolver
	if resolve == nil {
		resolve = cleaner.ParsePlan
	}
	now := deps.Clock
	if now == nil {
		now = time.Now
	}
	maxVUs := deps.MaxVUs
	if maxVUs <= 0 {
		maxVUs = domain.DefaultMaxVUs
	}
	return &Orchestrator{
		store:       deps.Store,
		runner:      deps.Runner,
		cleaner:     deps.Cleaner,
		hub:         deps.Hub,
		report:      deps.Report,
		decoder:     deps.SecretDecoder,
		requestLogs: deps.RequestLogs,
		maxVUs:      maxVUs,
		resolve:     resolve,
		now:         now,
		runs:        newRegistry(),
		seq:         newSeqCounter(),
	}
}

// StartTask 异步启动任务执行（任务已由服务层置为 running 并占用环境）。
func (o *Orchestrator) StartTask(_ context.Context, taskID int64) error {
	if o == nil || o.store == nil || o.runner == nil {
		return errcode.ErrDependency
	}
	if taskID <= 0 {
		return errcode.ErrParamFmt
	}
	ctx, cancel := context.WithCancel(context.Background())
	o.runs.add(taskID, cancel)
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		defer o.runs.remove(taskID)
		defer cancel()

		if err := o.run(ctx, taskID); err != nil {
			o.failTask(ctx, taskID, err)
		}
	}()
	return nil
}

// CancelTask 终止执行中的 k6 子进程（幂等）。
func (o *Orchestrator) CancelTask(_ context.Context, taskID int64) error {
	if o == nil {
		return errcode.ErrDependency
	}
	o.runs.cancel(taskID)
	o.logBackground(taskID, "warn", "收到取消指令，正在终止压测子进程")
	return nil
}

// CleanupTask 执行清理（任务已由服务层置为 cleaning）。
//
// 清理计划在启动前解析校验（失败立即返回，服务层回退 cleanup_pending）。
func (o *Orchestrator) CleanupTask(ctx context.Context, taskID int64) error {
	if o == nil || o.store == nil {
		return errcode.ErrDependency
	}
	if o.cleaner == nil {
		return fmt.Errorf("%w: 清理器未就绪", errcode.ErrDependency)
	}
	task, err := o.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	if task.Status != domain.TaskCleaning {
		return errcode.ErrState
	}
	plan, err := o.cleanPlan(ctx, task)
	if err != nil {
		return err
	}

	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		o.executeCleanup(ctx, task, plan)
	}()
	return nil
}

// Shutdown 取消全部执行并等待退出（优雅关闭）。
func (o *Orchestrator) Shutdown(ctx context.Context) {
	if o == nil {
		return
	}
	o.runs.cancelAll()

	done := make(chan struct{})
	go func() {
		o.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// ===================== 执行主流程 =====================

// run 执行一次压测：脚本生成 → k6 子进程 → 采集 → 评估 → 报告 → 自动清理。
func (o *Orchestrator) run(ctx context.Context, taskID int64) error {
	task, err := o.store.GetTask(ctx, taskID)
	if err != nil {
		return err
	}
	o.publish(taskID, "status", map[string]any{"status": task.Status, "message": "任务开始执行"})
	o.log(ctx, taskID, "info", fmt.Sprintf("任务开始执行 name=%s engine=k6", task.Name))

	script, err := o.buildScript(task)
	if err != nil {
		return fmt.Errorf("生成 k6 脚本失败: %w", err)
	}
	scriptPath := o.runner.ScriptPath(taskID)
	// 工作目录应由安装脚本创建并归属低权限执行账号（k6 需可读脚本 + 可写结果）。
	if err := os.MkdirAll(o.runner.WorkDir(), 0o750); err != nil {
		return fmt.Errorf("创建 k6 工作目录失败: %w", err)
	}
	// 脚本可能包含执行期解密后的认证头：0600 且执行结束即删除（INV-7）。
	if err := os.WriteFile(scriptPath, []byte(script), 0o600); err != nil {
		return fmt.Errorf("写入 k6 脚本失败: %w", err)
	}
	defer func() { _ = os.Remove(scriptPath) }()
	o.log(ctx, taskID, "info", "k6 脚本已生成，开始压测")

	resultPath := o.runner.ResultPath(taskID)
	if err := o.runner.Run(ctx, scriptPath, resultPath, func(line string) {
		o.log(ctx, taskID, "info", line)
	}); err != nil {
		if ctx.Err() != nil {
			// 已取消：状态由服务层（running → cancelled → cleanup_pending）负责。
			return nil
		}
		return fmt.Errorf("压测执行失败: %w", err)
	}

	// 采集中
	if err := o.transition(ctx, taskID, domain.TaskRunning, domain.TaskCollecting); err != nil {
		return err
	}
	o.publish(taskID, "status", map[string]any{"status": domain.TaskCollecting})

	parsed, err := k6.ParseFile(resultPath)
	if err != nil {
		return fmt.Errorf("采集压测结果失败: %w", err)
	}
	// 逐请求明细：写库前由日志服务按日志设置检查开关/类型并采样限量。
	o.recordRequestLogs(ctx, task, parsed)
	wall := parsed.EndedAt.Sub(parsed.StartedAt)
	metrics := evaluator.Normalize(toEvaluatorSamples(parsed.Samples), evaluator.Options{
		Wall:   wall,
		MaxVUs: o.targetVUs(task),
	})
	o.publish(taskID, "metrics", metrics)

	// 评估中
	if err := o.transition(ctx, taskID, domain.TaskCollecting, domain.TaskEvaluating); err != nil {
		return err
	}
	o.publish(taskID, "status", map[string]any{"status": domain.TaskEvaluating})
	evaluation := evaluator.Evaluate(metrics, toThresholds(task.ThresholdSnapshots))
	if err := o.saveResult(ctx, taskID, metrics, evaluation, wall); err != nil {
		return err
	}
	o.publish(taskID, "evaluation", evaluation)
	o.log(ctx, taskID, "info", fmt.Sprintf("阈值评估完成 state=%s pass=%t", evaluation.State, evaluation.Pass))

	// 报告生成中（Phase 5 注入 ReportGenerator；未注入时跳过产物生成）
	if err := o.transition(ctx, taskID, domain.TaskEvaluating, domain.TaskGenerating); err != nil {
		return err
	}
	o.publish(taskID, "status", map[string]any{"status": domain.TaskGenerating})
	if o.report != nil {
		if err := o.report.Generate(ctx, task, evaluation); err != nil {
			return fmt.Errorf("报告生成失败: %w", err)
		}
	}

	if err := o.transition(ctx, taskID, domain.TaskGenerating, domain.TaskFinished); err != nil {
		return err
	}
	o.markFinished(ctx, taskID)
	// 任务成功即释放环境占用（环境可再次使用）；环境释放不依赖清理。
	o.releaseEnv(ctx, taskID, task.EnvID)

	// 按需清理（可选附加操作）：接口压测默认不修改环境，未配置动作时直接完成；
	// 清理失败仅记录，不回退任务成功状态、不影响环境释放。
	plan, err := o.cleanPlan(ctx, task)
	if err != nil {
		o.logBackground(taskID, "info", "任务成功；未执行环境清理（"+err.Error()+"）")
		return nil
	}
	if len(plan.Actions) == 0 && len(plan.Scripts) == 0 {
		o.logBackground(taskID, "info", "任务成功；未配置环境清理动作，跳过清理")
		return nil
	}
	o.runCleanupBestEffort(ctx, task, plan)
	return nil
}

// buildScript 生成 k6 脚本（认证头密文在此解密注入，INV-7/DES-T-001）。
func (o *Orchestrator) buildScript(task *domain.TestTask) (string, error) {
	flow, err := domain.ParseFlow(task.Flow)
	if err != nil {
		return "", err
	}
	interfaces := make([]domain.InterfaceSnapshot, 0, len(task.InterfaceSnapshots))
	for i := range task.InterfaceSnapshots {
		snap := task.InterfaceSnapshots[i]
		if len(snap.AuthHeadersEnc) > 0 {
			if o.decoder == nil {
				return "", fmt.Errorf("认证头解密器未配置")
			}
			headers, err := o.decoder(snap.AuthHeadersEnc)
			if err != nil {
				return "", fmt.Errorf("解密接口认证头失败: %w", err)
			}
			if snap.Headers == nil {
				snap.Headers = map[string]string{}
			}
			for name, value := range headers {
				snap.Headers[name] = value
			}
			snap.AuthHeadersEnc = nil // 明文仅存在于内存与脚本字面量，不再回写
		}
		interfaces = append(interfaces, snap)
	}
	return k6.BuildScript(k6.ScriptSpec{
		Flow:       *flow,
		Interfaces: interfaces,
		MaxVUs:     o.maxVUs,
		TargetHost: task.TargetHost,
	})
}

// targetVUs 解析目标并发（concurrency 指标口径）。
func (o *Orchestrator) targetVUs(task *domain.TestTask) int {
	flow, err := domain.ParseFlow(task.Flow)
	if err != nil {
		return 0
	}
	params, err := flow.Params(o.maxVUs)
	if err != nil {
		return 0
	}
	return params.VUs
}

// saveResult 固化执行/评估结果（报告与历史查询复用）。
func (o *Orchestrator) saveResult(ctx context.Context, taskID int64, metrics map[evaluator.Metric]float64, evaluation evaluator.Evaluation, wall time.Duration) error {
	payload := struct {
		Engine          string                       `json:"engine"`
		DurationSeconds float64                      `json:"duration_seconds"`
		Metrics         map[evaluator.Metric]float64 `json:"metrics"`
		Evaluation      evaluator.Evaluation         `json:"evaluation"`
	}{
		Engine:          "k6",
		DurationSeconds: wall.Seconds(),
		Metrics:         metrics,
		Evaluation:      evaluation,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("序列化执行结果失败: %w", err)
	}
	if err := o.store.SetTaskResult(ctx, nil, taskID, raw); err != nil {
		return fmt.Errorf("写入执行结果失败: %w", err)
	}
	return nil
}

// ===================== 清理 =====================

// cleanPlan 解析清理计划：主机集合 = 环境绑定 SSH 主机 + 脚本显式主机。
func (o *Orchestrator) cleanPlan(ctx context.Context, task *domain.TestTask) (*cleaner.Plan, error) {
	hosts := make([]int64, 0, 2)
	if task.EnvID > 0 {
		if hostID, err := o.store.EnvironmentSshHost(ctx, task.EnvID); err == nil && hostID > 0 {
			hosts = append(hosts, hostID)
		}
	}
	// 环境未绑定 SSH 主机时，退化为清理脚本显式指定的主机（仍受白名单约束）。
	if len(hosts) == 0 {
		var cfg domain.CleanConfig
		if err := json.Unmarshal(task.CleanConfig, &cfg); err == nil {
			for _, script := range cfg.Scripts {
				hosts = append(hosts, script.HostID.Int64())
			}
		}
	}
	return o.resolve(task.CleanConfig, hosts)
}

// executeCleanup 执行清理：成功 cleaning → finished 并释放环境；失败保留待确认。
func (o *Orchestrator) executeCleanup(ctx context.Context, task *domain.TestTask, plan *cleaner.Plan) {
	if o.cleaner == nil {
		o.cleanupFailed(ctx, task.ID, fmt.Errorf("%w: 清理器未就绪", errcode.ErrDependency))
		return
	}
	res, err := o.cleaner.Run(ctx, plan)
	if err != nil {
		o.cleanupFailed(ctx, task.ID, err)
		return
	}
	if !res.OK() {
		o.cleanupFailed(ctx, task.ID, errors.New("清理步骤存在失败"))
		return
	}

	now := o.now()
	updCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), statusUpdateTimeout)
	defer cancel()
	if err := o.store.WithTx(updCtx, func(ctx context.Context, tx repository.Tx) error {
		if err := o.store.TransitionTask(ctx, tx, task.ID, domain.TaskCleaning, domain.TaskFinished); err != nil {
			return err
		}
		if err := o.store.SetTaskFinished(ctx, tx, task.ID, now); err != nil {
			return err
		}
		return o.store.ReleaseEnvironment(ctx, tx, task.EnvID)
	}); err != nil {
		o.logBackground(task.ID, "error", "清理完成但状态回写失败: "+err.Error())
		return
	}
	o.logBackground(task.ID, "info", "环境清理完成，任务已结束")
	o.publish(task.ID, "status", map[string]any{"status": domain.TaskFinished, "message": "环境清理完成"})
}

// cleanupFailed 清理失败：cleaning → cleanup_pending（保留现场，等待人工确认）。
func (o *Orchestrator) cleanupFailed(ctx context.Context, taskID int64, cause error) {
	o.logBackground(taskID, "error", "环境清理失败（保留现场待确认）: "+cause.Error())
	o.failCleanup(ctx, taskID)
}

// failCleanup 状态回写到 cleanup_pending（尽力而为）。
func (o *Orchestrator) failCleanup(ctx context.Context, taskID int64) {
	updCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), statusUpdateTimeout)
	defer cancel()
	if err := o.store.TransitionTask(updCtx, nil, taskID, domain.TaskCleaning, domain.TaskCleanupPending); err != nil {
		o.logBackground(taskID, "error", "回写清理待确认失败: "+err.Error())
		return
	}
	o.publish(taskID, "status", map[string]any{"status": domain.TaskCleanupPending, "message": "清理失败，保留现场待确认"})
}

// ===================== 失败处理 =====================

// failTask 执行异常：当前状态 → failed → cleanup_pending（不自动清理，保留现场）。
func (o *Orchestrator) failTask(ctx context.Context, taskID int64, cause error) {
	// 已被取消：状态迁移由服务层（running → cancelled → cleanup_pending）负责，避免双写竞态。
	if ctx != nil && ctx.Err() != nil {
		return
	}
	o.logBackground(taskID, "error", "任务执行失败: "+cause.Error())

	updCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), statusUpdateTimeout)
	defer cancel()

	task, err := o.store.GetTask(updCtx, taskID)
	if err != nil {
		return
	}
	if domain.CanTransition(task.Status, domain.TaskFailed) {
		if err := o.store.TransitionTask(updCtx, nil, taskID, task.Status, domain.TaskFailed); err != nil {
			o.logBackground(taskID, "error", "回写失败状态失败: "+err.Error())
			return
		}
		task.Status = domain.TaskFailed
	}
	_ = o.store.SetTaskFinished(updCtx, nil, taskID, o.now())
	if task.Status == domain.TaskFailed {
		if err := o.store.TransitionTask(updCtx, nil, taskID, domain.TaskFailed, domain.TaskCleanupPending); err != nil {
			o.logBackground(taskID, "error", "回写清理待确认失败: "+err.Error())
			return
		}
	}
	o.publish(taskID, "status", map[string]any{"status": domain.TaskCleanupPending, "message": "任务失败，保留现场待确认"})
}

// ===================== 状态/日志/推送 =====================

// transition 状态流转（失败返回错误，由调用方决定是否转失败态）。
func (o *Orchestrator) transition(ctx context.Context, taskID int64, from, to domain.TaskStatus) error {
	if err := o.store.TransitionTask(ctx, nil, taskID, from, to); err != nil {
		return fmt.Errorf("任务状态流转 %s→%s 失败: %w", from, to, err)
	}
	return nil
}

// markFinished 记录结束时间。
func (o *Orchestrator) markFinished(ctx context.Context, taskID int64) {
	_ = o.store.SetTaskFinished(ctx, nil, taskID, o.now())
}

// log 写测试日志并推送（推送/落库前统一脱敏，DES-I-001）。
func (o *Orchestrator) log(ctx context.Context, taskID int64, level, message string) {
	o.writeLog(ctx, taskID, level, message)
}

// logBackground 使用独立上下文写日志（取消/异常路径仍可追溯）。
func (o *Orchestrator) logBackground(taskID int64, level, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	o.writeLog(ctx, taskID, level, message)
}

// writeLog 脱敏 → 落库（test_log）→ WebSocket 推送。
func (o *Orchestrator) writeLog(ctx context.Context, taskID int64, level, message string) {
	message = strings.TrimSpace(logutil.SanitizeLog(message))
	if message == "" {
		return
	}
	seq := o.seq.next(taskID)
	if o.store != nil {
		if err := o.store.AppendTestLog(ctx, taskID, seq, level, message); err != nil {
			// 日志写入失败不阻断执行，仅记录到进程日志。
			fmt.Fprintf(os.Stderr, "[orchestrator] 写入测试日志失败 task=%d: %v\n", taskID, err)
		}
	}
	payload := map[string]any{
		"task_id": fmt.Sprintf("%d", taskID),
		"seq":     seq,
		"level":   level,
		"message": message,
		"time":    o.now().UTC().Format(time.RFC3339),
	}
	o.publish(taskID, "log", payload)
	if o.hub != nil {
		o.hub.Publish("logs:test", "log", payload)
	}
}

// publish 推送任务事件（hub 为 nil 时静默）。
func (o *Orchestrator) publish(taskID int64, event string, payload any) {
	if o == nil || o.hub == nil {
		return
	}
	o.hub.Publish(fmt.Sprintf("task:%d", taskID), event, payload)
}

// ===================== 转换辅助 =====================

// toEvaluatorSamples k6 样本 → 评估层样本。
func toEvaluatorSamples(samples []k6.Sample) []evaluator.Sample {
	out := make([]evaluator.Sample, 0, len(samples))
	for _, s := range samples {
		out = append(out, evaluator.Sample{Metric: s.Metric, Value: s.Value, Time: s.Time})
	}
	return out
}

// toThresholds 阈值快照 → 评估阈值。
func toThresholds(snapshots []domain.ThresholdSnapshot) []evaluator.Threshold {
	out := make([]evaluator.Threshold, 0, len(snapshots))
	for _, s := range snapshots {
		out = append(out, evaluator.Threshold{
			InterfaceID:   s.InterfaceID,
			InterfaceName: s.InterfaceName,
			Metric:        evaluator.Metric(s.Metric),
			Operator:      s.Operator,
			Value:         s.Value,
			Unit:          s.Unit,
			Enabled:       s.Enabled,
		})
	}
	return out
}

// releaseEnv 任务成功后释放环境占用（testing → pending），使环境可再次使用。
// 与清理解耦：即使清理失败或未配置清理，环境也应释放。
func (o *Orchestrator) releaseEnv(ctx context.Context, taskID, envID int64) {
	if envID <= 0 {
		return
	}
	updCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), statusUpdateTimeout)
	defer cancel()
	if err := o.store.WithTx(updCtx, func(ctx context.Context, tx repository.Tx) error {
		return o.store.ReleaseEnvironment(ctx, tx, envID)
	}); err != nil {
		o.logBackground(taskID, "error", "释放环境失败: "+err.Error())
	}
}

// runCleanupBestEffort 尽力执行可选清理：失败仅记录日志，不改变任务状态。
func (o *Orchestrator) runCleanupBestEffort(ctx context.Context, task *domain.TestTask, plan *cleaner.Plan) {
	if o.cleaner == nil {
		o.logBackground(task.ID, "warn", "清理器未就绪，跳过环境清理（不影响任务结果）")
		return
	}
	res, err := o.cleaner.Run(ctx, plan)
	if err != nil {
		o.logBackground(task.ID, "error", "环境清理失败（不影响任务结果）: "+err.Error())
		return
	}
	if !res.OK() {
		o.logBackground(task.ID, "warn", "环境清理存在失败步骤（不影响任务结果）")
		return
	}
	o.logBackground(task.ID, "info", "环境清理完成")
}
