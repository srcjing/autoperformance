package evaluator

import (
	"fmt"
	"strings"

	"auto_performance/internal/domain"
)

// State 评估结论状态。
type State string

// 评估结论（PRD §9.2 / INV-2）。
const (
	// StatePass 全部启用阈值达标。
	StatePass State = "pass"
	// StateFail 存在未达标（或非法）阈值。
	StateFail State = "fail"
	// StateUnconfigured 未配置任何启用阈值。
	StateUnconfigured State = "unconfigured"
	// StateMissing 阈值存在但缺少对应指标数据。
	StateMissing State = "missing"
)

// Threshold 待判定阈值（通常来自任务创建时的阈值快照）。
type Threshold struct {
	InterfaceID   int64
	InterfaceName string
	Metric        Metric
	Operator      string
	Value         float64
	// Unit 单位；error_rate/success_rate 支持 "ratio"（0~1）写法，内部统一为百分比口径。
	Unit    string
	Enabled bool
}

// Item 单条阈值判定结果（逐条输出便于报告展示与追溯）。
type Item struct {
	InterfaceID   int64   `json:"interface_id"`
	InterfaceName string  `json:"interface_name,omitempty"`
	Metric        Metric  `json:"metric"`
	Operator      string  `json:"operator"`
	Threshold     float64 `json:"threshold"`
	Actual        float64 `json:"actual"`
	HasActual     bool    `json:"has_actual"`
	Pass          bool    `json:"pass"`
	Reason        string  `json:"reason,omitempty"`
}

// Evaluation 评估结论（整体 + 逐条）。
type Evaluation struct {
	State   State    `json:"state"`
	Pass    bool     `json:"pass"`
	Items   []Item   `json:"items"`
	Missing []Metric `json:"missing,omitempty"`
}

// Evaluate 逐条判定阈值（INV-2）：
//   - 无启用阈值 → StateUnconfigured；
//   - 阈值存在但指标缺失 → StateMissing；
//   - 全部启用阈值达标 → StatePass；任一不达标/非法组合 → StateFail。
func Evaluate(metrics map[Metric]float64, thresholds []Threshold) Evaluation {
	eval := Evaluation{Items: make([]Item, 0, len(thresholds))}

	enabled := make([]Threshold, 0, len(thresholds))
	for _, t := range thresholds {
		if t.Enabled {
			enabled = append(enabled, t)
		}
	}
	if len(enabled) == 0 {
		eval.State = StateUnconfigured
		return eval
	}

	missingSeen := make(map[Metric]bool, 2)
	anyFail, anyMissing := false, false

	for _, t := range enabled {
		item := Item{
			InterfaceID:   t.InterfaceID,
			InterfaceName: t.InterfaceName,
			Metric:        t.Metric,
			Operator:      strings.ToLower(strings.TrimSpace(t.Operator)),
			Threshold:     normalizeThreshold(t),
		}

		// 指标↔方向非法组合一律判失败（Phase 3 已拦截，此处为脏数据兜底）。
		if err := domain.ValidateThreshold(string(t.Metric), item.Operator); err != nil {
			item.Pass = false
			item.Reason = "阈值方向与指标类型不匹配"
			anyFail = true
			eval.Items = append(eval.Items, item)
			continue
		}

		actual, ok := metrics[t.Metric]
		if !ok {
			item.Reason = fmt.Sprintf("缺少指标 %s 数据", t.Metric)
			anyMissing = true
			if !missingSeen[t.Metric] {
				missingSeen[t.Metric] = true
				eval.Missing = append(eval.Missing, t.Metric)
			}
			eval.Items = append(eval.Items, item)
			continue
		}

		item.Actual = actual
		item.HasActual = true
		switch item.Operator {
		case domain.OperatorLE:
			item.Pass = actual <= item.Threshold
		case domain.OperatorGE:
			item.Pass = actual >= item.Threshold
		default:
			item.Pass = false
			item.Reason = "不支持的阈值方向"
		}
		if !item.Pass {
			anyFail = true
			if item.Reason == "" {
				item.Reason = fmt.Sprintf("%s=%g 未满足 %s %g", t.Metric, actual, item.Operator, item.Threshold)
			}
		}
		eval.Items = append(eval.Items, item)
	}

	switch {
	case anyFail:
		eval.State = StateFail
	case anyMissing:
		eval.State = StateMissing
	default:
		eval.State = StatePass
		eval.Pass = true
	}
	return eval
}

// normalizeThreshold 统一阈值口径：error_rate/success_rate 的 ratio（0~1）写法转为百分比。
func normalizeThreshold(t Threshold) float64 {
	switch t.Metric {
	case MetricErrorRate, MetricSuccessRate:
		switch strings.ToLower(strings.TrimSpace(t.Unit)) {
		case "ratio", "fraction", "0-1":
			return t.Value * 100
		}
	}
	return t.Value
}
