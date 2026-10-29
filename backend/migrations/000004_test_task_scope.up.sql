-- 000004: 测试任务补充字段（PRD §4.8：发起人/产品范围/接口范围）
-- created_by: 任务发起人（管理员 ID），用于审计与「我的任务」筛选；
-- product_ids: 关联产品范围（JSONB 数组）；
-- interface_ids: 原始接口 ID 范围（快照为 interface_range_snapshot，原始 ID 便于执行期取认证头密文）。
ALTER TABLE test_task ADD COLUMN IF NOT EXISTS created_by BIGINT;
ALTER TABLE test_task ADD COLUMN IF NOT EXISTS product_ids JSONB;
ALTER TABLE test_task ADD COLUMN IF NOT EXISTS interface_ids JSONB;

CREATE INDEX IF NOT EXISTS idx_test_task_env_status ON test_task (env_id, status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_test_task_created_by ON test_task (created_by) WHERE deleted_at IS NULL;
