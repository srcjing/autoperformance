package orchestrator

// 任务全流程端到端集成测试（Phase 9 任务 9.2）。
//
// 覆盖：建环境/产品/接口/阈值 → 建任务（五要素 + 快照）→ start → 状态机流转 →
// 阈值评估 → 报告生成（html/pdf/word 三格式，PDF 用桩替代 headless Chromium）→
// 成功自动清理 / 失败待确认 → 人工确认清理完成。
//
// 依赖全部为内存假实现（无 PostgreSQL / k6 / Chromium）：仓储 e2eStore、
// 引擎 fakeEngine（写出真实 k6 --out json 结果文件）、清理 runner e2eCleanRunner。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"auto_performance/internal/cleaner"
	"auto_performance/internal/crypto"
	"auto_performance/internal/domain"
	"auto_performance/internal/evaluator"
	envsvc "auto_performance/internal/service/environment"
	productsvc "auto_performance/internal/service/product"
	reportsvc "auto_performance/internal/service/report"
	testingsvc "auto_performance/internal/service/testing"
)

// ===================== 假实现 =====================

// fakeEngine 假压测引擎：写出真实格式的 k6 JSON 结果文件，并按需模拟执行失败。
type fakeEngine struct {
	dir string
	// fail 非 nil 时执行失败（模拟 k6 异常退出）。
	fail error
	// samples 结果样本（默认 5 条 http_req_duration/http_reqs/http_req_failed）。
	samples []byte

	mu      sync.Mutex
	scripts []string
}

func newFakeEngine(t *testing.T) *fakeEngine {
	t.Helper()
	return &fakeEngine{dir: t.TempDir(), samples: k6ResultLines(time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC))}
}

func (f *fakeEngine) WorkDir() string { return f.dir }

func (f *fakeEngine) ScriptPath(taskID int64) string {
	return filepath.Join(f.dir, fmt.Sprintf("script-%d.js", taskID))
}

func (f *fakeEngine) ResultPath(taskID int64) string {
	return filepath.Join(f.dir, fmt.Sprintf("result-%d.json", taskID))
}

func (f *fakeEngine) Run(_ context.Context, scriptPath, resultPath string, onLine func(line string)) error {
	raw, err := os.ReadFile(scriptPath)
	if err != nil {
		return fmt.Errorf("读取 k6 脚本失败: %w", err)
	}
	f.mu.Lock()
	f.scripts = append(f.scripts, string(raw))
	f.mu.Unlock()

	if onLine != nil {
		onLine("k6 开始执行（fake engine）")
	}
	if f.fail != nil {
		return f.fail
	}
	if onLine != nil {
		onLine("k6 执行完成（fake engine）")
	}
	return os.WriteFile(resultPath, f.samples, 0o600)
}

// lastScript 返回最近一次生成的脚本内容。
func (f *fakeEngine) lastScript() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.scripts) == 0 {
		return ""
	}
	return f.scripts[len(f.scripts)-1]
}

// k6ResultLines 生成 k6 `--out json` 行格式样本：5 个请求，响应时间 120~160ms，全部成功。
func k6ResultLines(start time.Time) []byte {
	durations := []float64{120, 130, 140, 150, 160}
	var b strings.Builder
	for i, d := range durations {
		ts := start.Add(time.Duration(i) * time.Second).Format(time.RFC3339Nano)
		for _, metric := range []struct {
			name  string
			value float64
		}{
			{"http_req_duration", d},
			{"http_reqs", 1},
			{"http_req_failed", 0},
		} {
			line := map[string]any{
				"metric": metric.name,
				"type":   "Point",
				"data": map[string]any{
					"time":  ts,
					"value": metric.value,
					"tags":  map[string]string{"url": "https://order.example.test/api/orders"},
				},
			}
			raw, _ := json.Marshal(line)
			b.Write(raw)
			b.WriteByte('\n')
		}
	}
	return []byte(b.String())
}

// e2eCleanRunner 假清理执行器（记录调用次数，可模拟失败）。
type e2eCleanRunner struct {
	mu    sync.Mutex
	calls int
	fail  error
}

func (r *e2eCleanRunner) Run(_ context.Context, _ int64, _ string, _ []string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.fail != nil {
		return "", r.fail
	}
	return "ok", nil
}

func (r *e2eCleanRunner) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// fakePublisher 记录实时推送事件（验证状态/指标/评估/日志推送链路）。
type fakePublisher struct {
	mu     sync.Mutex
	events []string
}

