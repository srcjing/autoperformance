import { ApiError } from '@/api/http'
import type { BadgeMeta } from './report'

// 日志管理展示元数据与错误文案（权限点 log.user / log.system / log.test，均只读）。
// 日志级别着色统一走 ap-badge-*：info=蓝、warn=黄、error=红。

/** HTTP 方法着色（读操作蓝、写操作黄/红）。 */
export function httpMethodBadge(method?: string): BadgeMeta {
  switch ((method || '').toUpperCase()) {
    case 'GET':
    case 'HEAD':
      return { label: 'GET', badge: 'ap-badge-info' }
    case 'POST':
      return { label: 'POST', badge: 'ap-badge-success' }
    case 'PUT':
    case 'PATCH':
      return { label: (method || '').toUpperCase(), badge: 'ap-badge-warn' }
    case 'DELETE':
      return { label: 'DELETE', badge: 'ap-badge-danger' }
    default:
      return { label: (method || '-').toUpperCase(), badge: 'ap-badge-muted' }
  }
}

/** HTTP 状态码着色：2xx/3xx 正常、4xx 警告、5xx/0（网络失败）异常。 */
export function httpStatusBadge(status?: number): BadgeMeta {
  const code = status ?? 0
  if (code >= 200 && code < 400) return { label: String(code), badge: 'ap-badge-success' }
  if (code >= 400 && code < 500) return { label: String(code), badge: 'ap-badge-warn' }
  if (code >= 500) return { label: String(code), badge: 'ap-badge-danger' }
  return { label: code === 0 ? '失败' : String(code), badge: 'ap-badge-danger' }
}

/** 耗时展示（毫秒 → 人类可读）。 */
export function formatDurationMs(ms?: number): string {
  const value = ms ?? 0
  if (value <= 0) return '-'
  if (value < 1000) return `${value} ms`
  return `${(value / 1000).toFixed(2)} s`
}

/** 日志类型选项（日志设置勾选，与后端 domain.LogType* 对齐）。 */
export const LOG_TYPE_OPTIONS: Array<{ value: 'user' | 'system' | 'test' | 'request'; label: string }> = [
  { value: 'user', label: '用户日志（操作审计）' },
  { value: 'system', label: '系统日志（运行状态）' },
  { value: 'test', label: '测试日志（任务执行）' },
  { value: 'request', label: '请求详情（压测逐请求）' },
]

/** 日志级别元数据（info / warn / error）。 */
export const LOG_LEVEL_META: Record<string, BadgeMeta> = {
  info: { label: 'INFO', badge: 'ap-badge-info' },
  warn: { label: 'WARN', badge: 'ap-badge-warn' },
  error: { label: 'ERROR', badge: 'ap-badge-danger' },
}

/** 日志级别元数据（未知级别兜底展示原值）。 */
export function logLevelMeta(level?: string): BadgeMeta {
  if (!level) return { label: '-', badge: 'ap-badge-muted' }
  return LOG_LEVEL_META[level] ?? { label: level.toUpperCase(), badge: 'ap-badge-muted' }
}

/** 日志级别下拉选项。 */
export const LOG_LEVEL_OPTIONS: Array<{ value: string; label: string }> = [
  { value: 'info', label: 'INFO' },
  { value: 'warn', label: 'WARN' },
  { value: 'error', label: 'ERROR' },
]

/** 审计结果元数据（用户日志 result：ok / fail）。 */
export const AUDIT_RESULT_META: Record<string, BadgeMeta> = {
  ok: { label: '成功', badge: 'ap-badge-success' },
  fail: { label: '失败', badge: 'ap-badge-danger' },
}

/** 审计结果元数据（未知结果兜底）。 */
export function auditResultMeta(result?: string): BadgeMeta {
  if (!result) return { label: '-', badge: 'ap-badge-muted' }
  return AUDIT_RESULT_META[result] ?? { label: result, badge: 'ap-badge-muted' }
}

