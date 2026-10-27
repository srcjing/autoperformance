// Package reportgen 报告生成器（HTML/PDF/Word）。
//
// 对齐 architecture.md §1.4 与 PRD §3.4：
//   - HTML：html/template 渲染，CSS/JS 全内联，可离线读取；
//   - PDF：headless Chromium 打印 HTML（internal/reportgen/pdf）；
//   - Word：gooxml 生成 docx（internal/reportgen/word）。
//
// 本文件定义三种格式共用的渲染模型与聚合逻辑（达标统计、接口级结论、指标口径），
// 保证 HTML/Word 展现完全一致；报告内容不含认证头密文/明文，接口地址统一脱敏
// （DES-I-001，INV-7）。
package reportgen

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"auto_performance/internal/domain"
	"auto_performance/internal/evaluator"
)

// 报告结论标签（与 evaluator.State 对应）。
const (
	StatePass         = string(evaluator.StatePass)
	StateFail         = string(evaluator.StateFail)
	StateMissing      = string(evaluator.StateMissing)
	StateUnconfigured = string(evaluator.StateUnconfigured)
)

// MetricRow 关键指标展示行。
type MetricRow struct {
	Key   string
	Label string
	Value string
	Unit  string
}

// InterfaceRow 接口级达标结论（逐接口聚合其全部启用阈值）。
type InterfaceRow struct {
	InterfaceID int64
	Name        string
	Method      string
	URL         string
	Pass        bool
	Total       int
	Failed      int
	Reason      string
}

// BuildInput 报告渲染模型输入（由任务快照 + 评估结论 + 指标组装）。
type BuildInput struct {
	ReportID        int64
	TaskID          int64
	Title           string
	TaskName        string
	Environment     string
	Source          string
	GeneratedAt     time.Time
	DurationSeconds float64
	Metrics         map[evaluator.Metric]float64
	Evaluation      evaluator.Evaluation
	Interfaces      []domain.InterfaceSnapshot
}

// ReportData 报告渲染数据（HTML 模板与 Word 生成共用）。
type ReportData struct {
	ReportID        int64
	TaskID          int64
	Title           string
	TaskName        string
	Environment     string
	Source          string
	SourceLabel     string
	GeneratedAt     time.Time
	GeneratedAtText string
	DurationText    string
	Summary         domain.ReportSummary
	StateLabel      string
	StateClass      string
	StateBadgeClass string
	PassRateText    string
	Metrics         []MetricRow
	Interfaces      []InterfaceRow
	HasMetrics      bool
	HasInterfaces   bool
	HasThresholds   bool
	Footnote        string
}

// metricMeta 指标展示顺序、中文名与单位（PRD §9.1）。
type metricMeta struct {
	key   evaluator.Metric
	label string
	unit  string
}

var metricMetas = []metricMeta{
	{evaluator.MetricAvgRT, "平均响应时间", "ms"},
	{evaluator.MetricP95, "P95 响应时间", "ms"},
	{evaluator.MetricP99, "P99 响应时间", "ms"},
	{evaluator.MetricMaxRT, "最大响应时间", "ms"},
	{evaluator.MetricThroughput, "吞吐量", "req/s"},
	{evaluator.MetricErrorRate, "错误率", "%"},
	{evaluator.MetricSuccessRate, "成功率", "%"},
	{evaluator.MetricConcurrency, "并发数", "VU"},
}

// footnote 报告固定说明（脱敏口径可追溯）。
const footnote = "说明：报告中接口地址已脱敏（去除查询串与用户信息），接口认证头不进入报告内容（INV-7 / DES-I-001）。" +
	"达标口径：接口的全部启用阈值均满足才算该接口达标；接口达标率 = 达标接口数 / 参与评估接口总数（PRD §9.2）。"

