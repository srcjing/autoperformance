package evaluator

import "testing"

// TestEvaluateAllThresholdsMustPass 全启用阈值达标才算通过（INV-2 / AC-TEST-4）。
func TestEvaluateAllThresholdsMustPass(t *testing.T) {
	ths := []Threshold{
		{Metric: "p95", Operator: "le", Value: 500, Enabled: true},
		{Metric: "error_rate", Operator: "le", Value: 1.0, Enabled: true},
	}

	e := Evaluate(map[Metric]float64{"p95": 480, "error_rate": 1.5}, ths)
	if e.Pass {
		t.Fatal("error_rate 1.5 > 1.0 should fail")
	}
	if e.State != StateFail {
		t.Fatalf("State = %v, want %v", e.State, StateFail)
	}

	e2 := Evaluate(map[Metric]float64{"p95": 480, "error_rate": 0.5}, ths)
	if !e2.Pass {
		t.Fatal("both within threshold should pass")
	}
	if e2.State != StatePass {
		t.Fatalf("State = %v, want %v", e2.State, StatePass)
	}
	if len(e2.Items) != 2 {
		t.Fatalf("逐条结果应保留 2 条，got %d", len(e2.Items))
	}

	// 停用阈值不参与判定：即使不达标也不影响结论。
	ths[1].Enabled = false
	if e3 := Evaluate(map[Metric]float64{"p95": 480, "error_rate": 99}, ths); !e3.Pass {
		t.Fatal("停用阈值不应参与判定")
	}
}

// TestEvaluateUnconfiguredAndMissing 无启用阈值 / 指标缺失的状态口径。
func TestEvaluateUnconfiguredAndMissing(t *testing.T) {
	if got := Evaluate(nil, nil); got.State != StateUnconfigured {
		t.Fatalf("want Unconfigured, got %v", got.State)
	}
	// 全部阈值停用同样视为未配置。
	if got := Evaluate(nil, []Threshold{{Metric: "p95", Operator: "le", Value: 1, Enabled: false}}); got.State != StateUnconfigured {
		t.Fatalf("全停用阈值应为 Unconfigured, got %v", got.State)
	}
	if got := Evaluate(map[Metric]float64{"p95": 100},
		[]Threshold{{Metric: "error_rate", Operator: "le", Value: 1, Enabled: true}}); got.State != StateMissing {
		t.Fatalf("missing metric should be Missing, got %v", got.State)
	}
	if got := Evaluate(nil, []Threshold{{Metric: "p95", Operator: "le", Value: 100, Enabled: true}}); got.State != StateMissing {
		t.Fatalf("nil metrics should be Missing, got %v", got.State)
	}
	if got := Evaluate(map[Metric]float64{"p95": 100},
		[]Threshold{{Metric: "p95", Operator: "le", Value: 100, Enabled: true}}); got.State != StatePass {
		t.Fatalf("边界值 le 100 <= 100 应通过, got %v", got.State)
	}
}

// TestEvaluateOperatorAndUnit 方向比较与百分比单位归一化。
func TestEvaluateOperatorAndUnit(t *testing.T) {
	// ge：越大越好。
	if e := Evaluate(map[Metric]float64{"throughput": 120},
		[]Threshold{{Metric: "throughput", Operator: "ge", Value: 100, Enabled: true}}); !e.Pass {
		t.Fatal("throughput 120 >= 100 应通过")
	}
	if e := Evaluate(map[Metric]float64{"throughput": 80},
		[]Threshold{{Metric: "throughput", Operator: "ge", Value: 100, Enabled: true}}); e.Pass {
		t.Fatal("throughput 80 >= 100 应不通过")
	}

	// 非法 metric/operator 组合（INV-2）→ 该条判失败，整体不通过。
	if e := Evaluate(map[Metric]float64{"p95": 10},
		[]Threshold{{Metric: "throughput", Operator: "le", Value: 1, Enabled: true}}); e.Pass {
		t.Fatal("非法 metric↔operator 组合应判失败")
	}

	// error_rate 单位 % 与比值（0~1）两种写法都归一到百分比口径。
	if e := Evaluate(map[Metric]float64{"error_rate": 0.5},
		[]Threshold{{Metric: "error_rate", Operator: "le", Value: 0.01, Unit: "ratio", Enabled: true}}); !e.Pass {
		t.Fatal("ratio 口径阈值 0.01（=1%）应允许 0.5% 通过")
	}
}
