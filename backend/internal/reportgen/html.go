package reportgen

import (
	"fmt"
	"html/template"
	"io"
	"sync"

	"auto_performance/templates"
)

// htmlFuncs 模板辅助函数（数值格式化统一走 FormatNumber，避免模板内浮点尾差）。
var htmlFuncs = template.FuncMap{
	"num": FormatNumber,
}

var (
	htmlOnce sync.Once
	htmlTmpl *template.Template
	htmlErr  error
)

// loadHTMLTemplate 解析内嵌报告模板（进程内只解析一次）。
func loadHTMLTemplate() (*template.Template, error) {
	htmlOnce.Do(func() {
		raw, err := templates.FS.ReadFile(templates.ReportHTML)
		if err != nil {
			htmlErr = fmt.Errorf("读取报告模板失败: %w", err)
			return
		}
		tmpl, err := template.New("report").Funcs(htmlFuncs).Parse(string(raw))
		if err != nil {
			htmlErr = fmt.Errorf("解析报告模板失败: %w", err)
			return
		}
		htmlTmpl = tmpl
	})
	return htmlTmpl, htmlErr
}

// RenderHTML 渲染 HTML 报告（CSS/JS 全内联，无外部资源，可离线读取）。
//
// 模板使用 html/template 自动转义，接口名称/地址/原因等动态内容不会注入脚本。
func RenderHTML(w io.Writer, data *ReportData) error {
	if w == nil {
		return fmt.Errorf("报告输出目标为空")
	}
	if data == nil {
		return fmt.Errorf("报告数据为空")
	}
	tmpl, err := loadHTMLTemplate()
	if err != nil {
		return err
	}
	if err := tmpl.Execute(w, data); err != nil {
		return fmt.Errorf("渲染 HTML 报告失败: %w", err)
	}
	return nil
}
