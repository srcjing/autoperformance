import { API_BASE_URL, ApiError, getToken, request } from './http'
import type { ApiResponse, PageResult, ReportFormat, ReportInfo, ReportQuery } from '@/types'

// 报告管理接口（api-contract §2.5）。
// 权限点：report.list:read（查询/查看/导出）、report.list:write（重新生成/导入）。
// 业务错误码：24001 报告文件不存在 / 24002 类型不允许 / 24003 过大 / 24004 生成失败。
// 说明：产物下载需携带 Authorization 头，iframe 无法自定义请求头，因此统一先 fetch 成 Blob
// 再由页面用 objectURL 预览 / 触发下载（不暴露文件系统 URL）。

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

/** 报告列表（任务/类型/状态/来源/时间/关键字 + 分页）。 */
export function listReports(query: ReportQuery = {}): Promise<PageResult<ReportInfo>> {
  return request<PageResult<ReportInfo>>(`/reports${buildQuery({ ...query })}`)
}

/** 报告详情（不存在 10301）。 */
export function getReport(id: string): Promise<ReportInfo> {
  return request<ReportInfo>(`/reports/${id}`)
}

/** 生成/重新生成报告（仅 generation_failed / ready；生成中 10401、失败 24004）。 */
export function generateReport(id: string): Promise<ReportInfo> {
  return request<ReportInfo>(`/reports/${id}/generate`, { method: 'POST' })
}

/** 导入外部报告（multipart：file 必填，task_id/product_id 可选；24002/24003）。 */
export function importReport(file: File, taskId = '', productId = ''): Promise<ReportInfo> {
  const form = new FormData()
  form.append('file', file)
  if (taskId) form.append('task_id', taskId)
  if (productId) form.append('product_id', productId)
  return request<ReportInfo>('/reports/import', { method: 'POST', body: form })
}

/** 拉取报告产物（Blob）。 */
export async function fetchArtifact(id: string, format: ReportFormat): Promise<Blob> {
  const headers = new Headers()
  const token = getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)

  const res = await fetch(`${API_BASE_URL}/reports/${id}/artifact?format=${format}`, {
    headers,
  })
  if (!res.ok) {
    let payload: ApiResponse<unknown> | null = null
    try {
      payload = (await res.json()) as ApiResponse<unknown>
    } catch {
      payload = null
    }
    throw new ApiError(payload?.code ?? res.status, payload?.message || `获取报告文件失败(${res.status})`)
  }
  return await res.blob()
}

/** 下载报告产物（离线读取）。 */
export async function downloadArtifact(
  id: string,
  format: ReportFormat,
  filename?: string,
): Promise<void> {
  const headers = new Headers()
  const token = getToken()
  if (token) headers.set('Authorization', `Bearer ${token}`)

  const res = await fetch(`${API_BASE_URL}/reports/${id}/export?format=${format}`, { headers })
  if (!res.ok) {
    let payload: ApiResponse<unknown> | null = null
    try {
      payload = (await res.json()) as ApiResponse<unknown>
    } catch {
      payload = null
    }
    throw new ApiError(payload?.code ?? res.status, payload?.message || `导出报告失败(${res.status})`)
  }

  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = filename || `report-${id}.${extensionOf(format)}`
  document.body.appendChild(link)
  link.click()
  link.remove()
  URL.revokeObjectURL(url)
}

/** 报告格式 → 文件扩展名。 */
export function extensionOf(format: ReportFormat): string {
  return format === 'word' ? 'docx' : format
}
