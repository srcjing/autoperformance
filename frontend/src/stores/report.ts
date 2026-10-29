import { defineStore } from 'pinia'
import { ref } from 'vue'
import {
  downloadArtifact,
  fetchArtifact,
  generateReport,
  getReport,
  importReport,
  listReports,
} from '@/api/report'
import type { ReportFormat, ReportInfo, ReportQuery } from '@/types'

// 报告 Store（frontend-structure.md §3）：列表 + 详情 + 生成/导入 + 产物预览下载。
// 页面局部状态（筛选条件/弹窗开关/Tab 选中）留在组件内。

export const useReportStore = defineStore('report', () => {
  // ===================== 列表 =====================
  const reports = ref<ReportInfo[]>([])
  const total = ref(0)
  const page = ref(1)
  const pageSize = ref(20)
  const loading = ref(false)

  // ===================== 详情 =====================
  const currentReport = ref<ReportInfo | null>(null)
  const detailLoading = ref(false)

  /** 报告列表（筛选 + 分页）。 */
  async function fetchReports(query: ReportQuery = {}): Promise<void> {
    loading.value = true
    try {
      const res = await listReports(query)
      reports.value = res.list
      total.value = res.total
      page.value = res.page
      pageSize.value = res.page_size
    } finally {
      loading.value = false
    }
  }

  /** 报告详情（silent=true 用于轮询/刷新，不触发整页 loading）。 */
  async function fetchReport(id: string, silent = false): Promise<ReportInfo> {
    if (!silent) detailLoading.value = true
    try {
      const report = await getReport(id)
      currentReport.value = report
      return report
    } finally {
      if (!silent) detailLoading.value = false
    }
  }

  /** 重新生成报告（仅 generation_failed / ready；生成中 10401、失败 24004）。 */
  async function regenerate(id: string): Promise<ReportInfo> {
    const report = await generateReport(id)
    currentReport.value = report
    reports.value = reports.value.map((item) => (item.id === id ? report : item))
    return report
  }

  /** 导入外部报告（无任务归属时为孤立归档）。 */
  async function upload(file: File, taskId = '', productId = ''): Promise<ReportInfo> {
    return await importReport(file, taskId, productId)
  }

  /** 拉取产物 Blob（供 iframe/预览组件创建 objectURL）。 */
  async function previewBlob(reportId: string, format: ReportFormat): Promise<Blob> {
    return await fetchArtifact(reportId, format)
  }

  /** 导出产物（触发浏览器下载）。 */
  async function exportFile(
    reportId: string,
    format: ReportFormat,
    filename?: string,
  ): Promise<void> {
    await downloadArtifact(reportId, format, filename)
  }

  return {
    reports,
    total,
    page,
    pageSize,
    loading,
    currentReport,
    detailLoading,
    fetchReports,
    fetchReport,
    regenerate,
    upload,
    previewBlob,
    exportFile,
  }
})
