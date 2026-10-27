package testing

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// validCreateTaskRequest 五要素齐全的任务创建请求（接口范围 + 流程 + 报告 + 清理）。
func validCreateTaskRequest() CreateTaskInput {
	return CreateTaskInput{
		Name:         "峰值压测",
		EnvID:        1,
		InterfaceIDs: []int64{11, 12},
		CreatedBy:    7,
		Flow:         json.RawMessage(`{"concurrency_mode":"fixed","target_vus":100,"duration":"60s","warmup":"10s","timeout":"5s","think_time":"0.5s"}`),
		ReportConfig: json.RawMessage(`{"formats":["html","pdf","word"]}`),
		CleanConfig:  json.RawMessage(`{"actions":["clean_data","release_resource"],"timing":"auto"}`),
	}
}

// TestCreateTaskFailsWhenEnvironmentBusy 环境处于 testing（被占用）时禁止创建（AC-TEST-3 / INV-4）。
func TestCreateTaskFailsWhenEnvironmentBusy(t *testing.T) {
	repo := &fakeTaskRepo{envStatus: domain.EnvStatusTesting}
	svc := NewTaskService(repo, nil)

	_, err := svc.Create(context.Background(), validCreateTaskRequest())
	if !errors.Is(err, ErrEnvBusy) {
		t.Fatalf("want ErrEnvBusy(23002), got %v", err)
	}
	if len(repo.tasks) != 0 {
		t.Fatal("环境被占用时不得落库任务")
	}
}

// TestCreateTaskSnapshotsInterfaceAndThreshold 创建时固化接口范围与阈值快照（AC-TEST-4 / INV-5）。
func TestCreateTaskSnapshotsInterfaceAndThreshold(t *testing.T) {
	repo := &fakeTaskRepo{envStatus: domain.EnvStatusPending}
	svc := NewTaskService(repo, nil)

	task, err := svc.Create(context.Background(), validCreateTaskRequest())
	if err != nil {
		t.Fatal(err)
	}
	if len(task.InterfaceSnapshots) == 0 || len(task.ThresholdSnapshots) == 0 {
		t.Fatal("snapshot must be populated")
	}
	if task.Status != domain.TaskPending {
		t.Fatalf("新建任务状态应为 pending, got %s", task.Status)
	}

	// 快照隔离：创建后修改接口/阈值（fake 中改变返回值），历史任务快照不得变化。
	stored, err := svc.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	repo.thresholdValue = 9999
	again, err := svc.Get(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.ThresholdSnapshots) != len(again.ThresholdSnapshots) ||
		again.ThresholdSnapshots[0].Value != 100 {
		t.Fatal("快照必须与后续阈值变更隔离")
	}
}

// TestCreateTaskRequiresAllElements 五要素缺项校验（AC-TEST-1）。
func TestCreateTaskRequiresAllElements(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*CreateTaskInput)
		want   *errcode.Error
	}{
		{"名称缺失", func(in *CreateTaskInput) { in.Name = " " }, errcode.ErrParamMiss},
		{"环境缺失", func(in *CreateTaskInput) { in.EnvID = 0 }, errcode.ErrParamMiss},
		{"接口范围为空", func(in *CreateTaskInput) { in.InterfaceIDs = nil }, ErrInterfaceRangeEmpty},
		{"流程缺失", func(in *CreateTaskInput) { in.Flow = nil }, errcode.ErrParamMiss},
		{"报告配置缺失", func(in *CreateTaskInput) { in.ReportConfig = nil }, errcode.ErrParamMiss},
		{"清理配置缺失", func(in *CreateTaskInput) { in.CleanConfig = nil }, errcode.ErrParamMiss},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := validCreateTaskRequest()
			tc.mutate(&in)
			svc := NewTaskService(&fakeTaskRepo{envStatus: domain.EnvStatusPending}, nil)
			if _, err := svc.Create(context.Background(), in); !errors.Is(err, tc.want) {
				t.Fatalf("want %v, got %v", tc.want, err)
			}
		})
	}
}

// TestCreateTaskRejectsInvalidConcurrency 并发数必须为正整数且 ≤ max_vus（23004，DES-T-001）。
func TestCreateTaskRejectsInvalidConcurrency(t *testing.T) {
	for _, vus := range []string{`"1;require('child_process').exec('id')"`, `0`, `-1`, `1.5`, `null`, `10001`} {
		in := validCreateTaskRequest()
		in.Flow = json.RawMessage(`{"concurrency_mode":"fixed","target_vus":` + vus + `,"duration":"60s"}`)
		svc := NewTaskService(&fakeTaskRepo{envStatus: domain.EnvStatusPending}, nil).WithMaxVUs(10000)
		if _, err := svc.Create(context.Background(), in); !errors.Is(err, ErrConcurrencyInvalid) {
			t.Fatalf("target_vus=%s 应返回 23004, got %v", vus, err)
		}
	}
}

