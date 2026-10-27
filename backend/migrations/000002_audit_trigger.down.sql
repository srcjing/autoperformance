-- 回滚：删除各 user_log 分区上的只读触发器与函数。
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
    END LOOP;
END;
$$;

DROP FUNCTION IF EXISTS prevent_user_log_mutation();
