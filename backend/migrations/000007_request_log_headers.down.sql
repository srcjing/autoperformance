-- 000007 down: 移除请求详情中的请求头 / 响应头列。

ALTER TABLE request_log DROP COLUMN IF EXISTS response_headers;
ALTER TABLE request_log DROP COLUMN IF EXISTS request_headers;
