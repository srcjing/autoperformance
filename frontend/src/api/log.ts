import { request } from './http'
import type {
  LogSettingInfo,
  LogSettingPayload,
  PageResult,
  RequestLogInfo,
  RequestLogQuery,
  SystemLogInfo,
  SystemLogQuery,
  TestLogInfo,
  TestLogQuery,
  UserLogInfo,
  UserLogQuery,
} from '@/types'

// 日志管理接口（api-contract §2.6）。
// 查询权限点：log.user:read / log.system:read / log.test:read / log.request:read（日志只读）。
// 日志设置：setting.log:read（查看）/ setting.log:write（保存），系统管理类配置。
// 业务错误码：25001 查询时间范围非法（start_time > end_time）。
// 安全约束（DES-T-006 / DES-I-001）：日志查询无删除/修改端点；请求体/响应体写入前已脱敏截断。

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

/** 请求详情查询（task_id/interface_id 定位 + 关键字/时间范围 + 分页）。 */
export function listRequestLogs(query: RequestLogQuery = {}): Promise<PageResult<RequestLogInfo>> {
  return request<PageResult<RequestLogInfo>>(`/logs/request${buildQuery({ ...query })}`)
}

/** 读取日志设置（服务开关/最大条数/存储天数/启用类型）。 */
export function getLogSetting(): Promise<LogSettingInfo> {
  return request<LogSettingInfo>('/log-settings')
}

/** 保存日志设置（保存后服务端立即按新策略清理一次过期/超量日志）。 */
export function updateLogSetting(payload: LogSettingPayload): Promise<LogSettingInfo> {
  return request<LogSettingInfo>('/log-settings', { method: 'PUT', body: payload })
}
