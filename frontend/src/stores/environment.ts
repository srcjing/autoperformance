import { defineStore } from 'pinia'
import { ref } from 'vue'
import {
  bindEnvironmentTags,
  createEnvironment,
  createTag,
  deleteEnvironment,
  deleteTag,
  listCategories,
  listEnvironments,
  listTagEnvironments,
  listTags,
  updateEnvironment,
  updateEnvironmentStatus,
  updateTag,
} from '@/api/environment'
import type {
  EnvironmentCategory,
  EnvironmentInfo,
  EnvironmentPayload,
  EnvironmentQuery,
  EnvironmentStatus,
  TagInfo,
  TagPayload,
} from '@/types'

// 环境 Store（frontend-structure.md §3）：环境与标签的跨页面共享数据。
// 页面局部（弹窗开关/表单草稿）留在组件，领域数据在此集中，避免重复请求。
export const useEnvironmentStore = defineStore('environment', () => {
  const environments = ref<EnvironmentInfo[]>([])
  const total = ref(0)
  const page = ref(1)
  const pageSize = ref(20)
  const categories = ref<EnvironmentCategory[]>([])
  const tags = ref<TagInfo[]>([])
  const tagEnvMap = ref<Record<string, { list: EnvironmentInfo[]; total: number }>>({})
  const loading = ref(false)

  /** 环境列表（筛选 + 分页）。 */
  async function fetchList(query: EnvironmentQuery = {}): Promise<void> {
    loading.value = true
    try {
      const res = await listEnvironments(query)
      environments.value = res.list
      total.value = res.total
      page.value = res.page
      pageSize.value = res.page_size
    } finally {
      loading.value = false
    }
  }

  /** 环境分类（筛选与表单共用）。 */
  async function fetchCategories(): Promise<void> {
    const res = await listCategories()
    categories.value = res.list
  }

  /** 标签列表（含关联环境数）。 */
  async function fetchTags(keyword = ''): Promise<void> {
    const res = await listTags(keyword)
    tags.value = res.list
  }

  /** 新建/修改环境（id 为空表示新建）。 */
  async function saveEnvironment(id: string | null, payload: EnvironmentPayload): Promise<EnvironmentInfo> {
    const saved = id ? await updateEnvironment(id, payload) : await createEnvironment(payload)
    return saved
  }

  /** 删除环境（逻辑删除）。 */
  async function removeEnvironment(id: string): Promise<void> {
    await deleteEnvironment(id)
  }

  /** 停用/启用环境。 */
  async function setStatus(id: string, status: EnvironmentStatus): Promise<void> {
    if (status === 'testing') return // 状态由任务编排驱动，前端不发起
    await updateEnvironmentStatus(id, status)
  }

  /** 全量替换环境标签。 */
  async function bindTags(id: string, tagIds: string[]): Promise<void> {
    await bindEnvironmentTags(id, tagIds)
  }

  /** 标签关联环境反查（按标签缓存分页结果）。 */
  async function fetchTagEnvironments(tagId: string, pageNo = 1, size = 10): Promise<void> {
    const res = await listTagEnvironments(tagId, pageNo, size)
    tagEnvMap.value = {
      ...tagEnvMap.value,
      [tagId]: { list: res.list, total: res.total },
    }
  }

  /** 新建标签。 */
  async function saveTag(id: string | null, payload: TagPayload): Promise<TagInfo> {
    const saved = id ? await updateTag(id, payload) : await createTag(payload)
    return saved
  }

  /** 删除标签。 */
  async function removeTag(id: string): Promise<void> {
    await deleteTag(id)
  }

  return {
    environments,
    total,
    page,
    pageSize,
    categories,
    tags,
    tagEnvMap,
    loading,
    fetchList,
    fetchCategories,
    fetchTags,
    saveEnvironment,
    removeEnvironment,
    setStatus,
    bindTags,
    fetchTagEnvironments,
    saveTag,
    removeTag,
  }
})
