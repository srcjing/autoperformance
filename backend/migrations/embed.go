// Package migrations 通过 go:embed 内嵌数据库迁移 SQL。
//
// 对齐 architecture.md §6.4 / plan.md §1.1 关键口径 #2：golang-migrate 内嵌 SQL 迁移，
// 由 repository.MigrateUp 通过 iofs source 读取执行；升级脚本调用 `auto_performance migrate up`。
package migrations

import "embed"

// FS 内嵌全部迁移 SQL（000001_init_schema / 000002_audit_trigger /
// 000003_seed_builtin_admin 及其 down）。
//
//go:embed *.sql
var FS embed.FS
