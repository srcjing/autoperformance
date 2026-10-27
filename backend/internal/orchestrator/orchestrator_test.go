package orchestrator

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"auto_performance/internal/cleaner"
	"auto_performance/internal/domain"
	"auto_performance/internal/repository"
)

// fakeStore 内存编排器仓储（无 DB 依赖）。
type fakeStore struct {
	mu          sync.Mutex
	task        *domain.TestTask
	sshHost     int64
	logs        []string
	transitions []string
	released    bool
	finishedAt  *time.Time
}

func (f *fakeStore) GetTask(_ context.Context, _ int64) (*domain.TestTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.task == nil {
		return nil, errors.New("task not found")
	}
	cp := *f.task
	return &cp, nil
}

func (f *fakeStore) TransitionTask(_ context.Context, _ repository.Tx, _ int64, from, to domain.TaskStatus) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.task == nil || f.task.Status != from {
		return errors.New("status mismatch")
	}
	if !domain.CanTransition(from, to) {
		return errors.New("illegal transition")
	}
	f.task.Status = to
	f.transitions = append(f.transitions, string(from)+"->"+string(to))
	return nil
}

func (f *fakeStore) SetTaskStarted(_ context.Context, _ repository.Tx, _ int64, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.task.StartedAt = &at
	return nil
}

func (f *fakeStore) SetTaskFinished(_ context.Context, _ repository.Tx, _ int64, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finishedAt = &at
	return nil
}

func (f *fakeStore) SetTaskResult(_ context.Context, _ repository.Tx, _ int64, _ []byte) error {
	return nil
}

func (f *fakeStore) ReleaseEnvironment(_ context.Context, _ repository.Tx, _ int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.released = true
	return nil
}

func (f *fakeStore) EnvironmentSshHost(_ context.Context, _ int64) (int64, error) {
	return f.sshHost, nil
}

func (f *fakeStore) WithTx(ctx context.Context, fn repository.TxFunc) error { return fn(ctx, nil) }

func (f *fakeStore) AppendTestLog(_ context.Context, _, _ int64, _, message string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logs = append(f.logs, message)
	return nil
}

// fakeCleanRunner 清理执行器桩。
type fakeCleanRunner struct {
	err   error
	calls int
}

func (f *fakeCleanRunner) Run(_ context.Context, _ int64, _ string, _ []string) (string, error) {
	f.calls++
	if f.err != nil {
		return "", f.err
	}
	return "ok", nil
}

func newCleanTask(status domain.TaskStatus) *domain.TestTask {
	return &domain.TestTask{
		ID:          42,
		Name:        "任务",
		EnvID:       9,
		Status:      status,
		CleanConfig: []byte(`{"actions":["clean_data"]}`),
		Flow:        []byte(`{"concurrency_mode":"fixed","target_vus":5,"duration":"10s"}`),
	}
}