func (p *fakePublisher) Publish(channel, event string, _ any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, channel+"|"+event)
}

func (p *fakePublisher) has(channel, event string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	want := channel + "|" + event
	for _, e := range p.events {
		if e == want {
			return true
		}
	}
	return false
}

// e2ePDF PDF 生成桩：校验 HTML 源已落盘后写出最小 PDF 文件（替代 headless Chromium）。
type e2ePDF struct {
	mu    sync.Mutex
	calls int
}

func (p *e2ePDF) Generate(_ context.Context, htmlPath, outPath string) error {
	if _, err := os.Stat(htmlPath); err != nil {
		return fmt.Errorf("PDF 打印缺少 HTML 源: %w", err)
	}
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	return os.WriteFile(outPath, []byte("%PDF-1.4\n% e2e stub pdf\n%%EOF\n"), 0o640)
}

// e2eSecretDecoder 认证头解密器（与 cmd/server 的 interfaceHeaderDecoder 等价）。
func e2eSecretDecoder(key []byte) SecretDecoder {
	return func(enc []byte) (map[string]string, error) {
		plain, err := crypto.DecryptGCM(enc, key)
		if err != nil {
			return nil, err
		}
		out := map[string]string{}
		if err := json.Unmarshal(plain, &out); err != nil {
			return nil, err
		}
		return out, nil
	}
}

// ===================== 断言辅助 =====================

