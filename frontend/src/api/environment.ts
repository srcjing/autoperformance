import { request } from './http'
import type {
  CategoryPayload,
  EnvironmentCategory,
  EnvironmentInfo,
  EnvironmentPayload,
  EnvironmentQuery,
  PageResult,
  TagInfo,
  TagPayload,
} from '@/types'

// 环境管理接口（api-contract §2.2）。
// 权限点：环境/分类 env.list，标签 env.tag；read 用于查询、write 用于变更。

/** 构造 query 字符串（跳过空值；tag_ids 以逗号分隔取交集）。 */
function buildQuery(params: Record<string, string | number | string[] | undefined>): string {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === '') continue
    if (Array.isArray(value)) {
      if (value.length > 0) search.set(key, value.join(','))
      continue
    }
    search.set(key, String(value))
  }
  const qs = search.toString()
  return qs ? `?${qs}` : ''
}

// ===================== 环境分类 =====================

/** 环境分类列表。 */
export function listCategories(): Promise<{ list: EnvironmentCategory[] }> {
  return request<{ list: EnvironmentCategory[] }>('/environment-categories')
}

/** 新建环境分类（重名 10302）。 */
export function createCategory(payload: CategoryPayload): Promise<EnvironmentCategory> {
  return request<EnvironmentCategory>('/environment-categories', { method: 'POST', body: payload })
}

/** 修改环境分类（10301/10302）。 */
export function updateCategory(id: string, payload: CategoryPayload): Promise<EnvironmentCategory> {
  return request<EnvironmentCategory>(`/environment-categories/${id}`, { method: 'PUT', body: payload })
}

/** 删除环境分类（被引用 21002）。 */
export function deleteCategory(id: string): Promise<null> {
  return request<null>(`/environment-categories/${id}`, { method: 'DELETE' })
}

// ===================== 标签 =====================

/** 标签列表（含关联环境数）。 */
export function listTags(keyword = ''): Promise<{ list: TagInfo[] }> {
  return request<{ list: TagInfo[] }>(`/tags${buildQuery({ keyword })}`)
}

/** 新建标签（重名 10302）。 */
export function createTag(payload: TagPayload): Promise<TagInfo> {
  return request<TagInfo>('/tags', { method: 'POST', body: payload })
}

/** 修改标签（10301）。 */
export function updateTag(id: string, payload: TagPayload): Promise<TagInfo> {
  return request<TagInfo>(`/tags/${id}`, { method: 'PUT', body: payload })
}

/** 删除标签（被引用 21003）。 */
export function deleteTag(id: string): Promise<null> {
  return request<null>(`/tags/${id}`, { method: 'DELETE' })
}

/** 标签关联环境反查（分页）。 */
export function listTagEnvironments(
  id: string,
  page = 1,
  pageSize = 20,
): Promise<PageResult<EnvironmentInfo>> {
  return request<PageResult<EnvironmentInfo>>(
    `/tags/${id}/environments${buildQuery({ page, page_size: pageSize })}`,
  )
}

// ===================== 环境 =====================

/** 环境列表（关键字/分类/标签交集/状态 + 分页）。 */
export function listEnvironments(query: EnvironmentQuery = {}): Promise<PageResult<EnvironmentInfo>> {
  return request<PageResult<EnvironmentInfo>>(`/environments${buildQuery({ ...query })}`)
}

/** 环境详情。 */
export function getEnvironment(id: string): Promise<EnvironmentInfo> {
  return request<EnvironmentInfo>(`/environments/${id}`)
}

/** 新建环境（编码重复 21001；分类必填）。 */
export function createEnvironment(payload: EnvironmentPayload): Promise<EnvironmentInfo> {
  return request<EnvironmentInfo>('/environments', { method: 'POST', body: payload })
}

/** 全量修改环境（必须携带 version；冲突 10402）。 */
export function updateEnvironment(id: string, payload: EnvironmentPayload): Promise<EnvironmentInfo> {
  return request<EnvironmentInfo>(`/environments/${id}`, { method: 'PUT', body: payload })
}

/** 删除环境（逻辑删除；testing 时 21004）。 */
export function deleteEnvironment(id: string): Promise<null> {
  return request<null>(`/environments/${id}`, { method: 'DELETE' })
}

/** 全量替换环境标签（PUT /environments/{id}/tags）。 */
export function bindEnvironmentTags(id: string, tagIds: string[]): Promise<null> {
  return request<null>(`/environments/${id}/tags`, { method: 'PUT', body: { tag_ids: tagIds } })
}

/** 停用/启用环境（pending ↔ disabled；testing 时 21004/10401）。 */
export function updateEnvironmentStatus(id: string, status: 'pending' | 'disabled'): Promise<null> {
  return request<null>(`/environments/${id}/status`, { method: 'PATCH', body: { status } })
}
