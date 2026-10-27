<template>
  <div class="ap-page">
    <div class="ap-page-header">
      <div class="title-block">
        <h2 class="ap-page-title">
          报告详情
          <span v-if="report" class="report-name">· {{ report.title }}</span>
        </h2>
        <div class="title-links">
          <RouterLink to="/reports/list" class="ap-link">返回报告列表</RouterLink>
          <RouterLink
            v-if="report?.task_id"
            class="ap-link"
            :to="{ name: 'task-detail', params: { taskId: report.task_id } }"
          >
            关联任务 #{{ report.task_id }}
          </RouterLink>
          <span class="ap-muted">#{{ reportId }}</span>
        </div>
      </div>

      <div v-if="report" class="ap-actions">
        <button
          v-if="canRegenerate"
          type="button"
          class="ap-btn ap-btn-ghost"
          :disabled="acting"
          @click="onRegenerate"
        >
          重新生成
        </button>
        <button
          v-for="format in formats"
          :key="format"
          type="button"
          class="ap-btn ap-btn-primary"
          :disabled="acting"
          @click="onExport(format)"
        >
          导出 {{ reportFormatLabel(format) }}
        </button>
        <button type="button" class="ap-btn ap-btn-ghost" :disabled="store.detailLoading" @click="refresh()">
          刷新
        </button>
      </div>
    </div>

    <p v-if="errorMessage" class="ap-alert ap-alert-error" role="alert">{{ errorMessage }}</p>
    <p v-if="store.detailLoading && !report" class="ap-card ap-table-empty">加载中…</p>

    <template v-if="report">
      <div class="ap-card summary">
        <div class="summary-item">
          <span class="summary-label">状态</span>
          <span class="ap-badge" :class="reportStatusMeta(report.status).badge">
            {{ reportStatusMeta(report.status).label }}
          </span>
        </div>
        <div class="summary-item">
          <span class="summary-label">结论</span>
          <span class="ap-badge" :class="reportStateMeta(report.summary?.state).badge">
            {{ reportStateMeta(report.summary?.state).label }}
          </span>
        </div>
        <div class="summary-item">
          <span class="summary-label">接口达标率</span>
          <span>{{ passRateText }}</span>
        </div>
        <div class="summary-item">
          <span class="summary-label">达标 / 不达标接口</span>
          <span>{{ report.summary?.passed_interfaces ?? 0 }} / {{ report.summary?.failed_interfaces ?? 0 }}</span>
        </div>
        <div class="summary-item">
          <span class="summary-label">阈值项（达标 / 不达标）</span>
          <span>{{ report.summary?.passed_thresholds ?? 0 }} / {{ report.summary?.failed_thresholds ?? 0 }}</span>
        </div>
        <div class="summary-item">
          <span class="summary-label">执行用时</span>
          <span>{{ durationText }}</span>
        </div>
        <div class="summary-item">
          <span class="summary-label">来源</span>
          <span>{{ reportSourceLabel(report.source) }}</span>
        </div>
        <div class="summary-item">
          <span class="summary-label">生成时间</span>
          <span>{{ formatDateTime(report.created_at ?? '') }}</span>
        </div>
      </div>

      <div v-if="report.status === 'generating'" class="ap-alert ap-alert-hint">
        报告正在生成中，页面将自动刷新（生成完成后可在线查看与导出）。
      </div>
      <div v-if="report.status === 'generation_failed'" class="ap-alert ap-alert-error">
        报告生成失败：可点击「重新生成」重试（PDF 生成依赖服务端 headless Chromium）。
      </div>

      <div class="ap-card">
        <div class="tabs">
          <button
            v-for="tab in tabs"
            :key="tab.value"
            type="button"
            class="tab"
            :class="{ active: activeTab === tab.value }"
            :disabled="tab.disabled"
            @click="activeTab = tab.value"
          >
            {{ tab.label }}
          </button>
        </div>

        <div class="tab-body">
          <HtmlPreview
            v-if="activeTab === 'html'"
            :report-id="report.id"
            :available="hasArtifact(report, 'html')"
          />
          <PdfPreview
            v-else-if="activeTab === 'pdf'"
            :report-id="report.id"
            :available="hasArtifact(report, 'pdf')"
          />
          <EvaluationTable
            v-else-if="activeTab === 'evaluation'"
            :items="report.summary?.items ?? []"
          />
          <div v-else class="artifact-panel">
            <table class="ap-table">
              <thead>
                <tr>
                  <th>格式</th>
                  <th>大小</th>
                  <th>查看方式</th>
                  <th>生成时间</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="artifact in report.artifacts" :key="artifact.format">
                  <td>{{ reportFormatLabel(artifact.format) }}</td>
                  <td>{{ formatBytes(artifact.size) }}</td>
                  <td>{{ artifact.inline ? '在线查看（内联）' : '下载（附件）' }}</td>
                  <td>{{ formatDateTime(artifact.created_at ?? '') }}</td>
                  <td>
                    <button
                      type="button"
                      class="ap-btn ap-btn-ghost ap-btn-sm"
                      @click="onExport(artifact.format)"
                    >
                      导出
                    </button>
                  </td>
                </tr>
              </tbody>
            </table>
            <p v-if="report.artifacts.length === 0" class="ap-table-empty">暂无产物</p>

            <div v-if="report.source === 'external' && report.summary" class="import-meta">
              <h4>外部导入信息</h4>
              <div class="meta-line">
                <span class="summary-label">原始文件名</span>
                <span>{{ report.summary.original_filename || '-' }}</span>
              </div>
              <div class="meta-line">
                <span class="summary-label">文件大小</span>
                <span>{{ formatBytes(report.summary.size_bytes ?? 0) }}</span>
              </div>
              <div class="meta-line">
                <span class="summary-label">SHA256</span>
                <code class="sha">{{ report.summary.sha256 || '-' }}</code>
              </div>
            </div>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import EvaluationTable from '@/components/report/EvaluationTable.vue'
