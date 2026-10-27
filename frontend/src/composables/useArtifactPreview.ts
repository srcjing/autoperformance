import { onBeforeUnmount, ref, watch, type Ref } from 'vue'
import { useReportStore } from '@/stores/report'
import { describeReportError } from '@/utils/report'
import type { ReportFormat } from '@/types'

// 报告产物预览：因产物接口需要 Authorization 头（iframe 无法携带），
// 统一先 fetch 为 Blob，再用 objectURL 渲染；卸载/切换时释放 objectURL 防内存泄漏。
export function useArtifactPreview(
  reportId: Ref<string>,
  format: ReportFormat,
  available: Ref<boolean>,
) {
  const store = useReportStore()
  const url = ref('')
  const loading = ref(false)
  const error = ref('')

  function revoke(): void {
    if (url.value) {
      URL.revokeObjectURL(url.value)
      url.value = ''
    }
  }

  async function load(): Promise<void> {
    revoke()
    error.value = ''
    if (!available.value || !reportId.value) return
    loading.value = true
    try {
      const blob = await store.previewBlob(reportId.value, format)
      url.value = URL.createObjectURL(blob)
    } catch (err) {
      error.value = describeReportError(err, '加载报告预览失败')
    } finally {
      loading.value = false
    }
  }

  watch([reportId, available], () => {
    void load()
  }, { immediate: true })

  onBeforeUnmount(revoke)

  return { url, loading, error, reload: load }
}
