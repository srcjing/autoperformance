import { request } from './http'
import type {
  PageResult,
  SystemLogInfo,
  SystemLogQuery,
  TestLogInfo,
  TestLogQuery,
  UserLogInfo,
  UserLogQuery,
} from '@/types'

// 日志管理接口（api-contract §2.6）。
// 权限点：log.user:read / log.system:read / log.test:read —— 三类日志均只读，无写端点。
// 业务错误码：25001 查询时间范围非法（start_time > end_time）。
// 安全约束（DES-T-006 / DES-I-001）：日志为只读接口；审计链 HMAC 防篡改、写入前统一脱敏。

/** 构造 query 字符串（跳过空值）。 */
function buildQuery(params: Record<string, string | number | undefined>): string {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === '') continue
    search.set(key, String(value))
  }
  const qs = search.toString()
  return qs ? `?${qs}` : ''
}

/** 用户日志查询（操作人/动作/关联对象/级别/关键字/时间范围 + 分页）。 */
export function listUserLogs(query: UserLogQuery = {}): Promise<PageResult<UserLogInfo>> {
  return request<PageResult<UserLogInfo>>(`/logs/user${buildQuery({ ...query })}`)
}

/** 系统日志查询（事件/级别/关键字/时间范围 + 分页）。 */
export function listSystemLogs(query: SystemLogQuery = {}): Promise<PageResult<SystemLogInfo>> {
  return request<PageResult<SystemLogInfo>>(`/logs/system${buildQuery({ ...query })}`)
}

/** 测试日志查询（task_id 定位 + 级别/关键字/时间范围 + 分页）。 */
export function listTestLogs(query: TestLogQuery = {}): Promise<PageResult<TestLogInfo>> {
  return request<PageResult<TestLogInfo>>(`/logs/test${buildQuery({ ...query })}`)
}
