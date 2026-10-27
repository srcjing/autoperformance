-- 删除审计日志只读触发器（000002_audit_trigger.up.sql 的回滚）
DROP TRIGGER IF EXISTS user_log_no_update ON user_log;
DROP TRIGGER IF EXISTS user_log_no_delete ON user_log;
DROP FUNCTION IF EXISTS prevent_user_log_mutation();
