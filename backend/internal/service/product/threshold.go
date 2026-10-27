package product

import (
	"context"
	"errors"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"auto_performance/internal/domain"
	"auto_performance/internal/errcode"
)

// ===================== 阈值 =====================

// ThresholdInput 阈值输入（PUT 全量替换的单项）。
type ThresholdInput struct {
	Metric   string
	Operator string
	Value    float64
	Unit     string
	Enabled  *bool // 缺省视为启用（PRD §4.6 是否启用）
}

// ThresholdView 阈值视图。
type ThresholdView struct {
	ID          int64
	InterfaceID int64
	Metric      string
	Operator    string
	Value       float64
	Unit        string
	Enabled     bool
	Version     int64
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ListThresholds 接口阈值列表（接口不存在 → 10301）。
func (s *Service) ListThresholds(ctx context.Context, interfaceID int64) ([]ThresholdView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if interfaceID <= 0 {
		return nil, errcode.ErrParamFmt
	}
	if _, err := s.repo.GetInterface(ctx, interfaceID); err != nil {
		return nil, err
	}
	items, err := s.repo.ListThresholds(ctx, interfaceID)
	if err != nil {
		return nil, err
	}
	return toThresholdViews(items), nil
}

// ReplaceThresholds 全量替换接口阈值（INV-2：任一非法组合整体拒绝，不落库）。
func (s *Service) ReplaceThresholds(ctx context.Context, interfaceID int64, inputs []ThresholdInput) ([]ThresholdView, error) {
	if s == nil || s.repo == nil {
		return nil, errcode.ErrDependency
	}
	if interfaceID <= 0 {
		return nil, errcode.ErrParamFmt
	}
	if _, err := s.repo.GetInterface(ctx, interfaceID); err != nil {
		return nil, err
	}

	items, err := validateThresholdInputs(interfaceID, inputs)
	if err != nil {
		return nil, err
	}
	if err := s.repo.ReplaceThresholds(ctx, interfaceID, items); err != nil {
		return nil, err
	}

	saved, err := s.repo.ListThresholds(ctx, interfaceID)
	if err != nil {
		return nil, err
	}
	return toThresholdViews(saved), nil
}

// validateThresholdInputs 逐条校验阈值并归一化（非法组合 → 22003；其余非法 → 10003）。
//
// 同一接口内同一指标只允许一条阈值：评估口径为「全启用阈值达标才算达标」（PRD §9.2），
// 重复指标会造成口径歧义，故拒绝。
func validateThresholdInputs(interfaceID int64, inputs []ThresholdInput) ([]domain.Threshold, error) {
	if len(inputs) > len(domain.Metrics()) {
		return nil, errcode.ErrParamFmt
	}

	seen := make(map[string]bool, len(inputs))
	items := make([]domain.Threshold, 0, len(inputs))
	for _, in := range inputs {
		metric := strings.TrimSpace(in.Metric)
		operator := strings.TrimSpace(in.Operator)

		if err := domain.ValidateThreshold(metric, operator); err != nil {
			if errors.Is(err, domain.ErrThresholdMismatch) {
				return nil, ErrThresholdMismatch // 22003
			}
			return nil, errcode.ErrParamFmt
		}
		if seen[metric] {
			return nil, errcode.ErrParamFmt
		}
		seen[metric] = true

		if math.IsNaN(in.Value) || math.IsInf(in.Value, 0) || in.Value < 0 {
			return nil, errcode.ErrParamFmt
		}
		if (metric == domain.MetricErrorRate || metric == domain.MetricSuccessRate) && in.Value > 100 {
			return nil, errcode.ErrParamFmt // 百分比口径 0~100（PRD §9.1）
		}

		unit := strings.TrimSpace(in.Unit)
		if unit == "" {
			unit = domain.DefaultThresholdUnit(metric)
		}
		if utf8.RuneCountInString(unit) > maxUnitLen {
			return nil, errcode.ErrParamFmt
		}

		enabled := true
		if in.Enabled != nil {
			enabled = *in.Enabled
		}

		items = append(items, domain.Threshold{
			InterfaceID: interfaceID,
			Metric:      metric,
			Operator:    operator,
			Value:       in.Value,
			Unit:        unit,
			Enabled:     enabled,
		})
	}
	return items, nil
}

// toThresholdViews 领域实体 → 视图列表。
func toThresholdViews(items []domain.Threshold) []ThresholdView {
	out := make([]ThresholdView, 0, len(items))
	for i := range items {
		t := &items[i]
		out = append(out, ThresholdView{
			ID:          t.ID,
			InterfaceID: t.InterfaceID,
			Metric:      t.Metric,
			Operator:    t.Operator,
			Value:       t.Value,
			Unit:        t.Unit,
			Enabled:     t.Enabled,
			Version:     t.Version,
			CreatedAt:   t.CreatedAt,
			UpdatedAt:   t.UpdatedAt,
		})
	}
	return out
}