import HtmlPreview from '@/components/report/HtmlPreview.vue'
import PdfPreview from '@/components/report/PdfPreview.vue'
import { useAuthStore } from '@/stores/auth'
import { useReportStore } from '@/stores/report'
import { formatDateTime } from '@/utils/format'
import {
  artifactFormats,
  describeReportError,
  formatBytes,
  hasArtifact,
  reportFormatLabel,
  reportSourceLabel,
  reportStateMeta,
  reportStatusMeta,
} from '@/utils/report'
import type { ReportFormat, ReportInfo } from '@/types'

// 报告详情页（PRD §3.4.2）：摘要 + HTML/PDF 在线预览 + 达标明细 + 产物导出。
// HTML/PDF 通过鉴权接口取 Blob 后由 iframe 渲染（不暴露文件系统 URL）。
const route = useRoute()
const store = useReportStore()
const auth = useAuthStore()

const errorMessage = ref('')
const acting = ref(false)
const activeTab = ref<'html' | 'pdf' | 'evaluation' | 'artifacts'>('html')

const reportId = computed(() => String(route.params.reportId ?? ''))
const report = computed(() => store.currentReport)
const canWrite = computed(() => auth.hasPerm('report.list', 'write'))

const formats = computed<ReportFormat[]>(() => artifactFormats(report.value))
const canRegenerate = computed(
  () => canWrite.value && Boolean(report.value?.task_id) && report.value?.status !== 'generating',
)

const passRateText = computed(() => {
  const summary = report.value?.summary
  if (!summary || summary.total_interfaces <= 0) return '-'
  return `${summary.pass_rate}%（${summary.passed_interfaces}/${summary.total_interfaces} 接口）`
})

const durationText = computed(() => {
  const seconds = report.value?.summary?.duration_seconds ?? 0
  if (!seconds || seconds <= 0) return '-'
  if (seconds < 60) return `${seconds.toFixed(1)} 秒`
  const total = Math.round(seconds)
  return `${Math.floor(total / 60)} 分 ${String(total % 60).padStart(2, '0')} 秒`
})

const tabs = computed(() => [
  { value: 'html' as const, label: 'HTML 预览', disabled: !hasArtifact(report.value, 'html') },
  { value: 'pdf' as const, label: 'PDF 预览', disabled: !hasArtifact(report.value, 'pdf') },
  { value: 'evaluation' as const, label: '达标明细', disabled: false },
  { value: 'artifacts' as const, label: '产物信息', disabled: false },
])

