package domain

import "testing"

// TestTaskStateMachine 校验任务状态机邻接表（AC-TEST-2 / INV-3）。
//
// 用例与 specs/plan.md Phase 4 任务 4.3 一致：合法流转必须放行，
// 非法流转（成功跳待确认、失败直达完成、待确认/取消回执行）必须拒绝。
func TestTaskStateMachine(t *testing.T) {
	valid := [][2]TaskStatus{
		{TaskPending, TaskRunning}, {TaskRunning, TaskCollecting},
		{TaskCollecting, TaskEvaluating}, {TaskEvaluating, TaskGenerating},
		{TaskGenerating, TaskFinished}, {TaskFinished, TaskCleaning},
		{TaskRunning, TaskFailed}, {TaskRunning, TaskCancelled},
		{TaskFailed, TaskCleanupPending}, {TaskCancelled, TaskCleanupPending},
		{TaskCleanupPending, TaskCleaning}, {TaskCleaning, TaskEnded},
		{TaskFailed, TaskPending}, // retry
	}
	for _, p := range valid {
		if !CanTransition(p[0], p[1]) {
			t.Fatalf("expected %v -> %v", p[0], p[1])
		}
		if err := Transition(p[0], p[1]); err != nil {
			t.Fatalf("Transition(%v, %v) 应成功, got %v", p[0], p[1], err)
		}
	}

	invalid := [][2]TaskStatus{
		{TaskFinished, TaskCleanupPending}, // 成功不得跳待确认
		{TaskFailed, TaskFinished},         // 失败不得直达完成
		{TaskCleanupPending, TaskRunning},  // 待确认不得直接回执行
		{TaskCancelled, TaskRunning},       // 取消不得回执行
		{TaskPending, TaskFinished},        // 待执行不得跳过执行链路
	}
	for _, p := range invalid {
		if CanTransition(p[0], p[1]) {
			t.Fatalf("unexpected %v -> %v", p[0], p[1])
		}
		if err := Transition(p[0], p[1]); err == nil {
			t.Fatalf("Transition(%v, %v) 应失败", p[0], p[1])
		}
	}

	// 未知状态一律非法（防御脏数据 / 拼写错误）。
	if CanTransition(TaskStatus("bogus"), TaskRunning) {
		t.Fatal("未知源状态不应允许流转")
	}
	if CanTransition(TaskPending, TaskStatus("bogus")) {
		t.Fatal("未知目标状态不应允许流转")
	}
}

// TestTaskStatusHelpers 校验动作判定辅助函数（start/cancel/retry/cleanup 的入口约束）。
func TestTaskStatusHelpers(t *testing.T) {
	if !CanStartTask(TaskPending) || CanStartTask(TaskRunning) {
		t.Fatal("仅 pending 可启动")
	}
	if !CanCancelTask(TaskRunning) || CanCancelTask(TaskPending) || CanCancelTask(TaskFinished) {
		t.Fatal("仅 running 可取消")
	}
	if !CanRetryTask(TaskFailed) || !CanRetryTask(TaskCleanupPending) || CanRetryTask(TaskRunning) {
		t.Fatal("仅 failed / cleanup_pending 可重试")
	}
	if !CanCleanupTask(TaskCleanupPending) || CanCleanupTask(TaskFinished) || CanCleanupTask(TaskFailed) {
		t.Fatal("仅 cleanup_pending 可人工确认清理")
	}
	if !IsTerminalTaskStatus(TaskEnded) || !IsTerminalTaskStatus(TaskFinished) ||
		IsTerminalTaskStatus(TaskCleaning) || IsTerminalTaskStatus(TaskCleanupPending) {
		t.Fatal("终态判定异常")
	}
	if !IsActiveTaskStatus(TaskRunning) || !IsActiveTaskStatus(TaskCleaning) || IsActiveTaskStatus(TaskPending) {
		t.Fatal("活跃状态判定异常")
	}
}
