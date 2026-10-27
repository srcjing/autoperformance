-- 000005: 接口地址拆分（协议 + 端口 + 路径）+ 任务压测目标 host
-- PRD §4.5：接口地址 = scheme（http/https）+ port（1-65535）+ path（以 / 开头），接口不再配置 host；
-- PRD §4.8：任务保存 target_host（从所选环境 hosts 中任选一个），执行期完整地址 =
--           {scheme}://{target_host}:{port}{path}。
-- 兼容：保留原 url 列（历史数据不再读取），迁移时按 url 解析并回填三段，解析失败取默认值。

ALTER TABLE interface ADD COLUMN IF NOT EXISTS scheme VARCHAR(8) NOT NULL DEFAULT 'http';
ALTER TABLE interface ADD COLUMN IF NOT EXISTS port INT NOT NULL DEFAULT 80;
ALTER TABLE interface ADD COLUMN IF NOT EXISTS path TEXT NOT NULL DEFAULT '/';

-- 回填：仅处理非空 url；协议按前缀判定（http/https），端口取 authority 中显式端口，
-- 路径取 authority 之后的第一个 '/' 起始段（剥离 query/fragment）。解析失败保留默认值。
WITH parsed AS (
    SELECT id,
           CASE
               WHEN lower(btrim(url)) LIKE 'https://%' THEN 'https'
               WHEN lower(btrim(url)) LIKE 'http://%' THEN 'http'
               ELSE 'http'
           END AS scheme,
           NULLIF(substring(btrim(url) FROM '^[A-Za-z][A-Za-z0-9+.-]*://[^/:?#]+:([0-9]{1,5})'), '')::int AS raw_port,
           NULLIF(substring(btrim(url) FROM '^[A-Za-z][A-Za-z0-9+.-]*://[^/?#]*(/[^?#]*)'), '') AS raw_path
    FROM interface
    WHERE url IS NOT NULL AND btrim(url) <> ''
)
UPDATE interface i
SET scheme = p.scheme,
    port = CASE
               WHEN p.raw_port BETWEEN 1 AND 65535 THEN p.raw_port
               WHEN p.scheme = 'https' THEN 443
               ELSE 80
           END,
    path = COALESCE(p.raw_path, '/')
FROM parsed p
WHERE i.id = p.id;

-- 数据库层兜底校验（与 service/domain 白名单一致，防止绕过接口直接写库）。
ALTER TABLE interface ADD CONSTRAINT ck_interface_scheme CHECK (scheme IN ('http', 'https'));
ALTER TABLE interface ADD CONSTRAINT ck_interface_port CHECK (port BETWEEN 1 AND 65535);
ALTER TABLE interface ADD CONSTRAINT ck_interface_path CHECK (path LIKE '/%');

-- 任务压测目标 host（从所选环境 hosts 任选；历史任务留空）。
ALTER TABLE test_task ADD COLUMN IF NOT EXISTS target_host TEXT NOT NULL DEFAULT '';
