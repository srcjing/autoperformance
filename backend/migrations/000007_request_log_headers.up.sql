-- 000007: 请求详情补充请求头 / 响应头
-- PRD §3.5：请求详情需可排查「参数是否带上、服务端返回了什么头」。
-- 安全：认证类头（Cookie/Authorization/Token/*Key*/Set-Cookie 等）在 k6 侧即脱敏为
--       `******(len=N)`，明文不进入 stdout、不落库（对齐 DES-I-001 / INV-7）。
-- 体积：两端各截断至 4KB（与 request_body 一致）。

ALTER TABLE request_log ADD COLUMN IF NOT EXISTS request_headers  TEXT NOT NULL DEFAULT '';
ALTER TABLE request_log ADD COLUMN IF NOT EXISTS response_headers TEXT NOT NULL DEFAULT '';
