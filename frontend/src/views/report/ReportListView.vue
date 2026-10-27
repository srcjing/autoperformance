<template>
  <div class="ap-page">
    <div class="ap-page-header">
      <div class="title-block">
        <h2 class="ap-page-title">报告列表</h2>
        <span class="ap-muted">
          任务完成后自动生成 HTML / PDF / Word 报告；支持在线查看、导出离线读取与外部报告导入归档
        </span>
      </div>
      <button v-if="canWrite" type="button" class="ap-btn ap-btn-primary" @click="openImport">
        导入报告
      </button>
    </div>

    <form class="ap-card ap-toolbar" @submit.prevent="search">
      <label class="ap-field">
        <span class="ap-field-label">报告标题</span>
        <input v-model="filters.keyword" placeholder="关键字检索" maxlength="128" />
      </label>
      <label class="ap-field">
        <span class="ap-field-label">格式</span>
        <select v-model="filters.type">
          <option value="">全部格式</option>
          <option v-for="item in REPORT_FORMAT_OPTIONS" :key="item.value" :value="item.value">
            {{ item.label }}
          </option>
        </select>
      </label>
      <label class="ap-field">
        <span class="ap-field-label">状态</span>
        <select v-model="filters.status">
          <option value="">全部状态</option>
          <option v-for="item in REPORT_STATUS_OPTIONS" :key="item.value" :value="item.value">
            {{ item.label }}
          </option>
        </select>
      </label>
      <label class="ap-field">
        <span class="ap-field-label">来源</span>
        <select v-model="filters.source">
          <option value="">全部来源</option>
          <option value="internal">平台生成</option>
          <option value="external">外部导入</option>
        </select>
      </label>
      <label class="ap-field">
        <span class="ap-field-label">生成时间起</span>
        <input v-model="filters.startDate" type="date" />
      </label>
      <label class="ap-field">
        <span class="ap-field-label">生成时间止</span>
        <input v-model="filters.endDate" type="date" />
      </label>
      <div class="ap-actions">
        <button type="submit" class="ap-btn ap-btn-primary" :disabled="store.loading">查询</button>
        <button type="button" class="ap-btn ap-btn-ghost" @click="resetFilters">重置</button>
      </div>
    </form>

    <p v-if="errorMessage" class="ap-alert ap-alert-error" role="alert">{{ errorMessage }}</p>

    <ReportTable
      :reports="store.reports"
      :loading="store.loading"
      :can-write="canWrite"
      @export="onExport"
      @regenerate="onRegenerate"
    />

    <div class="ap-pagination">
      <span>共 {{ store.total }} 条，第 {{ store.page }} / {{ totalPages }} 页</span>
      <button
        type="button"
        class="ap-btn ap-btn-ghost ap-btn-sm"
        :disabled="store.page <= 1 || store.loading"
        @click="goPage(store.page - 1)"
      >
        上一页
      </button>
      <button
        type="button"
        class="ap-btn ap-btn-ghost ap-btn-sm"
        :disabled="store.page >= totalPages || store.loading"
        @click="goPage(store.page + 1)"
      >
        下一页
      </button>
    </div>

    <ImportDialog
      :open="importOpen"
      :saving="importing"
      :error-message="importError"
      @close="closeImport"
      @submit="onImport"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import ImportDialog from '@/components/report/ImportDialog.vue'
import ReportTable from '@/components/report/ReportTable.vue'
import { useAuthStore } from '@/stores/auth'
import { useReportStore } from '@/stores/report'
import { describeReportError, reportFormatLabel } from '@/utils/report'
import type { ReportFormat, ReportInfo, ReportQuery } from '@/types'

// 报告列表页（PRD §3.4）：筛选/分页 + 导入 + 导出 + 生成失败重新生成。
// 权限：report.list:write 才能导入/重新生成；read 可查看/导出（最终由服务端强制）。
const store = useReportStore()
const auth = useAuthStore()
const router = useRouter()

const canWrite = computed(() => auth.hasPerm('report.list', 'write'))