/** 默认选中首个可预览的 Tab。 */
function pickDefaultTab(current: ReportInfo): void {
  if (current.status === 'generating') {
    activeTab.value = 'artifacts'
    return
  }
  if (hasArtifact(current, 'html')) {
    activeTab.value = 'html'
  } else if (hasArtifact(current, 'pdf')) {
    activeTab.value = 'pdf'
  } else if (current.summary?.items?.length) {
    activeTab.value = 'evaluation'
  } else {
    activeTab.value = 'artifacts'
  }
}

// ===================== 轮询（生成中自动刷新） =====================

let pollTimer: number | null = null

function stopPolling(): void {
  if (pollTimer !== null) {
    window.clearInterval(pollTimer)
    pollTimer = null
  }
}

function ensurePolling(): void {
  if (report.value?.status === 'generating' && pollTimer === null) {
    pollTimer = window.setInterval(() => {
      const before = report.value?.status
      void refresh(true).then(() => {
        // 生成完成：自动切回可预览的 Tab（无需用户手动刷新）。
        if (before === 'generating' && report.value && report.value.status !== 'generating') {
          pickDefaultTab(report.value)
        }
      })
    }, 3000)
  } else if (report.value?.status !== 'generating') {
    stopPolling()
  }
}

async function refresh(silent = false): Promise<void> {
  if (!reportId.value) return
  try {
    const loaded = await store.fetchReport(reportId.value, silent)
    errorMessage.value = ''
    if (!silent) pickDefaultTab(loaded)
    ensurePolling()
  } catch (err) {
    errorMessage.value = describeReportError(err, '加载报告详情失败')
  }
}

// ===================== 操作 =====================

async function onExport(format: ReportFormat): Promise<void> {
  const current = report.value
  if (!current || acting.value) return
  acting.value = true
  errorMessage.value = ''
  const extension = format === 'word' ? 'docx' : format
  const name = `${(current.title || 'report').replace(/[\\/:*?"<>|]/g, '_').slice(0, 120)}.${extension}`
  try {
    await store.exportFile(current.id, format, name)
  } catch (err) {
    errorMessage.value = describeReportError(err, '导出报告失败')
  } finally {
    acting.value = false
  }
}

async function onRegenerate(): Promise<void> {
  const current = report.value
  if (!current || acting.value) return
  if (!window.confirm(`确认重新生成报告「${current.title}」？`)) return

  acting.value = true
  errorMessage.value = ''
  try {
    await store.regenerate(current.id)
    await refresh(true)
    pickDefaultTab(current)
  } catch (err) {
    errorMessage.value = describeReportError(err, '重新生成报告失败')
    await refresh(true)
  } finally {
    acting.value = false
  }
}

watch(
  () => report.value?.status,
  () => {
    ensurePolling()
  },
)

onMounted(async () => {
  if (!reportId.value) {
    errorMessage.value = '缺少报告 ID，请从报告列表进入详情页'
    return
  }
  if (store.currentReport && store.currentReport.id !== reportId.value) {
    store.currentReport = null
  }
  await refresh()
})

onUnmounted(stopPolling)
</script>

<style scoped>
.title-block {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.report-name {
  font-size: 16px;
  font-weight: 500;
  color: #4b5563;
}

.title-links {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 12px;
}

.summary {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(190px, 1fr));
  gap: 12px;
}

.summary-item {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: #1f2933;
  min-width: 0;
}

.summary-label {
  color: #6b7280;
  font-size: 12px;
  white-space: nowrap;
}

.tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  border-bottom: 1px solid #e5e7eb;
  padding-bottom: 8px;
  margin-bottom: 12px;
}

.tab {
  padding: 6px 14px;
  border: 1px solid transparent;
  border-radius: 6px;
  background: none;
  font-size: 13px;
  color: #4b5563;
  cursor: pointer;
}

.tab:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}

.tab.active {
  background: #eff6ff;
  border-color: #bfdbfe;
  color: var(--ap-primary);
  font-weight: 600;
}

.tab-body {
  min-height: 120px;
}

.artifact-panel {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.import-meta {
  padding: 12px 14px;
  border-radius: 6px;
  background: #f9fafb;
  font-size: 13px;
}

.import-meta h4 {
  margin: 0 0 8px;
  font-size: 13px;
  color: #1f2933;
}

.meta-line {
  display: flex;
  gap: 10px;
  line-height: 1.8;
  word-break: break-all;
}

.sha {
  font-size: 12px;
  color: #4b5563;
}
</style>
