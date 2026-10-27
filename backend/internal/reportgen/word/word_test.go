package word

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"

	"auto_performance/internal/domain"
	"auto_performance/internal/evaluator"
	"auto_performance/internal/reportgen"
)

// sampleData 报告渲染模型样例（一个达标接口 + 一个不达标接口）。
func sampleData(t *testing.T) *reportgen.ReportData {
	t.Helper()
	return reportgen.BuildData(reportgen.BuildInput{
		ReportID:        7,
		TaskID:          3,
		Title:           "订单服务 性能测试报告",
		TaskName:        "订单服务压测",
		Environment:     "预发环境",
		Source:          domain.ReportSourceInternal,
		DurationSeconds: 30,
		Metrics: map[evaluator.Metric]float64{
			evaluator.MetricP95:       210,
			evaluator.MetricErrorRate: 2.5,
		},
		Evaluation: evaluator.Evaluation{
			State: evaluator.StateFail,
			Items: []evaluator.Item{
				{InterfaceID: 11, InterfaceName: "查询订单", Metric: evaluator.MetricP95, Operator: domain.OperatorLE,
					Threshold: 500, Actual: 210, HasActual: true, Pass: true},
				{InterfaceID: 12, InterfaceName: "创建订单", Metric: evaluator.MetricErrorRate, Operator: domain.OperatorLE,
					Threshold: 1, Actual: 2.5, HasActual: true, Pass: false, Reason: "错误率超阈值"},
			},
		},
		Interfaces: []domain.InterfaceSnapshot{
			{InterfaceID: 11, Name: "查询订单", Method: "GET", URL: "https://api.example.test/orders?token=x"},
			{InterfaceID: 12, Name: "创建订单", Method: "POST", URL: "https://api.example.test/orders"},
		},
	})
}

// TestGenerateDocx 校验 Word 产物为合法 docx（zip + word/document.xml）且包含达标/不达标结论。
func TestGenerateDocx(t *testing.T) {
	var buf bytes.Buffer
	if err := Generate(&buf, sampleData(t)); err != nil {
		t.Fatalf("生成 docx 失败: %v", err)
	}
	raw := buf.Bytes()
	if len(raw) < 1000 {
		t.Fatalf("docx 体积异常: %d 字节", len(raw))
	}
	if !bytes.HasPrefix(raw, []byte("PK\x03\x04")) {
		t.Fatalf("docx 不是 zip 容器（magic 校验失败）")
	}

	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("解析 docx zip 失败: %v", err)
	}
	var documentXML string
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("打开 word/document.xml 失败: %v", err)
		}
		content, err := io.ReadAll(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatalf("读取 word/document.xml 失败: %v", err)
		}
		documentXML = string(content)
	}
	if documentXML == "" {
		t.Fatalf("docx 缺少 word/document.xml")
	}

	for _, want := range []string{"报告编号", "达标", "不达标", "查询订单", "创建订单", "错误率超阈值", "接口达标率"} {
		if !strings.Contains(documentXML, want) {
			t.Errorf("Word 报告缺少内容 %q", want)
		}
	}
	// 接口地址脱敏：不得写入查询串中的凭据。
	if strings.Contains(documentXML, "token=x") {
		t.Errorf("Word 报告泄露接口地址查询串")
	}
}

// TestGenerateDocxUnconfigured 未配置阈值时仍能产出可用文档（统计缺省值不 panic）。
func TestGenerateDocxUnconfigured(t *testing.T) {
	data := reportgen.BuildData(reportgen.BuildInput{
		ReportID:   8,
		Title:      "空报告",
		Evaluation: evaluator.Evaluation{State: evaluator.StateUnconfigured},
	})
	var buf bytes.Buffer
	if err := Generate(&buf, data); err != nil {
		t.Fatalf("生成 docx 失败: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatalf("docx 内容为空")
	}
}
