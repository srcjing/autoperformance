// Package word 报告 Word 生成（gooxml 模板填充式排版）。
//
// 与 HTML 报告共用 reportgen.ReportData 渲染模型，保证两种格式的统计口径与结论一致；
// 不达标项以「红色加粗」突出显示（PRD §3.4.5：不达标项在报告中高亮标注）。
package word

import (
	"fmt"
	"io"
	"strings"

	"github.com/carmel/gooxml/color"
	"github.com/carmel/gooxml/document"
	"github.com/carmel/gooxml/measurement"

	"auto_performance/internal/reportgen"
)

// 字号（磅）。
const (
	titleSize   = 18 * measurement.Point
	headingSize = 13 * measurement.Point
	smallSize   = 9 * measurement.Point
)

// cell 表格单元格（danger=true 时红色加粗，用于不达标高亮）。
type cell struct {
	text   string
	danger bool
}

// Generate 渲染 Word 报告（.docx）到 w。
func Generate(w io.Writer, data *reportgen.ReportData) error {
	if w == nil {
		return fmt.Errorf("报告输出目标为空")
	}
	if data == nil {
		return fmt.Errorf("报告数据为空")
	}

	doc := document.New()

	// 标题与元信息
	title := doc.AddParagraph()
	titleRun := title.AddRun()
	titleRun.Properties().SetBold(true)
	titleRun.Properties().SetSize(titleSize)
	titleRun.AddText(data.Title)

	subtitle := doc.AddParagraph()
	subtitleRun := subtitle.AddRun()
	subtitleRun.Properties().SetColor(color.Gray)
	subtitleRun.Properties().SetSize(smallSize)
	subtitleRun.AddText(fmt.Sprintf("%s · 报告编号 #%d · 生成时间 %s",
		data.SourceLabel, data.ReportID, data.GeneratedAtText))

	meta := doc.AddParagraph()
	metaRun := meta.AddRun()
	metaRun.Properties().SetSize(smallSize)
	taskName := data.TaskName
	if taskName == "" {
		taskName = "-"
	}
	metaRun.AddText(fmt.Sprintf("任务：%s    环境：%s    执行用时：%s", taskName, data.Environment, data.DurationText))

	// 一、达标概览
	addHeading(doc, "一、达标概览")
	if data.Summary.TotalInterfaces == 0 {
		addParagraph(doc, "未配置启用阈值，本报告不计入达标率统计。", false)
	}
	addTable(doc, []string{"统计项", "数值"}, [][]cell{
		{{text: "整体结论"}, {text: data.StateLabel, danger: strings.Contains(data.StateLabel, "不达标")}},
		{{text: "接口达标率"}, {text: data.PassRateText + "%"}},
		{{text: "达标 / 不达标接口"}, {text: fmt.Sprintf("%d / %d（共 %d 个）",
			data.Summary.PassedInterfaces, data.Summary.FailedInterfaces, data.Summary.TotalInterfaces)}},
		{{text: "阈值项 达标 / 不达标"}, {text: fmt.Sprintf("%d / %d（共 %d 条启用阈值）",
			data.Summary.PassedThresholds, data.Summary.FailedThresholds, data.Summary.TotalThresholds)}},
	})

	// 二、关键指标
	addHeading(doc, "二、关键指标")
	if data.HasMetrics {
		rows := make([][]cell, 0, len(data.Metrics))
		for _, metric := range data.Metrics {
			rows = append(rows, []cell{{text: metric.Label}, {text: metric.Value}, {text: metric.Unit}})
		}
		addTable(doc, []string{"指标", "实测值", "单位"}, rows)
	} else {
		addParagraph(doc, "本次执行未采集到指标数据。", false)
	}

	// 三、接口达标明细
	addHeading(doc, "三、接口达标明细")
	if data.HasInterfaces {
		rows := make([][]cell, 0, len(data.Interfaces))
		for _, iface := range data.Interfaces {
			verdict := "达标"
			if !iface.Pass {
				verdict = "不达标"
			}
			reason := iface.Reason
			if reason == "" {
				reason = "-"
			}
			rows = append(rows, []cell{
				{text: iface.Name},
				{text: iface.Method},
				{text: iface.URL},
				{text: fmt.Sprintf("%d", iface.Total)},
				{text: verdict, danger: !iface.Pass},
				{text: reason, danger: !iface.Pass},
			})
		}
		addTable(doc, []string{"接口", "方法", "地址（已脱敏）", "阈值项", "结论", "说明"}, rows)
	} else {
		addParagraph(doc, "未配置启用阈值，无接口达标明细。", false)
	}

	// 四、阈值快照判定
	addHeading(doc, "四、阈值快照判定")
	if data.HasThresholds {
		rows := make([][]cell, 0, len(data.Summary.Items))
		for _, item := range data.Summary.Items {
			actual := "-"
			if item.HasActual {
				actual = reportgen.FormatNumber(item.Actual)
			}
			verdict := "达标"
			if !item.Pass {
				verdict = "不达标"
			}
			reason := item.Reason
			if reason == "" {
				reason = "-"
			}
			rows = append(rows, []cell{
				{text: item.InterfaceName},
				{text: item.Metric},
				{text: item.Operator},
				{text: reportgen.FormatNumber(item.Threshold)},
				{text: actual},
				{text: verdict, danger: !item.Pass},
				{text: reason, danger: !item.Pass},
			})
		}
		addTable(doc, []string{"接口", "指标", "条件", "阈值", "实测", "结论", "说明"}, rows)
	} else {
		addParagraph(doc, "无阈值判定记录。", false)
	}

	// 说明
	note := doc.AddParagraph()
	noteRun := note.AddRun()
	noteRun.Properties().SetSize(smallSize)
	noteRun.Properties().SetColor(color.Gray)
	noteRun.AddText(data.Footnote)

	if err := doc.Save(w); err != nil {
		return fmt.Errorf("保存 Word 报告失败: %w", err)
	}
	return nil
}

// addHeading 小节标题（加粗、略大字号）。
func addHeading(doc *document.Document, text string) {
	p := doc.AddParagraph()
	run := p.AddRun()
	run.Properties().SetBold(true)
	run.Properties().SetSize(headingSize)
	run.AddText(text)
}

// addParagraph 普通段落（danger 时红色，用于失败提示）。
func addParagraph(doc *document.Document, text string, danger bool) {
	p := doc.AddParagraph()
	run := p.AddRun()
	if danger {
		run.Properties().SetColor(color.Red)
	}
	run.AddText(text)
}

// addTable 写入表格（首行为表头；danger 单元格红色加粗高亮）。
func addTable(doc *document.Document, headers []string, rows [][]cell) {
	table := doc.AddTable()

	headerRow := table.AddRow()
	for _, header := range headers {
		run := headerRow.AddCell().AddParagraph().AddRun()
		run.Properties().SetBold(true)
		run.AddText(header)
	}

	for _, row := range rows {
		tableRow := table.AddRow()
		for _, item := range row {
			run := tableRow.AddCell().AddParagraph().AddRun()
			if item.danger {
				run.Properties().SetBold(true)
				run.Properties().SetColor(color.Red)
			}
			run.AddText(item.text)
		}
	}
}
