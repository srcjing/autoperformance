// Package k6 k6 压测引擎适配层。
//
// 对齐 architecture.md §1.1 与 DES-T-001/DES-T-002：
//   - script.go：text/template 生成脚本，参数白名单 + 变量转义，无用户 JS 拼接；
//   - runner.go：exec.CommandContext 逐参传递（无 shell），低权限运行，流式输出回调；
//   - parser.go：解析 `k6 run --out json` 逐行样本，供评估器归一化。
package k6
