-- 000006: 压测请求详情日志（request_log）+ 日志设置（log_setting 单行）
-- PRD §3.5：请求详情记录压测时对被测接口的每个请求明细（method/url/入参/响应状态/
-- 响应内容/错误码/错误信息/耗时）。数据量大 → 服务层按单任务采样限量 + 日志设置控制
-- 保留策略（最大条数/存储天数）。
-- 说明：user_log 为 append-only 审计表（000002_audit_trigger 禁止 UPDATE/DELETE），
--      日志清理只作用于 request_log / test_log / system_log。

CREATE TABLE IF NOT EXISTS request_log (
    id              BIGSERIAL PRIMARY KEY,
    task_id         BIGINT      NOT NULL,
    interface_id    BIGINT      NOT NULL DEFAULT 0,
    seq             BIGINT      NOT NULL DEFAULT 0,   -- 任务内请求序号（写入顺序）
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    method          TEXT        NOT NULL DEFAULT '',
    url             TEXT        NOT NULL DEFAULT '',
    request_body    TEXT        NOT NULL DEFAULT '',   -- 已脱敏 + 截断（4KB）
    response_status INT         NOT NULL DEFAULT 0,
    response_body   TEXT        NOT NULL DEFAULT '',   -- 已脱敏 + 截断（4KB）
    error_code      TEXT        NOT NULL DEFAULT '',
    error_message   TEXT        NOT NULL DEFAULT '',
    duration_ms     BIGINT      NOT NULL DEFAULT 0
);

-- 按任务/接口定位 + 时间倒序翻页（PRD §3.5：请求详情可按任务/接口筛选）。
CREATE INDEX IF NOT EXISTS idx_request_log_task_time ON request_log (task_id, occurred_at);
CREATE INDEX IF NOT EXISTS idx_request_log_interface ON request_log (interface_id);
-- 过期清理扫描（日志设置 retention_days）。
CREATE INDEX IF NOT EXISTS idx_request_log_occurred_at ON request_log (occurred_at);

-- 日志设置：单行表（id 恒为 1），控制日志服务开关/最大条数/存储天数/启用类型。
CREATE TABLE IF NOT EXISTS log_setting (
    id             SMALLINT    PRIMARY KEY DEFAULT 1,
    enabled        BOOLEAN     NOT NULL DEFAULT TRUE,
    max_count      BIGINT      NOT NULL DEFAULT 100000,
    retention_days INT         NOT NULL DEFAULT 30,
    log_types      JSONB       NOT NULL DEFAULT '["user","system","test","request"]'::jsonb,
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT ck_log_setting_single CHECK (id = 1),
    CONSTRAINT ck_log_setting_max_count CHECK (max_count > 0),
    CONSTRAINT ck_log_setting_retention CHECK (retention_days > 0)
);

-- 初始化单行设置（缺省值对齐 domain.DefaultLogSetting）。
INSERT INTO log_setting (id) VALUES (1) ON CONFLICT (id) DO NOTHING;

-- 系统日志/测试日志按时间清理索引（分区表上为分区本地索引）。
CREATE INDEX IF NOT EXISTS idx_system_log_created_at ON system_log (created_at);
CREATE INDEX IF NOT EXISTS idx_test_log_created_at ON test_log (created_at);