const REPORT_FORMAT_OPTIONS: Array<{ value: ReportFormat; label: string }> = [
  { value: 'html', label: 'HTML' },
  { value: 'pdf', label: 'PDF' },
  { value: 'word', label: 'Word' },
]

const REPORT_STATUS_OPTIONS = [
  { value: 'generating', label: '生成中' },
  { value: 'ready', label: '已生成' },
  { value: 'generation_failed', label: '生成失败' },
]

const filters = reactive({
  keyword: '',
  type: '',
  status: '',
  source: '',
  startDate: '',
  endDate: '',
})

const errorMessage = ref('')
const importOpen = ref(false)
const importError = ref('')
const importing = ref(false)

const totalPages = computed(() =>
  Math.max(1, Math.ceil(store.total / Math.max(store.pageSize, 1))),
)

/** 日期（YYYY-MM-DD）→ RFC3339（后端按 UTC 解析）。 */
function toRfc3339(date: string, endOfDay: boolean): string | undefined {
  if (!date) return undefined
  return `${date}T${endOfDay ? '23:59:59' : '00:00:00'}Z`
}

function buildQuery(page: number): ReportQuery {
  return {
    keyword: filters.keyword.trim() || undefined,
    type: filters.type || undefined,
    status: filters.status || undefined,
    source: filters.source || undefined,
    start_time: toRfc3339(filters.startDate, false),
    end_time: toRfc3339(filters.endDate, true),
    page,
    page_size: 20,
  }
}

async function load(page = 1): Promise<void> {
  errorMessage.value = ''
  try {
    await store.fetchReports(buildQuery(page))
  } catch (err) {
    errorMessage.value = describeReportError(err, '加载报告列表失败')
  }
}

function search(): void {
  void load(1)
}

function resetFilters(): void {
  filters.keyword = ''
  filters.type = ''
  filters.status = ''
  filters.source = ''
  filters.startDate = ''
  filters.endDate = ''
  void load(1)
}

function goPage(page: number): void {
  void load(page)
}

// ===================== 导入 =====================

function openImport(): void {
  importError.value = ''
  importOpen.value = true
}

function closeImport(): void {
  if (importing.value) return
  importOpen.value = false
  importError.value = ''
}

async function onImport(payload: { file: File; taskId: string; productId: string }): Promise<void> {
  if (importing.value) return
  importing.value = true
  importError.value = ''
  try {
    const report = await store.upload(payload.file, payload.taskId, payload.productId)
    importOpen.value = false
    await load(1)
    await router.push({ name: 'report-detail', params: { reportId: report.id } })
  } catch (err) {
    // 24002 类型不允许 / 24003 过大 / 10301 归属任务不存在。
    importError.value = describeReportError(err, '导入报告失败')
  } finally {
    importing.value = false
  }
}

// ===================== 导出 / 重新生成 =====================

/** 文件名清洗（去掉路径分隔符与 Windows 保留字符）。 */
function safeFileName(title: string): string {
  return (title || 'report').replace(/[\\/:*?"<>|]/g, '_').slice(0, 120)
}

async function onExport(report: ReportInfo, format: ReportFormat): Promise<void> {
  errorMessage.value = ''
  const extension = format === 'word' ? 'docx' : format
  try {
    await store.exportFile(report.id, format, `${safeFileName(report.title)}.${extension}`)
  } catch (err) {
    errorMessage.value = describeReportError(err, `导出 ${reportFormatLabel(format)} 报告失败`)
  }
}

async function onRegenerate(report: ReportInfo): Promise<void> {
  if (!window.confirm(`确认重新生成报告「${report.title}」？将重新渲染 HTML/PDF/Word 产物。`)) {
    return
  }
  errorMessage.value = ''
  try {
    const updated = await store.regenerate(report.id)
    await load(store.page)
    await router.push({ name: 'report-detail', params: { reportId: updated.id } })
  } catch (err) {
    // 10401 生成中 / 24004 生成失败（可再次重试）
    errorMessage.value = describeReportError(err, '重新生成报告失败')
    await load(store.page)
  }
}

onMounted(() => {
  void load(1)
})
</script>

<style scoped>
.title-block {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.title-block .ap-muted {
  font-size: 12px;
}
</style>
