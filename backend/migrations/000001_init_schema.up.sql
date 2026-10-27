-- auto_performance 初始 schema（对齐 architecture.md §5.1 数据模型）
-- 约定：
--   * 所有业务表含 version（乐观锁，默认 1）、created_at/updated_at/deleted_at（逻辑删除）。
--   * ID 一律 BIGSERIAL 自增；JSON 序列化为字符串（规避 JS 精度丢失）。
--   * 密钥类字段（interface.headers_enc、certificate.cert_path/key_path、ssh_host.key_path）存密文。
--   * 日志表（user_log/system_log/test_log）按 created_at RANGE 分区（季度）。
--   * 唯一约束采用「部分唯一索引（WHERE deleted_at IS NULL）」以支持逻辑删除后可重建同名。

-- =====================================================================
-- 1. 环境分类 / 标签 / 证书 / SSH-Host（无外键依赖，先行创建）
-- =====================================================================
CREATE TABLE environment_category (
    id          BIGSERIAL PRIMARY KEY,
    name        VARCHAR(64)  NOT NULL,
    description TEXT,
    version     BIGINT       NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);
CREATE UNIQUE INDEX uq_environment_category_name ON environment_category (name) WHERE deleted_at IS NULL;

CREATE TABLE tag (
    id          BIGSERIAL PRIMARY KEY,
    name        VARCHAR(64)  NOT NULL,
    color       VARCHAR(16),
    description TEXT,
    version     BIGINT       NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);
CREATE UNIQUE INDEX uq_tag_name ON tag (name) WHERE deleted_at IS NULL;

CREATE TABLE certificate (
    id                 BIGSERIAL PRIMARY KEY,
    name               VARCHAR(128) NOT NULL,
    issuer             VARCHAR(128),
    valid_from         DATE,
    valid_to           DATE,
    cert_path          TEXT,          -- 证书路径（私钥加密存储，见 crypto 信封加密）
    key_path           TEXT,          -- 私钥路径（密文）
    is_builtin         BOOLEAN      NOT NULL DEFAULT FALSE,
    status             VARCHAR(16)  NOT NULL DEFAULT 'valid', -- valid/expiring/expired
    associated_service VARCHAR(128),
    version            BIGINT       NOT NULL DEFAULT 1,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at         TIMESTAMPTZ
);

CREATE TABLE ssh_host (
    id           BIGSERIAL PRIMARY KEY,
    type         VARCHAR(8)   NOT NULL,             -- host | ssh
    host         VARCHAR(128) NOT NULL,
    port         INTEGER,
    username     VARCHAR(64),
    auth_type    VARCHAR(16),                       -- password | key
    key_path     TEXT,                              -- SSH 私钥路径（密文）
    host_entries JSONB,                             -- Host 解析内容
    description  TEXT,
    version      BIGINT       NOT NULL DEFAULT 1,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at   TIMESTAMPTZ
);

-- =====================================================================
-- 2. 环境 / 标签关联
-- =====================================================================
CREATE TABLE environment (
    id          BIGSERIAL PRIMARY KEY,
    name        VARCHAR(128) NOT NULL,
    code        VARCHAR(64)  NOT NULL,
    category_id BIGINT       NOT NULL REFERENCES environment_category (id),
    type        VARCHAR(16)  NOT NULL DEFAULT 'single', -- single | cluster
    hosts       JSONB,                                  -- IP/域名列表
    os          VARCHAR(64),
    ssh_host_id BIGINT       REFERENCES ssh_host (id),
    cert_id     BIGINT       REFERENCES certificate (id),
    description TEXT,
    status      VARCHAR(16)  NOT NULL DEFAULT 'pending', -- pending | testing | disabled
    version     BIGINT       NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);
