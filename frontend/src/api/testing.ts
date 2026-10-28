import { request } from './http'
import type {
  CreateTaskPayload,
  PageResult,
  TaskQuery,
  TemplateQuery,
  TestTaskInfo,
  TestTemplateInfo,
  TestTemplatePayload,
} from '@/types'

// 测试管理接口（api-contract §2.4）。
// 权限点：模板 testing.template，任务 testing.task；read 用于查询、write 用于变更。
// 业务错误码：23001 状态不允许 / 23002 环境占用 / 23003 接口范围为空 /
//             23004 并发数非法 / 23005 模板不存在（由页面按 code 分支提示）。

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

// ===================== 模板 =====================

/** 模板列表（关键字 + 分页）。 */
export function listTemplates(query: TemplateQuery = {}): Promise<PageResult<TestTemplateInfo>> {
  return request<PageResult<TestTemplateInfo>>(`/templates${buildQuery({ ...query })}`)
}

/** 模板详情（不存在 23005）。 */
export function getTemplate(id: string): Promise<TestTemplateInfo> {
  return request<TestTemplateInfo>(`/templates/${id}`)
}

/** 新建模板（流程/报告/清理配置必填，白名单校验失败 10003）。 */
export function createTemplate(payload: TestTemplatePayload): Promise<TestTemplateInfo> {
  return request<TestTemplateInfo>('/templates', { method: 'POST', body: payload })
}

/** 修改模板（必须携带 version；并发冲突 10402）。 */
export function updateTemplate(
  id: string,
  payload: TestTemplatePayload,
): Promise<TestTemplateInfo> {
  return request<TestTemplateInfo>(`/templates/${id}`, { method: 'PUT', body: payload })
}

/** 删除模板（逻辑删除；不存在 23005）。 */
export function deleteTemplate(id: string): Promise<null> {
  return request<null>(`/templates/${id}`, { method: 'DELETE' })
}

// ===================== 任务 =====================

/** 任务列表（关键字/状态/环境/产品/模板/创建时间 + 分页）。 */
export function listTasks(query: TaskQuery = {}): Promise<PageResult<TestTaskInfo>> {
  return request<PageResult<TestTaskInfo>>(`/tasks${buildQuery({ ...query })}`)
}

/** 任务详情（不存在 10301）。 */
export function getTask(id: string): Promise<TestTaskInfo> {
  return request<TestTaskInfo>(`/tasks/${id}`)
}

/** 创建任务（五要素组装 + 接口/阈值快照；环境占用 23002 / 范围为空 23003 / 并发非法 23004）。 */
export function createTask(payload: CreateTaskPayload): Promise<TestTaskInfo> {
  return request<TestTaskInfo>('/tasks', { method: 'POST', body: payload })
}

/** 执行任务（pending → running；非 pending 23001、环境占用 23002）。 */
export function startTask(id: string): Promise<TestTaskInfo> {
  return request<TestTaskInfo>(`/tasks/${id}/start`, { method: 'POST' })
}

/** 停止任务（running → cancelled → cleanup_pending）。 */
export function cancelTask(id: string): Promise<TestTaskInfo> {
  return request<TestTaskInfo>(`/tasks/${id}/cancel`, { method: 'POST' })
}

/** 重试失败任务（failed / cleanup_pending → pending，复用原快照）。 */
export function retryTask(id: string): Promise<TestTaskInfo> {
  return request<TestTaskInfo>(`/tasks/${id}/retry`, { method: 'POST' })
}

/** 人工确认清理（仅 cleanup_pending → cleaning → finished）。 */
export function cleanupTask(id: string): Promise<TestTaskInfo> {
  return request<TestTaskInfo>(`/tasks/${id}/cleanup`, { method: 'POST' })
}

/** 同步待执行任务的接口/阈值快照（接口配置变更后立即刷新）。 */
export function refreshTaskSnapshot(id: string): Promise<TestTaskInfo> {
  return request<TestTaskInfo>(`/tasks/${id}/refresh-snapshot`, { method: 'POST' })
}
