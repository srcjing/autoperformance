// Package domain 领域模型（实体 + 枚举/状态机）。
//
// 本文件定义测试任务状态机（PRD §7.2 / api-contract §3 状态机，INV-3）：
// 合法流转用邻接表声明，非法流转在领域层即被拒绝，服务层统一映射业务码 23001。
package domain

import "errors"

// TaskStatus 测试任务状态（PRD §4.8 / §7.2）。
type TaskStatus string

// 任务状态常量（与 DB test_task.status 取值一致）。
const (
	// TaskPending 待执行。
	TaskPending TaskStatus = "pending"
	// TaskRunning 执行中（k6 子进程运行）。
	TaskRunning TaskStatus = "running"
	// TaskCollecting 采集中（解析引擎结果）。
	TaskCollecting TaskStatus = "collecting"
	// TaskEvaluating 评估中（阈值对比）。
	TaskEvaluating TaskStatus = "evaluating"
	// TaskGenerating 报告生成中。
	TaskGenerating TaskStatus = "generating"
	// TaskFinished 已完成（报告就绪 / 清理已完成）。
	TaskFinished TaskStatus = "finished"
	// TaskFailed 执行失败。
	TaskFailed TaskStatus = "failed"
	// TaskCancelled 已取消（手动停止）。
	TaskCancelled TaskStatus = "cancelled"
	// TaskCleanupPending 清理待确认（失败/取消保留现场，等待人工确认）。
	TaskCleanupPending TaskStatus = "cleanup_pending"
	// TaskCleaning 清理中。
	TaskCleaning TaskStatus = "cleaning"
	// TaskEnded 已结束（清理完成的终态）。
	TaskEnded TaskStatus = "ended"
)

// ErrInvalidTransition 任务状态流转非法（服务层映射为业务码 23001）。
//
// 领域层不依赖 service 包，避免 domain → service 的反向依赖；
// 业务错误码由 internal/service/testing 统一登记并映射。
var ErrInvalidTransition = errors.New("任务状态流转非法")

// taskTransitions 状态机邻接表（唯一事实源）。
//
// 与 PRD §7.2 / api-contract §3 对齐，并补充两处实现必需的动作边：
//   - cleaning → cleanup_pending：清理执行失败时回到待确认，避免任务卡在 cleaning；
//   - cleanup_pending → pending：失败任务在「重试」时以重试取代清理（释放环境后重新执行），
//     否则失败自动进入 cleanup_pending 后将永远无法重试（4.7 retry 语义）。
var taskTransitions = map[TaskStatus][]TaskStatus{
	TaskPending:        {TaskRunning},
	TaskRunning:        {TaskCollecting, TaskFailed, TaskCancelled},
	TaskCollecting:     {TaskEvaluating, TaskFailed},
	TaskEvaluating:     {TaskGenerating, TaskFailed},
	TaskGenerating:     {TaskFinished, TaskFailed},
	TaskFinished:       {TaskCleaning},
	TaskFailed:         {TaskCleanupPending, TaskPending},
	TaskCancelled:      {TaskCleanupPending},
	TaskCleanupPending: {TaskCleaning, TaskPending},
	// cleaning → finished：成功路径（plan 4.6「成功自动 cleaning→finished」）；
	// cleaning → ended：终态路径（plan 4.3 状态机用例）。
	TaskCleaning: {TaskFinished, TaskEnded, TaskCleanupPending},
	TaskEnded:    {},
}

// TaskStatuses 返回全部合法状态（顺序固定，供文档/前端下拉使用）。
func TaskStatuses() []TaskStatus {
	return []TaskStatus{
		TaskPending, TaskRunning, TaskCollecting, TaskEvaluating, TaskGenerating,
		TaskFinished, TaskFailed, TaskCancelled, TaskCleanupPending, TaskCleaning, TaskEnded,
	}
}

// IsValidTaskStatus 判断状态取值是否合法。
func IsValidTaskStatus(status TaskStatus) bool {
	_, ok := taskTransitions[status]
	return ok
}

// CanTransition 判断 from → to 是否为合法流转（同状态视为非法，便于暴露状态冲突）。
func CanTransition(from, to TaskStatus) bool {
	targets, ok := taskTransitions[from]
	if !ok {
		return false
	}
	if from == to {
		return false
	}
	for _, t := range targets {
		if t == to {
			return true
		}
	}
	return false
}

// Transition 校验流转合法性；非法返回 ErrInvalidTransition（服务层映射 23001）。
func Transition(from, to TaskStatus) error {
	if !CanTransition(from, to) {
		return ErrInvalidTransition
	}
	return nil
}

// CanStartTask 是否可执行（仅 pending）。
func CanStartTask(status TaskStatus) bool { return status == TaskPending }

// CanCancelTask 是否可取消（仅 running，对齐 api-contract §2.4「running → cancelled」）。
func CanCancelTask(status TaskStatus) bool { return status == TaskRunning }

// CanRetryTask 是否可重试（failed，或以重试取代清理的 cleanup_pending）。
func CanRetryTask(status TaskStatus) bool {
	return status == TaskFailed || status == TaskCleanupPending
}

// CanCleanupTask 是否可人工确认清理（仅 cleanup_pending）。
func CanCleanupTask(status TaskStatus) bool { return status == TaskCleanupPending }

// IsActiveTaskStatus 是否处于活跃执行阶段（占用子进程/环境）。
func IsActiveTaskStatus(status TaskStatus) bool {
	switch status {
	case TaskRunning, TaskCollecting, TaskEvaluating, TaskGenerating, TaskCleaning:
		return true
	default:
		return false
	}
}

// IsTerminalTaskStatus 是否终态（finished / ended）。cleanup_pending 不是终态：仍需人工确认。
func IsTerminalTaskStatus(status TaskStatus) bool {
	return status == TaskFinished || status == TaskEnded
}