// TestExecuteCleanupSuccess 清理成功：cleaning → finished 并释放环境（AC-TEST-2）。
func TestExecuteCleanupSuccess(t *testing.T) {
	store := &fakeStore{task: newCleanTask(domain.TaskCleaning), sshHost: 5}
	runner := &fakeCleanRunner{}
	o := New(Deps{
		Store:   store,
		Cleaner: cleaner.New(runner, cleaner.NewAllowlist([]int64{5}, nil)),
	})

	task, _ := store.GetTask(context.Background(), 42)
	plan, err := o.cleanPlan(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	o.executeCleanup(context.Background(), task, plan)

	if store.task.Status != domain.TaskFinished {
		t.Fatalf("清理成功后状态应为 finished, got %s", store.task.Status)
	}
	if !store.released {
		t.Fatal("清理成功后应释放环境占用")
	}
	if runner.calls == 0 {
		t.Fatal("内置清理动作未执行")
	}
	if store.finishedAt == nil {
		t.Fatal("清理成功后应写入结束时间")
	}
}

// TestExecuteCleanupFailureLeavesPending 清理失败：cleaning → cleanup_pending（保留现场）。
func TestExecuteCleanupFailureLeavesPending(t *testing.T) {
	store := &fakeStore{task: newCleanTask(domain.TaskCleaning), sshHost: 5}
	runner := &fakeCleanRunner{err: errors.New("ssh down")}
	o := New(Deps{
		Store:   store,
		Cleaner: cleaner.New(runner, cleaner.NewAllowlist([]int64{5}, nil)),
	})

	task, _ := store.GetTask(context.Background(), 42)
	plan, err := o.cleanPlan(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	o.executeCleanup(context.Background(), task, plan)

	if store.task.Status != domain.TaskCleanupPending {
		t.Fatalf("清理失败后应回到 cleanup_pending, got %s", store.task.Status)
	}
	if store.released {
		t.Fatal("清理失败不得释放环境（保留现场）")
	}
}

// TestCleanPlanRequiresSSHHost 环境未绑定 SSH 主机 → 清理计划无法构建（DES-T-003 可信主机）。
func TestCleanPlanRequiresSSHHost(t *testing.T) {
	store := &fakeStore{task: newCleanTask(domain.TaskCleaning), sshHost: 0}
	o := New(Deps{Store: store})
	task, _ := store.GetTask(context.Background(), 42)
	if _, err := o.cleanPlan(context.Background(), task); !errors.Is(err, cleaner.ErrNoHost) {
		t.Fatalf("缺少清理主机应返回 ErrNoHost, got %v", err)
	}
}

// TestFailTaskEntersCleanupPending 执行异常：当前状态 → failed → cleanup_pending。
func TestFailTaskEntersCleanupPending(t *testing.T) {
	store := &fakeStore{task: newCleanTask(domain.TaskRunning)}
	o := New(Deps{Store: store})

	o.failTask(context.Background(), 42, errors.New("k6 crashed"))

	if store.task.Status != domain.TaskCleanupPending {
		t.Fatalf("失败后应进入 cleanup_pending, got %s", store.task.Status)
	}
	if !strings.Contains(strings.Join(store.transitions, ","), "running->failed") {
		t.Fatalf("缺少 running→failed 流转: %v", store.transitions)
	}
	if len(store.logs) == 0 || !strings.Contains(store.logs[0], "任务执行失败") {
		t.Fatalf("失败原因未写入测试日志: %v", store.logs)
	}
}

// TestBuildScriptDecryptsAuthHeaders 认证头密文在执行期解密注入脚本，且明文不回写快照（INV-7）。
func TestBuildScriptDecryptsAuthHeaders(t *testing.T) {
	store := &fakeStore{task: newCleanTask(domain.TaskRunning)}
	store.task.InterfaceSnapshots = []domain.InterfaceSnapshot{{
		InterfaceID:    11,
		Name:           "查询订单",
		Method:         "GET",
		URL:            "https://api.example.test/orders",
		Headers:        map[string]string{"X-Trace-Id": "abc"},
		AuthHeadersEnc: []byte("cipher"),
		Timeouts:       domain.InterfaceTimeouts{TotalMS: 1000},
		Status:         domain.InterfaceStatusEnabled,
		CapturedAt:     time.Now(),
	}}
	o := New(Deps{
		Store:  store,
		MaxVUs: 100,
		SecretDecoder: func(enc []byte) (map[string]string, error) {
			if string(enc) != "cipher" {
				return nil, errors.New("unexpected ciphertext")
			}
			return map[string]string{"Authorization": "Bearer token-123"}, nil
		},
	})

	task, _ := store.GetTask(context.Background(), 42)
	script, err := o.buildScript(task)
	if err != nil {
		t.Fatalf("脚本生成失败: %v", err)
	}
	if !strings.Contains(script, `Authorization`) || !strings.Contains(script, `Bearer token-123`) {
		t.Fatalf("认证头未注入脚本:\n%s", script)
	}
	if strings.Contains(script, "cipher") {
		t.Fatal("脚本不得包含认证头密文")
	}
}

// TestToThresholds 阈值快照转换为评估阈值（含停用项，由评估器过滤）。
func TestToThresholds(t *testing.T) {
	got := toThresholds([]domain.ThresholdSnapshot{
		{InterfaceID: 1, Metric: "p95", Operator: "le", Value: 500, Unit: "ms", Enabled: true},
		{InterfaceID: 1, Metric: "error_rate", Operator: "le", Value: 1, Enabled: false},
	})
	if len(got) != 2 || got[0].Metric != "p95" || !got[0].Enabled || got[1].Enabled {
		t.Fatalf("阈值转换异常: %+v", got)
	}
}