/** 动作展示文案（与后端 middleware/audit.go auditAction 枚举对齐）。 */
export const AUDIT_ACTION_LABEL: Record<string, string> = {
  login: '登录',
  logout: '登出',
  change_password: '修改密码',
  create: '新增',
  update: '修改',
  delete: '删除',
  read: '查询',
  start: '执行任务',
  stop: '停止任务',
  retry: '重试任务',
  cleanup: '清理环境',
  generate: '生成报告',
  export: '导出报告',
  view_artifact: '查看报告',
  import: '导入报告',
  reinit: '重新初始化',
  grant_roles: '角色授权',
  bind_tags: '绑定标签',
  update_status: '状态变更',
  reset_password: '重置密码',
}

/** 动作查询下拉选项（按操作频次排序）。 */
export const AUDIT_ACTION_OPTIONS: Array<{ value: string; label: string }> = [
  'login',
  'logout',
  'change_password',
  'create',
  'update',
  'delete',
  'start',
  'stop',
  'retry',
  'cleanup',
  'generate',
  'export',
  'import',
  'grant_roles',
  'bind_tags',
  'update_status',
  'reset_password',
].map((value) => ({ value, label: AUDIT_ACTION_LABEL[value] ?? value }))

/** 动作展示文案。 */
export function auditActionLabel(action?: string): string {
  if (!action) return '-'
  return AUDIT_ACTION_LABEL[action] ?? action
}

/** 关联对象类型展示文案（与后端 auditResourceType 对齐）。 */
export const AUDIT_RESOURCE_LABEL: Record<string, string> = {
  environment: '环境',
  environment_category: '环境分类',
  tag: '标签',
  product: '产品',
  interface: '接口',
  template: '测试模板',
  task: '测试任务',
  report: '测试报告',
  log: '日志',
  admin: '管理员',
  role: '角色',
  permission: '权限',
  certificate: '证书',
  ssh_host: 'SSH / Host',
  auth: '认证',
}

/** 关联对象类型下拉选项。 */
export const AUDIT_RESOURCE_OPTIONS: Array<{ value: string; label: string }> = Object.entries(
  AUDIT_RESOURCE_LABEL,
).map(([value, label]) => ({ value, label }))

/** 关联对象类型展示文案。 */
export function auditResourceLabel(type?: string): string {
  if (!type) return '-'
  return AUDIT_RESOURCE_LABEL[type] ?? type
}

/** 系统日志事件元数据（start / restart / stop）。 */
export const SYSTEM_EVENT_META: Record<string, BadgeMeta> = {
  start: { label: '服务启动', badge: 'ap-badge-success' },
  restart: { label: '服务重启', badge: 'ap-badge-warn' },
  stop: { label: '服务停止', badge: 'ap-badge-muted' },
}

/** 系统日志事件下拉选项。 */
export const SYSTEM_EVENT_OPTIONS: Array<{ value: string; label: string }> = [
  { value: 'start', label: '服务启动' },
  { value: 'restart', label: '服务重启' },
  { value: 'stop', label: '服务停止' },
]

/** 系统日志事件元数据（未知事件兜底展示原值）。 */
export function systemEventMeta(event?: string): BadgeMeta {
  if (!event) return { label: '-', badge: 'ap-badge-muted' }
  return SYSTEM_EVENT_META[event] ?? { label: event, badge: 'ap-badge-muted' }
}

/** 日志相关错误文案（按业务码分支）。 */
export function describeLogError(error: unknown, fallback: string): string {
  if (!(error instanceof ApiError)) return fallback
  switch (error.code) {
    case 25001:
      return '查询时间范围非法：开始时间不能晚于结束时间'
    case 10003:
      return '查询条件格式非法，请检查时间/级别/ID 参数'
    case 10301:
      return '关联对象不存在或已被删除'
    case 10201:
      return '无权访问该日志子模块，请联系管理员授权'
    case 10602:
      return '服务依赖未就绪（如数据库不可用），请稍后重试'
    default:
      return error.message || fallback
  }
}
