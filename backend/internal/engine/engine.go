// Package engine 引擎适配层：抽象压测引擎能力。
//
// 首期实现为 k6 子进程（engine/k6）：BuildScript 生成脚本、Runner 逐参启动子进程并
// 流式回调输出、Parse 解析 `--out json` 样本。编排器（internal/orchestrator）当前直接
// 依赖 k6 的强类型 API（ScriptSpec/Run/ParseFile），以换取参数白名单与路径沙箱的类型
// 安全；Engine 接口保留为多引擎（如多机加压/其他压测器）扩展点，新增引擎时实现该接口
// 并在编排器中注入，即可复用任务状态机、评估、清理与报告链路。
package engine

import "context"

// Engine 压测引擎扩展接口（Generate/Run/Parse/Stream）。
type Engine interface {
	// Generate 根据任务上下文生成引擎可执行脚本。
	Generate(ctx context.Context, spec interface{}) (string, error)
	// Run 执行压测，返回引擎原始结果。
	Run(ctx context.Context, script string) ([]byte, error)
	// Parse 解析引擎结果。
	Parse(result []byte) (interface{}, error)
	// Stream 流式输出执行进度/日志。
	Stream(ctx context.Context) <-chan string
}