// BuildData 组装报告渲染模型（统计口径与评估器一致，避免多处重复实现）。
func BuildData(in BuildInput) *ReportData {
	summary := buildSummary(in)

	data := &ReportData{
		ReportID:        in.ReportID,
		TaskID:          in.TaskID,
		Title:           in.Title,
		TaskName:        in.TaskName,
		Environment:     in.Environment,
		Source:          in.Source,
		SourceLabel:     sourceLabel(in.Source),
		GeneratedAt:     in.GeneratedAt,
		GeneratedAtText: in.GeneratedAt.Local().Format("2006-01-02 15:04:05"),
		DurationText:    FormatDuration(in.DurationSeconds),
		Summary:         summary,
		StateLabel:      stateLabel(summary.State),
		StateClass:      stateClass(summary.State),
		StateBadgeClass: stateBadgeClass(summary.State),
		PassRateText:    FormatNumber(summary.PassRate),
		Footnote:        footnote,
	}
	if data.Title == "" {
		data.Title = "性能测试报告"
	}
	if data.Environment == "" {
		data.Environment = "-"
	}

	data.Metrics = buildMetrics(in.Metrics)
	data.HasMetrics = len(data.Metrics) > 0
	data.Interfaces = buildInterfaces(in.Evaluation.Items, in.Interfaces)
	data.HasInterfaces = len(data.Interfaces) > 0
	data.HasThresholds = len(summary.Items) > 0
	return data
}

// buildSummary 汇总达标统计：接口级「全部启用阈值达标才算达标」（INV-2）。
func buildSummary(in BuildInput) domain.ReportSummary {
	summary := domain.ReportSummary{
		State:           string(in.Evaluation.State),
		Pass:            in.Evaluation.Pass,
		DurationSeconds: in.DurationSeconds,
	}
	if summary.State == "" {
		summary.State = StateUnconfigured
	}

	// 接口聚合（保持评估顺序，接口 ID → 是否全部达标）。
	order := make([]int64, 0, len(in.Evaluation.Items))
	ifacePass := make(map[int64]bool, len(in.Evaluation.Items))
	summary.Items = make([]domain.ReportSummaryItem, 0, len(in.Evaluation.Items))
	for _, item := range in.Evaluation.Items {
		if _, seen := ifacePass[item.InterfaceID]; !seen {
			order = append(order, item.InterfaceID)
			ifacePass[item.InterfaceID] = true
		}
		if !item.Pass {
			ifacePass[item.InterfaceID] = false
		}
		summary.Items = append(summary.Items, domain.ReportSummaryItem{
			InterfaceID:   item.InterfaceID,
			InterfaceName: item.InterfaceName,
			Metric:        string(item.Metric),
			Operator:      operatorSymbol(item.Operator),
			Threshold:     item.Threshold,
			Actual:        item.Actual,
			HasActual:     item.HasActual,
			Pass:          item.Pass,
			Reason:        item.Reason,
		})
		summary.TotalThresholds++
		if item.Pass {
			summary.PassedThresholds++
		} else {
			summary.FailedThresholds++
		}
	}
	for _, id := range order {
		summary.TotalInterfaces++
		if ifacePass[id] {
			summary.PassedInterfaces++
		} else {
			summary.FailedInterfaces++
		}
	}
	if summary.TotalInterfaces > 0 {
		summary.PassRate = round2(float64(summary.PassedInterfaces) / float64(summary.TotalInterfaces) * 100)
	}

	for _, metric := range in.Evaluation.Missing {
		summary.MissingMetrics = append(summary.MissingMetrics, string(metric))
	}
	if len(in.Metrics) > 0 {
		summary.Metrics = make(map[string]float64, len(in.Metrics))
		for metric, value := range in.Metrics {
			summary.Metrics[string(metric)] = value
		}
	}
	return summary
}

// buildMetrics 按固定口径顺序展示实测指标。
func buildMetrics(metrics map[evaluator.Metric]float64) []MetricRow {
	if len(metrics) == 0 {
		return nil
	}
	rows := make([]MetricRow, 0, len(metricMetas))
	for _, meta := range metricMetas {
		value, ok := metrics[meta.key]
		if !ok {
			continue
		}
		rows = append(rows, MetricRow{
			Key:   string(meta.key),
			Label: meta.label,
			Value: FormatNumber(value),
			Unit:  meta.unit,
		})
	}
	return rows
}

