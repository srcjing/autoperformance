-- 000005 回滚：逆序删除任务目标 host 与接口地址三段（含兜底约束）。
ALTER TABLE test_task DROP COLUMN IF EXISTS target_host;

ALTER TABLE interface DROP CONSTRAINT IF EXISTS ck_interface_path;
ALTER TABLE interface DROP CONSTRAINT IF EXISTS ck_interface_port;
ALTER TABLE interface DROP CONSTRAINT IF EXISTS ck_interface_scheme;

ALTER TABLE interface DROP COLUMN IF EXISTS path;
ALTER TABLE interface DROP COLUMN IF EXISTS port;
ALTER TABLE interface DROP COLUMN IF EXISTS scheme;