func waitTaskStatus(t *testing.T, store *e2eStore, taskID int64, want domain.TaskStatus) *domain.TestTask {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		task, err := store.GetTask(context.Background(), taskID)
		if err != nil {
			t.Fatalf("查询任务 %d 失败: %v", taskID, err)
		}
		if task.Status == want {
			return task
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待任务状态 %s 超时，当前 %s（流转=%v）", want, task.Status, store.transitionLog())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func assertTransitions(t *testing.T, store *e2eStore, want []string) {
	t.Helper()
	got := store.transitionLog()
	if len(got) != len(want) {
		t.Fatalf("状态流转轨迹长度不符\n got=%v\nwant=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("第 %d 步状态流转 = %s, want %s\n got=%v\nwant=%v", i+1, got[i], want[i], got, want)
		}
	}
}

// ===================== 主用例 =====================

// TestEndToEndTaskFlow 任务全流程集成测试（AC-TEST-1~4 / AC-REPORT-1 / INV-3/4/5）。
func TestEndToEndTaskFlow(t *testing.T) {
	ctx := context.Background()
	store := newE2EStore()
	headerKey := productsvc.DeriveHeaderKey("", "e2e-secret-key", "e2e-jwt-secret")

	// ---------- 1. 建环境（分类/标签/环境）----------
	envSvc := envsvc.New(store)
	category, err := envSvc.CreateCategory(ctx, envsvc.CategoryInput{Name: "单机环境", Description: "单机部署"})
	if err != nil {
		t.Fatalf("创建环境分类失败: %v", err)
	}
	tag, err := envSvc.CreateTag(ctx, envsvc.TagInput{Name: "预发", Color: "#1677ff"})
	if err != nil {
		t.Fatalf("创建标签失败: %v", err)
	}
	env, err := envSvc.CreateEnvironment(ctx, envsvc.EnvironmentInput{
		Name:       "预发环境",
		Code:       "PRE-01",
		CategoryID: category.ID,
		Type:       domain.EnvTypeSingle,
		Hosts:      []string{"10.10.0.11"},
		OS:         "linux",
		SshHostID:  5, // 与清理主机白名单一致（DES-T-003）
		TagIDs:     []int64{tag.ID},
	})
	if err != nil {
		t.Fatalf("创建环境失败: %v", err)
	}
	if env.Status != domain.EnvStatusPending {
		t.Fatalf("新建环境状态 = %s, want pending", env.Status)
	}

	// ---------- 2. 建产品 / 接口（认证头加密）/ 阈值 ----------
	prodSvc := productsvc.New(store, headerKey)
	product, err := prodSvc.CreateProduct(ctx, productsvc.ProductInput{
		Name: "订单服务", Code: "order-svc", ProductVersion: "v1.0.0", Platform: "linux",
	})
	if err != nil {
		t.Fatalf("创建产品失败: %v", err)
	}
	api, err := prodSvc.CreateInterface(ctx, product.ID, productsvc.InterfaceInput{
		Name:   "查询订单",
		Method: domain.MethodGet,
		URL:    "https://order.example.test/api/orders?id=1",
		Headers: map[string]string{
			"X-Trace-Id":    "trace-e2e",
			"Authorization": "Bearer e2e-secret-token",
		},
		Timeouts: domain.InterfaceTimeouts{TotalMS: 3000},
	})
	if err != nil {
		t.Fatalf("创建接口失败: %v", err)
	}
	// INV-7 / DES-I-001：响应只暴露认证头名称，不含明文。
	if len(api.AuthHeaders) != 1 || api.AuthHeaders[0] != "Authorization" {
		t.Fatalf("接口视图认证头名称异常: %v", api.AuthHeaders)
	}
	if _, leaked := api.Headers["Authorization"]; leaked {
		t.Fatal("普通请求头不得包含认证头明文")
	}
	if _, err := prodSvc.ReplaceThresholds(ctx, api.ID, []productsvc.ThresholdInput{
		{Metric: "p95", Operator: "le", Value: 500, Unit: "ms"},
		{Metric: "error_rate", Operator: "le", Value: 1, Unit: "%"},
	}); err != nil {
		t.Fatalf("写入阈值失败: %v", err)
	}

	// ---------- 3. 装配执行层（假 k6 引擎 + 假清理器 + 报告服务）----------
	engine := newFakeEngine(t)
	cleanRunner := &e2eCleanRunner{}
	pub := &fakePublisher{}
	pdfStub := &e2ePDF{}
	reportDir := t.TempDir()

	reportSvc, err := reportsvc.New(store, reportsvc.Options{Dir: reportDir, PDF: pdfStub})
	if err != nil {
		t.Fatalf("初始化报告服务失败: %v", err)
	}

	orch := New(Deps{
		Store:         store,
		Runner:        engine,
		Cleaner:       cleaner.New(cleanRunner, cleaner.NewAllowlist([]int64{5}, nil)),
		Hub:           pub,
		Report:        reportSvc,
		MaxVUs:        100,
		SecretDecoder: e2eSecretDecoder(headerKey),
	})
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		orch.Shutdown(shutdownCtx)
	})
	taskSvc := testingsvc.NewTaskService(store, orch).WithMaxVUs(100)

	// ---------- 4. 建任务（五要素 + 快照）----------
	task := createE2ETask(t, ctx, taskSvc, env.ID, product.ID, api.ID, "E2E 基线压测")

	if task.Status != domain.TaskPending {
		t.Fatalf("新建任务状态 = %s, want pending", task.Status)
	}
	// INV-4：创建任务不占用环境（执行时才占用）。
	if gotEnv, _ := store.GetEnvironment(ctx, env.ID); gotEnv.Status != domain.EnvStatusPending {
		t.Fatalf("创建任务后环境状态 = %s, want pending", gotEnv.Status)
	}
	// INV-5：接口范围与阈值已固化。
	if len(task.InterfaceSnapshots) != 1 {
		t.Fatalf("接口快照数量 = %d, want 1", len(task.InterfaceSnapshots))
	}
	snap := task.InterfaceSnapshots[0]
	if snap.InterfaceID != api.ID || snap.URL == "" || snap.Method != domain.MethodGet {
		t.Fatalf("接口快照内容异常: %+v", snap)
	}
	// INV-7：快照只保存认证头密文，不含明文。
	if len(snap.AuthHeadersEnc) == 0 {
		t.Fatal("接口快照应保存认证头密文（headers_enc）")
	}
	if strings.Contains(fmt.Sprintf("%v", snap.Headers), "e2e-secret-token") {
		t.Fatal("接口快照的普通请求头不得包含认证头明文")
	}
	if len(task.ThresholdSnapshots) != 2 {
		t.Fatalf("阈值快照数量 = %d, want 2", len(task.ThresholdSnapshots))
	}

	// ---------- 5. start → 状态机 → 评估 → 报告 → 自动清理 ----------
	if _, err := taskSvc.Start(ctx, task.ID); err != nil {
		t.Fatalf("启动任务失败: %v", err)
	}
	final := waitTaskStatus(t, store, task.ID, domain.TaskFinished)

	assertTransitions(t, store, []string{
		"pending->running",
		"running->collecting",
		"collecting->evaluating",
		"evaluating->generating",
		"generating->finished",
		"finished->cleaning", // 成功自动清理
		"cleaning->finished",
	})

	// 脚本生成：接口地址 + 执行期解密的认证头（INV-7 / INV-9）。
	script := engine.lastScript()
	if !strings.Contains(script, "https://order.example.test/api/orders") {
		t.Fatalf("k6 脚本缺少接口地址:\n%s", script)
	}
	if !strings.Contains(script, "e2e-secret-token") {
		t.Fatalf("k6 脚本缺少执行期解密的认证头:\n%s", script)
	}

	// 评估结果落库（INV-2：全部启用阈值达标才算达标）。
	if len(final.Result) == 0 {
		t.Fatal("任务执行结果未落库")
	}
	var result struct {
		Metrics    map[evaluator.Metric]float64 `json:"metrics"`
		Evaluation evaluator.Evaluation         `json:"evaluation"`
	}
	if err := json.Unmarshal(final.Result, &result); err != nil {
		t.Fatalf("解析任务结果失败: %v", err)
	}
	if !result.Evaluation.Pass || result.Evaluation.State != evaluator.StatePass {
		t.Fatalf("评估结果应达标, got state=%s pass=%v", result.Evaluation.State, result.Evaluation.Pass)
	}
	if p95 := result.Metrics[evaluator.MetricP95]; p95 < 100 || p95 > 500 {
		t.Fatalf("P95 指标异常: %v", p95)
	}

	// 成功自动清理：环境已释放回 pending，清理动作已执行。
	if gotEnv, _ := store.GetEnvironment(ctx, env.ID); gotEnv.Status != domain.EnvStatusPending {
		t.Fatalf("成功清理后环境状态 = %s, want pending", gotEnv.Status)
	}
	if cleanRunner.callCount() == 0 {
		t.Fatal("成功路径未执行清理动作")
	}

	// 测试日志落库 + 实时推送（脱敏后）。
	logs := store.logs()
	if len(logs) == 0 {
		t.Fatal("测试日志为空")
	}
	for _, l := range logs {
		if strings.Contains(l.Message, "e2e-secret-token") {
			t.Fatalf("测试日志泄露认证头明文: %s", l.Message)
		}
	}
	if !pub.has(fmt.Sprintf("task:%d", task.ID), "status") {
		t.Fatalf("未推送任务状态事件: %v", pub.events)
	}
	if !pub.has(fmt.Sprintf("task:%d", task.ID), "evaluation") {
		t.Fatalf("未推送评估结果事件: %v", pub.events)
	}

	// ---------- 6. 报告三格式生成 + 在线查看/导出 ----------
	page, err := reportSvc.List(ctx, reportsvc.ListQuery{TaskID: task.ID})
	if err != nil {
		t.Fatalf("查询报告失败: %v", err)
	}
	if page.Total != 1 || len(page.List) != 1 {
		t.Fatalf("任务报告数量 = %d, want 1", page.Total)
	}
	report := page.List[0]
	if report.Status != domain.ReportStatusReady {
		t.Fatalf("报告状态 = %s, want ready", report.Status)
	}
	if len(report.Artifacts) != 3 {
		t.Fatalf("报告产物数量 = %d, want 3（html/pdf/word）", len(report.Artifacts))
	}
	formats := map[string]bool{}
	for _, a := range report.Artifacts {
		formats[a.Format] = true
		if a.Size <= 0 {
			t.Fatalf("产物 %s 大小异常: %d", a.Format, a.Size)
		}
	}
	for _, want := range []string{domain.ReportFormatHTML, domain.ReportFormatPDF, domain.ReportFormatWord} {
		if !formats[want] {
			t.Fatalf("缺少 %s 报告产物: %v", want, formats)
		}
	}
	if pdfStub.calls != 1 {
		t.Fatalf("PDF 渲染调用次数 = %d, want 1", pdfStub.calls)
	}

	for _, tc := range []struct {
		format     string
		wantInline bool
		wantPrefix string
	}{
		{domain.ReportFormatHTML, true, "<!DOCTYPE html>"},
		{domain.ReportFormatPDF, true, "%PDF"},
		{domain.ReportFormatWord, false, "PK"},
	} {
		file, err := reportSvc.Artifact(ctx, report.ID, tc.format)
		if err != nil {
			t.Fatalf("读取 %s 产物失败: %v", tc.format, err)
		}
		if file.Inline != tc.wantInline {
			t.Fatalf("%s 产物 inline = %v, want %v", tc.format, file.Inline, tc.wantInline)
		}
		raw, err := os.ReadFile(file.Path)
		if err != nil {
			t.Fatalf("读取产物文件失败: %v", err)
		}
		if !strings.HasPrefix(string(raw), tc.wantPrefix) {
			t.Fatalf("%s 产物内容前缀不符: %.24q", tc.format, string(raw))
		}
	}
	// 路径穿越载荷（DES-T-002）：非法格式一律拒绝。
	if _, err := reportSvc.Artifact(ctx, report.ID, "../../etc/passwd"); !errors.Is(err, reportsvc.ErrReportFileNotFound) {
		t.Fatalf("路径穿越载荷应返回报告不存在, got %v", err)
	}

	// ---------- 7. 失败路径：failed → cleanup_pending（保留现场）----------
	failEngine := newFakeEngine(t)
	failEngine.fail = errors.New("k6 执行失败: exit status 1")
	failOrch := New(Deps{
		Store:         store,
		Runner:        failEngine,
		Cleaner:       cleaner.New(cleanRunner, cleaner.NewAllowlist([]int64{5}, nil)),
		Hub:           pub,
		Report:        reportSvc,
		MaxVUs:        100,
		SecretDecoder: e2eSecretDecoder(headerKey),
	})
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		failOrch.Shutdown(shutdownCtx)
	})
	failSvc := testingsvc.NewTaskService(store, failOrch).WithMaxVUs(100)

	failedTask := createE2ETask(t, ctx, failSvc, env.ID, product.ID, api.ID, "E2E 失败用例")
	if _, err := failSvc.Start(ctx, failedTask.ID); err != nil {
		t.Fatalf("启动失败用例失败: %v", err)
	}
	waitTaskStatus(t, store, failedTask.ID, domain.TaskCleanupPending)

	// 失败不自动清理：环境仍被占用，等待人工确认。
	if gotEnv, _ := store.GetEnvironment(ctx, env.ID); gotEnv.Status != domain.EnvStatusTesting {
		t.Fatalf("失败后环境状态 = %s, want testing（保留现场）", gotEnv.Status)
	}
	if released := store.envReleasedCount(); released != 1 {
		t.Fatalf("失败路径不得释放环境，已释放次数 = %d, want 1", released)
	}

	// ---------- 8. 人工确认清理：cleanup_pending → cleaning → finished ----------
	if _, err := failSvc.Cleanup(ctx, failedTask.ID); err != nil {
		t.Fatalf("人工确认清理失败: %v", err)
	}
	waitTaskStatus(t, store, failedTask.ID, domain.TaskFinished)
	if gotEnv, _ := store.GetEnvironment(ctx, env.ID); gotEnv.Status != domain.EnvStatusPending {
		t.Fatalf("人工清理后环境状态 = %s, want pending", gotEnv.Status)
	}
	if released := store.envReleasedCount(); released != 2 {
		t.Fatalf("人工清理后环境释放次数 = %d, want 2", released)
	}

	// 失败用例不产生报告（仅在 evaluable 成功后生成）。
	failedReports, err := reportSvc.List(ctx, reportsvc.ListQuery{TaskID: failedTask.ID})
	if err != nil {
		t.Fatalf("查询失败用例报告失败: %v", err)
	}
	if failedReports.Total != 0 {
		t.Fatalf("执行失败的任务不应生成报告, got %d", failedReports.Total)
	}

	// 非 cleanup_pending 状态不允许再次人工清理（23001）。
	if _, err := failSvc.Cleanup(ctx, failedTask.ID); !errors.Is(err, testingsvc.ErrInvalidTaskState) {
		t.Fatalf("finished 任务重复清理应返回状态非法, got %v", err)
	}
}

// createE2ETask 以五要素创建任务（不自动启动）。
func createE2ETask(t *testing.T, ctx context.Context, svc *testingsvc.Service,
	envID, productID, apiID int64, name string) *testingsvc.TaskView {
	t.Helper()
	task, err := svc.Create(ctx, testingsvc.CreateTaskInput{
		Name:         name,
		EnvID:        envID,
		ProductIDs:   []int64{productID},
		InterfaceIDs: []int64{apiID},
		Flow:         json.RawMessage(`{"concurrency_mode":"fixed","target_vus":5,"duration":"10s"}`),
		ReportConfig: json.RawMessage(`{"formats":["html","pdf","word"]}`),
		CleanConfig:  json.RawMessage(`{"actions":["clean_data","restore_config"]}`),
		CreatedBy:    7,
	})
	if err != nil {
		t.Fatalf("创建任务失败: %v", err)
	}
	return task
}

// 编译期断言：假引擎与 k6.Runner 具备相同的编排执行能力。
var _ EngineRunner = (*fakeEngine)(nil)