// buildInterfaces 逐接口聚合阈值结论，并补全接口名称/方法/脱敏地址。
func buildInterfaces(items []evaluator.Item, snapshots []domain.InterfaceSnapshot) []InterfaceRow {
	if len(items) == 0 {
		return nil
	}
	snapshotOf := make(map[int64]domain.InterfaceSnapshot, len(snapshots))
	for _, snap := range snapshots {
		snapshotOf[snap.InterfaceID] = snap
	}

	order := make([]int64, 0, len(items))
	rows := make(map[int64]*InterfaceRow, len(items))
	reasons := make(map[int64][]string, len(items))

	for _, item := range items {
		row, ok := rows[item.InterfaceID]
		if !ok {
			snap := snapshotOf[item.InterfaceID]
			name := strings.TrimSpace(snap.Name)
			if name == "" {
				name = strings.TrimSpace(item.InterfaceName)
			}
			if name == "" {
				name = fmt.Sprintf("接口 #%d", item.InterfaceID)
			}
			row = &InterfaceRow{
				InterfaceID: item.InterfaceID,
				Name:        name,
				Method:      snap.Method,
				URL:         MaskURL(snap.URL),
				Pass:        true,
			}
			rows[item.InterfaceID] = row
			order = append(order, item.InterfaceID)
		}
		row.Total++
		if !item.Pass {
			row.Pass = false
			row.Failed++
			if len(reasons[item.InterfaceID]) < 2 && item.Reason != "" {
				reasons[item.InterfaceID] = append(reasons[item.InterfaceID], item.Reason)
			}
		}
	}

	out := make([]InterfaceRow, 0, len(order))
	for _, id := range order {
		row := rows[id]
		row.Reason = strings.Join(reasons[id], "；")
		if row.Method == "" {
			row.Method = "-"
		}
		if row.URL == "" {
			row.URL = "-"
		}
		out = append(out, *row)
	}
	return out
}

// MaskURL 接口地址脱敏（DES-I-001）：保留 scheme/host/path，剥离查询串、片段与用户信息。
func MaskURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		// 非法/相对地址不原样输出，避免误带凭据或本地路径。
		return "（地址不可解析，已隐藏）"
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

// FormatDuration 执行用时展示（保留 2 位小数）。
func FormatDuration(seconds float64) string {
	if seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return "-"
	}
	if seconds < 60 {
		return FormatNumber(round2(seconds)) + " 秒"
	}
	total := int(math.Round(seconds))
	return fmt.Sprintf("%d 分 %02d 秒", total/60, total%60)
}

// FormatNumber 数值展示（最多 2 位小数，去掉末尾 0）。
func FormatNumber(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return "-"
	}
	return strconv.FormatFloat(round2(v), 'f', -1, 64)
}

// operatorSymbol 阈值方向符号（le → ≤，ge → ≥）。
func operatorSymbol(operator string) string {
	switch strings.ToLower(strings.TrimSpace(operator)) {
	case domain.OperatorLE:
		return "≤"
	case domain.OperatorGE:
		return "≥"
	default:
		return strings.TrimSpace(operator)
	}
}

// sourceLabel 报告来源展示文案。
func sourceLabel(source string) string {
	switch source {
	case domain.ReportSourceExternal:
		return "外部导入归档"
	default:
		return "平台生成"
	}
}

// stateLabel 评估结论文案（PRD §9.2）。
func stateLabel(state string) string {
	switch state {
	case StatePass:
		return "整体达标"
	case StateFail:
		return "存在不达标项"
	case StateMissing:
		return "数据缺失"
	case StateUnconfigured:
		return "未配置阈值"
	default:
		return "未知状态"
	}
}

// stateClass 结论文字样式类。
func stateClass(state string) string {
	switch state {
	case StatePass:
		return "state-pass"
	case StateFail:
		return "state-fail"
	case StateMissing:
		return "state-warn"
	default:
		return "state-muted"
	}
}

// stateBadgeClass 结论徽标样式类。
func stateBadgeClass(state string) string {
	switch state {
	case StatePass:
		return "badge-pass"
	case StateFail:
		return "badge-fail"
	case StateMissing:
		return "badge-warn"
	default:
		return "badge-muted"
	}
}

// round2 保留两位小数（统计口径稳定，避免浮点尾差）。
func round2(v float64) float64 {
	return math.Round(v*100) / 100
}