CREATE UNIQUE INDEX uq_environment_code ON environment (code) WHERE deleted_at IS NULL;
CREATE INDEX idx_environment_category ON environment (category_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_environment_status ON environment (status) WHERE deleted_at IS NULL;

CREATE TABLE env_tag (
    env_id     BIGINT      NOT NULL REFERENCES environment (id) ON DELETE CASCADE,
    tag_id     BIGINT      NOT NULL REFERENCES tag (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (env_id, tag_id)
);
CREATE INDEX idx_env_tag_tag ON env_tag (tag_id);

-- =====================================================================
-- 3. 产品 / 接口 / 阈值
-- =====================================================================
CREATE TABLE product (
    id          BIGSERIAL PRIMARY KEY,
    name        VARCHAR(128) NOT NULL,
    code        VARCHAR(64)  NOT NULL,
    product_version VARCHAR(32),                 -- 产品软件版本（业务字段，避免与乐观锁 version 冲突）
    platform    VARCHAR(64),
    description TEXT,
    status      VARCHAR(16)  NOT NULL DEFAULT 'enabled', -- enabled | disabled
    version     BIGINT       NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);
CREATE UNIQUE INDEX uq_product_code ON product (code) WHERE deleted_at IS NULL;

CREATE TABLE interface (
    id          BIGSERIAL PRIMARY KEY,
    product_id  BIGINT      NOT NULL REFERENCES product (id),
    name        VARCHAR(128) NOT NULL,
    method      VARCHAR(8),                             -- GET/POST/PUT/DELETE/PATCH
    url         TEXT,
    headers     JSONB,                                  -- 非敏感头
    headers_enc BYTEA,                                  -- 认证头等敏感头（AES-256-GCM 密文）
    body        JSONB,                                  -- 参数模板
    timeouts    JSONB,
    description TEXT,
    enabled     BOOLEAN     NOT NULL DEFAULT TRUE,
    status      VARCHAR(16) NOT NULL DEFAULT 'enabled', -- enabled | disabled
    version     BIGINT      NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);
CREATE INDEX idx_interface_product ON interface (product_id) WHERE deleted_at IS NULL;

CREATE TABLE threshold (
    id           BIGSERIAL PRIMARY KEY,
    interface_id BIGINT           NOT NULL REFERENCES interface (id) ON DELETE CASCADE,
    metric       VARCHAR(32)      NOT NULL,   -- avg_rt/p95/p99/max_rt/throughput/error_rate/success_rate/concurrency
    operator     VARCHAR(8)       NOT NULL,   -- le | ge
    value        DOUBLE PRECISION NOT NULL,
    unit         VARCHAR(16),                  -- ms | % | req/s | 个
    severity     VARCHAR(16),
    enabled      BOOLEAN          NOT NULL DEFAULT TRUE,
    version      BIGINT           NOT NULL DEFAULT 1,
    created_at   TIMESTAMPTZ      NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ      NOT NULL DEFAULT now(),
    deleted_at   TIMESTAMPTZ
);
CREATE INDEX idx_threshold_interface ON threshold (interface_id) WHERE deleted_at IS NULL;

-- =====================================================================
-- 4. 测试模板 / 任务 / 报告
-- =====================================================================
CREATE TABLE test_template (
    id            BIGSERIAL PRIMARY KEY,
    name          VARCHAR(128) NOT NULL,
    env_ids       JSONB,                               -- 环境 ID 列表
    interface_ids JSONB,                               -- 接口 ID 列表
    flow          JSONB,                               -- 流程参数
    report_config JSONB,                               -- 报告输出配置
    clean_config  JSONB,                               -- 清理配置
    description   TEXT,
    version       BIGINT       NOT NULL DEFAULT 1,
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ
);

CREATE TABLE test_task (
    id                       BIGSERIAL PRIMARY KEY,
    template_id              BIGINT,                   -- 来源模板（可空）
    name                     VARCHAR(128) NOT NULL,
    env_id                   BIGINT       NOT NULL REFERENCES environment (id),
    interface_range_snapshot JSONB,                    -- 接口范围快照（创建时固化）
    threshold_snapshot       JSONB,                    -- 阈值快照（创建时固化）
    flow                     JSONB,
    report_config            JSONB,
    clean_config             JSONB,
    status                   VARCHAR(32)  NOT NULL DEFAULT 'pending',
    started_at               TIMESTAMPTZ,
    finished_at              TIMESTAMPTZ,
    result                   JSONB,                    -- 评估/执行结果摘要
    version                  BIGINT       NOT NULL DEFAULT 1,
    created_at               TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at               TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at               TIMESTAMPTZ
);
CREATE INDEX idx_test_task_env ON test_task (env_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_test_task_status ON test_task (status) WHERE deleted_at IS NULL;

CREATE TABLE test_report (
    id         BIGSERIAL PRIMARY KEY,
    task_id    BIGINT,                                -- 可空（外部导入孤立归档）
    product_id BIGINT,                                -- 可空（归属产品）
    source     VARCHAR(16) NOT NULL DEFAULT 'internal', -- internal | external
    type       VARCHAR(16),                           -- html | pdf | word
    title      VARCHAR(256),
    summary    JSONB,                                 -- 达标统计/关键指标摘要
    metrics    JSONB,                                 -- 指标 JSONB
    status     VARCHAR(32) NOT NULL DEFAULT 'generating', -- generating/ready/generation_failed
    version    BIGINT      NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);
CREATE INDEX idx_test_report_task ON test_report (task_id) WHERE deleted_at IS NULL;

CREATE TABLE report_artifact (
    id         BIGSERIAL PRIMARY KEY,
    report_id  BIGINT      NOT NULL REFERENCES test_report (id) ON DELETE CASCADE,
    format     VARCHAR(16) NOT NULL,                  -- html | pdf | word
    path       TEXT        NOT NULL,
    size       BIGINT,
    version    BIGINT      NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at TIMESTAMPTZ
);
CREATE INDEX idx_report_artifact_report ON report_artifact (report_id) WHERE deleted_at IS NULL;

-- =====================================================================
-- 5. 管理员 / 角色 / 权限（分级分权 RBAC）
-- =====================================================================
CREATE TABLE admin (
    id                   BIGSERIAL PRIMARY KEY,
    username             VARCHAR(64)  NOT NULL,
    name                 VARCHAR(64),
    phone                VARCHAR(32),
    email                VARCHAR(128),
    description          TEXT,
    password_hash        VARCHAR(128) NOT NULL,        -- bcrypt
    must_change_password BOOLEAN      NOT NULL DEFAULT TRUE,
    status               VARCHAR(16)  NOT NULL DEFAULT 'enabled', -- enabled | disabled
    last_login_at        TIMESTAMPTZ,
    is_builtin           BOOLEAN      NOT NULL DEFAULT FALSE,
    version              BIGINT       NOT NULL DEFAULT 1,
    created_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at           TIMESTAMPTZ
);
CREATE UNIQUE INDEX uq_admin_username ON admin (username) WHERE deleted_at IS NULL;

CREATE TABLE role (
    id          BIGSERIAL PRIMARY KEY,
    name        VARCHAR(64)  NOT NULL,
    description TEXT,
    version     BIGINT       NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);
CREATE UNIQUE INDEX uq_role_name ON role (name) WHERE deleted_at IS NULL;

CREATE TABLE role_permission (
    id          BIGSERIAL PRIMARY KEY,
    role_id     BIGINT      NOT NULL REFERENCES role (id) ON DELETE CASCADE,
    module_code VARCHAR(64) NOT NULL,                  -- 子模块授权码
    permission  VARCHAR(8)  NOT NULL,                  -- read | write
    version     BIGINT      NOT NULL DEFAULT 1,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at  TIMESTAMPTZ
);
CREATE INDEX idx_role_permission_role ON role_permission (role_id) WHERE deleted_at IS NULL;

CREATE TABLE admin_role (
    admin_id   BIGINT      NOT NULL REFERENCES admin (id) ON DELETE CASCADE,
    role_id    BIGINT      NOT NULL REFERENCES role (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (admin_id, role_id)
);
CREATE INDEX idx_admin_role_role ON admin_role (role_id);

-- =====================================================================
-- 6. 日志（按 created_at RANGE 季度分区）
-- =====================================================================
CREATE TABLE user_log (
    id            BIGSERIAL,
    admin_id      BIGINT,                             -- 操作人（5W1H 之 Who）
    action        VARCHAR(64) NOT NULL,               -- 动作枚举
    resource_type VARCHAR(64),
    resource_id   VARCHAR(64),
    detail        TEXT,                               -- 已脱敏细节
    ip            VARCHAR(64),
    result        VARCHAR(16),                        -- ok | fail
    prev_hash     BYTEA,                              -- 上一条审计 hash（HMAC 链）
    hash          BYTEA,                              -- 本条目 HMAC-SHA256
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- 季度分区示例（2026 Q4）。后续季度由运维按需添加：
--   CREATE TABLE user_log_2027q1 PARTITION OF user_log
--       FOR VALUES FROM ('2027-01-01') TO ('2027-04-01');
CREATE TABLE user_log_2026q4 PARTITION OF user_log
    FOR VALUES FROM ('2026-10-01') TO ('2027-01-01');

CREATE TABLE system_log (
    id         BIGSERIAL,
    level      VARCHAR(16) NOT NULL,                  -- info | warn | error
    event      VARCHAR(64),                           -- start | restart | stop
    message    TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

CREATE TABLE system_log_2026q4 PARTITION OF system_log
    FOR VALUES FROM ('2026-10-01') TO ('2027-01-01');

CREATE TABLE test_log (
    id         BIGSERIAL,
    task_id    BIGINT,                                -- 按任务定位
    seq        BIGINT,                                -- 增量序号
    level      VARCHAR(16) NOT NULL,
    message    TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

CREATE TABLE test_log_2026q4 PARTITION OF test_log
    FOR VALUES FROM ('2026-10-01') TO ('2027-01-01');
CREATE INDEX idx_test_log_task ON test_log (task_id);
