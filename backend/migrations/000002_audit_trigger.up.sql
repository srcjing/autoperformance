-- 审计日志只读触发器（对齐 security-design-report.md DES-T-006）
-- 目标：user_log 为 append-only，禁止应用层 UPDATE/DELETE（防篡改）。
-- 说明：user_log 为分区表（PARTITION BY RANGE），PostgreSQL 12 不允许在父表上
--       建立行级触发器，故在每个分区子表上分别建立；新增分区需以同样方式补建
--       （见迁移 000005 或运维分区脚本）。

CREATE OR REPLACE FUNCTION prevent_user_log_mutation()
RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'user_log is append-only; UPDATE/DELETE is forbidden';
END;
$$ LANGUAGE plpgsql;

DO $$
DECLARE
    part text;
BEGIN
    FOR part IN
        SELECT c.relname
        FROM pg_inherits i
        JOIN pg_class c ON c.oid = i.inhrelid
        WHERE i.inhparent = 'user_log'::regclass
    LOOP
        EXECUTE format('DROP TRIGGER IF EXISTS user_log_no_update ON %I', part);
        EXECUTE format('DROP TRIGGER IF EXISTS user_log_no_delete ON %I', part);
        EXECUTE format('CREATE TRIGGER user_log_no_update BEFORE UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION prevent_user_log_mutation()', part);
        EXECUTE format('CREATE TRIGGER user_log_no_delete BEFORE DELETE ON %I FOR EACH ROW EXECUTE FUNCTION prevent_user_log_mutation()', part);
    END LOOP;
END;
$$;
