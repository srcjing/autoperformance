DROP INDEX IF EXISTS idx_test_task_created_by;
DROP INDEX IF EXISTS idx_test_task_env_status;

ALTER TABLE test_task DROP COLUMN IF EXISTS interface_ids;
ALTER TABLE test_task DROP COLUMN IF EXISTS product_ids;
ALTER TABLE test_task DROP COLUMN IF EXISTS created_by;
