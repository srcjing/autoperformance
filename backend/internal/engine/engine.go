// Package engine 引擎适配层：抽象压测引擎接口。
//
// 对齐 architecture.md §2.2：Engine 接口（Generate/Run/Parse/Stream），
// 首期实现 k6（子进程 CLI），骨架阶段仅声明接口。
package engine

import "context"

// Engine 压测引擎接口（Generate/Run/Parse/Stream）。
// 实现：k6 子进程；后续如需多机加压，可在此接口上扩展新实现。
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