// TestStartLocksEnvironment 启动任务：pending → running 并占用环境（INV-4）。
func TestStartLocksEnvironment(t *testing.T) {
	repo := &fakeTaskRepo{envStatus: domain.EnvStatusPending}
	exec := &fakeExecutor{}
	svc := NewTaskService(repo, exec)

	task, err := svc.Create(context.Background(), validCreateTaskRequest())
	if err != nil {
		t.Fatal(err)
	}
	view, err := svc.Start(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != domain.TaskRunning {
		t.Fatalf("status = %s, want running", view.Status)
	}
	if repo.envStatus != domain.EnvStatusTesting {
		t.Fatalf("环境应被占用为 testing, got %s", repo.envStatus)
	}
	if len(exec.started) != 1 || exec.started[0] != task.ID {
		t.Fatalf("编排器未被触发: %v", exec.started)
	}

	// 重复启动 → 23001（非 pending）。
	if _, err := svc.Start(context.Background(), task.ID); !errors.Is(err, ErrInvalidTaskState) {
		t.Fatalf("重复启动应 23001, got %v", err)
	}
}

// TestCancelRunningTask 停止执行中任务：终止子进程并进入清理待确认（AC-TEST-2 / INV-3）。
func TestCancelRunningTask(t *testing.T) {
	repo := &fakeTaskRepo{envStatus: domain.EnvStatusPending}
	exec := &fakeExecutor{}
	svc := NewTaskService(repo, exec)

	task, err := svc.Create(context.Background(), validCreateTaskRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(context.Background(), task.ID); err != nil {
		t.Fatal(err)
	}

	view, err := svc.Cancel(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(exec.cancelled) != 1 || exec.cancelled[0] != task.ID {
		t.Fatalf("未终止子进程: %v", exec.cancelled)
	}
	if view.Status != domain.TaskCleanupPending {
		t.Fatalf("取消后应保留现场待确认(cleanup_pending), got %s", view.Status)
	}
	if repo.envStatus != domain.EnvStatusTesting {
		t.Fatal("取消后应保留环境占用（保留现场）")
	}

	// 非 running 的取消 → 23001。
	if _, err := svc.Cancel(context.Background(), task.ID); !errors.Is(err, ErrInvalidTaskState) {
		t.Fatalf("重复取消应 23001, got %v", err)
	}
}

// TestCleanupOnlyFromCleanupPending 人工清理仅允许 cleanup_pending（AC-TEST-2）。
func TestCleanupOnlyFromCleanupPending(t *testing.T) {
	repo := &fakeTaskRepo{envStatus: domain.EnvStatusPending}
	exec := &fakeExecutor{}
	svc := NewTaskService(repo, exec)

	// pending → 拒绝
	pendingTask, err := svc.Create(context.Background(), validCreateTaskRequest())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Cleanup(context.Background(), pendingTask.ID); !errors.Is(err, ErrInvalidTaskState) {
		t.Fatalf("pending 清理应 23001, got %v", err)
	}

	// running → 拒绝
	if _, err := svc.Start(context.Background(), pendingTask.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Cleanup(context.Background(), pendingTask.ID); !errors.Is(err, ErrInvalidTaskState) {
		t.Fatalf("running 清理应 23001, got %v", err)
	}

	// 取消后 cleanup_pending → 允许
	if _, err := svc.Cancel(context.Background(), pendingTask.ID); err != nil {
		t.Fatal(err)
	}
	view, err := svc.Cleanup(context.Background(), pendingTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != domain.TaskCleaning {
		t.Fatalf("清理确认后应进入 cleaning, got %s", view.Status)
	}
	if len(exec.cleaned) != 1 {
		t.Fatalf("清理器未被触发: %v", exec.cleaned)
	}
}

// TestRetryFailedTask 失败重试：failed → pending 并释放环境占用。
func TestRetryFailedTask(t *testing.T) {
	repo := &fakeTaskRepo{envStatus: domain.EnvStatusPending}
	exec := &fakeExecutor{}
	svc := NewTaskService(repo, exec)

	task, err := svc.Create(context.Background(), validCreateTaskRequest())
	if err != nil {
		t.Fatal(err)
	}
	// 模拟编排器执行失败后的终态。
	repo.mustTransition(t, task.ID, domain.TaskPending, domain.TaskRunning)
	repo.mustTransition(t, task.ID, domain.TaskRunning, domain.TaskFailed)
	repo.mustTransition(t, task.ID, domain.TaskFailed, domain.TaskCleanupPending)
	repo.envStatus = domain.EnvStatusTesting

	view, err := svc.Retry(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != domain.TaskPending {
		t.Fatalf("重试后状态应为 pending, got %s", view.Status)
	}
	if repo.envStatus != domain.EnvStatusPending {
		t.Fatalf("重试应释放环境占用, got %s", repo.envStatus)
	}
	if view.StartedAt != nil || view.FinishedAt != nil {
		t.Fatal("重试应清空执行时间")
	}
}

// TestTaskNotFound 任务不存在 → 10301。
func TestTaskNotFound(t *testing.T) {
	svc := NewTaskService(&fakeTaskRepo{}, nil)
	if _, err := svc.Get(context.Background(), 999); !errors.Is(err, errcode.ErrNotFound) {
		t.Fatalf("want 10301, got %v", err)
	}
}

// TestListRejectsInvalidStatus 列表状态筛选白名单（非法状态 → 10003）。
func TestListRejectsInvalidStatus(t *testing.T) {
	svc := NewTaskService(&fakeTaskRepo{}, nil)
	if _, err := svc.List(context.Background(), ListTasksQuery{Status: "bogus"}); !errors.Is(err, errcode.ErrParamFmt) {
		t.Fatalf("非法状态应 10003, got %v", err)
	}
	if _, err := svc.List(context.Background(), ListTasksQuery{Status: string(domain.TaskPending), Page: 1, PageSize: 20}); err != nil {
		t.Fatalf("合法状态查询失败: %v", err)
	}
}
