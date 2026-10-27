package product

import (
	"context"
	"errors"
	"testing"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// boolPtr 便于在表驱动用例里构造 *bool。
func boolPtr(v bool) *bool { return &v }

// TestReplaceThresholdsRejectsMismatch 阈值方向与指标不匹配 → 22003，且整体不落库（INV-2）。
func TestReplaceThresholdsRejectsMismatch(t *testing.T) {
	svc, repo := newTestService()
	addProduct(repo, 100, "网关服务", "gateway", domain.ProductStatusEnabled)
	addInterface(repo, 200, 100, "健康检查", domain.InterfaceStatusEnabled, nil)
	ctx := context.Background()

	invalid := [][]ThresholdInput{
		{{Metric: "success_rate", Operator: "le", Value: 99}},
		{{Metric: "p95", Operator: "ge", Value: 500}},
		{{Metric: "error_rate", Operator: "ge", Value: 1}},
		{{Metric: "concurrency", Operator: "le", Value: 10}},
		{{Metric: "unknown", Operator: "le", Value: 1}},
		{{Metric: "p95", Operator: "lt", Value: 1}},
	}
	for _, items := range invalid {
		if _, err := svc.ReplaceThresholds(ctx, 200, items); !errors.Is(err, ErrThresholdMismatch) {
			t.Errorf("%+v want 22003, got %v", items, err)
		}
	}
	if repo.thresholdWrite != 0 {
		t.Fatal("非法组合不得落库")
	}

	if _, err := svc.ReplaceThresholds(ctx, 999, []ThresholdInput{
		{Metric: "p95", Operator: "le", Value: 500},
	}); !errors.Is(err, errcode.ErrNotFound) {
		t.Fatalf("接口不存在 want 10301, got %v", err)
	}
}

// TestReplaceThresholdsReplacesAll 全量替换：旧阈值被清空，新阈值按 PRD 字段保存，
// 单位/启用缺省值补齐（PRD §4.6）。
func TestReplaceThresholdsReplacesAll(t *testing.T) {
	svc, repo := newTestService()
	addProduct(repo, 100, "网关服务", "gateway", domain.ProductStatusEnabled)
	addInterface(repo, 200, 100, "健康检查", domain.InterfaceStatusEnabled, nil)
	ctx := context.Background()

	first, err := svc.ReplaceThresholds(ctx, 200, []ThresholdInput{
		{Metric: "p95", Operator: "le", Value: 500},
	})
	if err != nil {
		t.Fatalf("首次设置阈值失败: %v", err)
	}
	if len(first) != 1 || first[0].Unit != "ms" || !first[0].Enabled {
		t.Fatalf("默认单位/启用未补齐: %+v", first)
	}

	second, err := svc.ReplaceThresholds(ctx, 200, []ThresholdInput{
		{Metric: "p99", Operator: "le", Value: 800, Unit: "ms", Enabled: boolPtr(true)},
		{Metric: "error_rate", Operator: "le", Value: 1.5, Unit: "%", Enabled: boolPtr(false)},
		{Metric: "throughput", Operator: "ge", Value: 200, Unit: "req/s", Enabled: boolPtr(true)},
	})
	if err != nil {
		t.Fatalf("替换阈值失败: %v", err)
	}
	if repo.thresholdWrite != 2 {
		t.Fatalf("全量替换应每次写库一次，got %d", repo.thresholdWrite)
	}
	if len(second) != 3 {
		t.Fatalf("替换后应有 3 条阈值: %+v", second)
	}
	metrics := map[string]bool{}
	for _, item := range second {
		metrics[item.Metric] = true
		if item.InterfaceID != 200 {
			t.Fatalf("阈值归属异常: %+v", item)
		}
	}
	for _, want := range []string{"p95"} {
		if metrics[want] {
			t.Fatalf("旧阈值 %s 应被替换掉: %+v", want, second)
		}
	}
	if !metrics["p99"] || !metrics["error_rate"] || !metrics["throughput"] {
		t.Fatalf("新阈值未生效: %+v", second)
	}
	if second[1].Enabled {
		t.Fatalf("enabled=false 未生效: %+v", second[1])
	}

	// 全量替换为空 → 清空所有阈值。
	empty, err := svc.ReplaceThresholds(ctx, 200, nil)
	if err != nil {
		t.Fatalf("清空阈值失败: %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("清空后不应有阈值: %+v", empty)
	}
}

// TestReplaceThresholdsRejectsBadValues 数值/重复指标等非方向类非法输入 → 10003。
func TestReplaceThresholdsRejectsBadValues(t *testing.T) {
	svc, repo := newTestService()
	addProduct(repo, 100, "网关服务", "gateway", domain.ProductStatusEnabled)
	addInterface(repo, 200, 100, "健康检查", domain.InterfaceStatusEnabled, nil)
	ctx := context.Background()

	cases := [][]ThresholdInput{
		{{Metric: "p95", Operator: "le", Value: -1}},
		{{Metric: "error_rate", Operator: "le", Value: 101}},
		{{Metric: "p95", Operator: "le", Value: 500}, {Metric: "p95", Operator: "le", Value: 600}},
	}
	for _, items := range cases {
		if _, err := svc.ReplaceThresholds(ctx, 200, items); !errors.Is(err, errcode.ErrParamFmt) {
			t.Errorf("%+v want 10003, got %v", items, err)
		}
	}
	if repo.thresholdWrite != 0 {
		t.Fatal("非法阈值不得落库")
	}
}

// TestListThresholdsRequiresInterface 阈值列表要求接口存在（10301）。
func TestListThresholdsRequiresInterface(t *testing.T) {
	svc, repo := newTestService()
	addProduct(repo, 100, "网关服务", "gateway", domain.ProductStatusEnabled)
	addInterface(repo, 200, 100, "健康检查", domain.InterfaceStatusEnabled, nil)
	ctx := context.Background()

	if _, err := svc.ListThresholds(ctx, 200); err != nil {
		t.Fatalf("查询阈值失败: %v", err)
	}
	if _, err := svc.ListThresholds(ctx, 999); !errors.Is(err, errcode.ErrNotFound) {
		t.Fatalf("接口不存在 want 10301, got %v", err)
	}
}
