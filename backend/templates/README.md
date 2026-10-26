# templates

报告 HTML 模板目录（go:embed 内嵌进二进制）。

对齐 architecture.md §1.4/§6.3：HTML 报告由 Go html/template 渲染，CSS/JS 全内联（无外部 CDN）。
骨架阶段暂无模板文件；报告生成（reportgen/html）落地时在此填充 .tmpl。
