-- 000006 回滚：逆序删除索引与新增表（request_log / log_setting）。
DROP INDEX IF EXISTS idx_test_log_created_at;
DROP INDEX IF EXISTS idx_system_log_created_at;

DROP TABLE IF EXISTS log_setting;

DROP INDEX IF EXISTS idx_request_log_occurred_at;
DROP INDEX IF EXISTS idx_request_log_interface;
DROP INDEX IF EXISTS idx_request_log_task_time;
DROP TABLE IF EXISTS request_log;
