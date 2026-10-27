-- 审计日志只读触发器（对齐 security-design-report.md DES-T-006）
-- 目标：user_log 为 append-only，禁止应用层 UPDATE/DELETE（防篡改）。
-- HMAC 链式签名在应用层计算，本触发器作为数据库侧兜底防线。

CREATE OR REPLACE FUNCTION prevent_user_log_mutation()
RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'user_log is append-only; UPDATE/DELETE is forbidden';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER user_log_no_update
BEFORE UPDATE ON user_log
FOR EACH ROW
EXECUTE FUNCTION prevent_user_log_mutation();

CREATE TRIGGER user_log_no_delete
BEFORE DELETE ON user_log
FOR EACH ROW
EXECUTE FUNCTION prevent_user_log_mutation();
