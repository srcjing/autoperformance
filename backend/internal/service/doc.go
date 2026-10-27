// Package service 业务服务层（用例编排）。
//
// 对齐 architecture.md §2.2：承接 api 层调用，编排领域/仓储/执行层完成用例，
// 各业务模块按子包组织（auth/environment/product/testing/report/log/setting），
// 业务错误码（2xxxx）由各子包 errors.go 统一登记。
package service
