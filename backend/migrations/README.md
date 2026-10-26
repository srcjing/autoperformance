# migrations

数据库迁移目录（golang-migrate SQL 迁移）。

- 命名约定：`{seq}_{name}.up.sql` / `{seq}_{name}.down.sql`，seq 递增不跳号。
- 对齐 architecture.md §5.1 数据模型：environment / tag / product / interface / threshold /
  test_template / test_task / test_report / report_artifact / user_log / system_log / test_log /
  admin / role / role_permission / admin_role / certificate / ssh_host。

骨架阶段暂无迁移文件；升级流程（upgrade.sh）会在替换二进制后执行 `migrate up`（见 deploy/scripts/upgrade.sh）。
