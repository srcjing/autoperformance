import { computed, ref } from 'vue'
import { listEnvironments } from '@/api/environment'
import { useAuthStore } from '@/stores/auth'
import type { EnvironmentInfo } from '@/types'

/**
 * 环境选项（测试任务筛选/详情展示环境名称）。
 *
 * 与 useEnvironmentStore 分离：这里只做「只读选项」拉取，不改动环境管理页的共享列表状态。
 * 缺少 env.list:read 时静默跳过（列表回退展示环境 ID）。
 */
export function useEnvironmentOptions() {
  const auth = useAuthStore()
  const options = ref<EnvironmentInfo[]>([])
  const loading = ref(false)
  const error = ref('')

  const names = computed<Record<string, string>>(() => {
    const map: Record<string, string> = {}
    for (const item of options.value) map[item.id] = item.name
    return map
  })

  async function load(): Promise<void> {
    if (!auth.hasPerm('env.list', 'read')) return
    loading.value = true
    error.value = ''
    try {
      const res = await listEnvironments({ page: 1, page_size: 200 })
      options.value = res.list
    } catch {
      error.value = '加载环境列表失败'
    } finally {
      loading.value = false
    }
  }

  return { options, names, loading, error, load }
}
