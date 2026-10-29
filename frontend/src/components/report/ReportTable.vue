<template>
  <div class="ap-card">
    <table class="ap-table">
      <thead>
        <tr>
          <th>报告标题</th>
          <th>格式</th>
          <th>状态</th>
          <th>接口达标率</th>
          <th>关联任务</th>
          <th>生成时间</th>
          <th>操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="report in reports" :key="report.id">
          <td>
            <RouterLink
              class="ap-link report-title"
              :to="{ name: 'report-detail', params: { reportId: report.id } }"
            >
              {{ report.title }}
            </RouterLink>
            <div class="ap-muted report-meta">
              #{{ report.id }} · {{ reportSourceLabel(report.source) }}
            </div>
          </td>
          <td>
            <span v-if="report.artifacts.length === 0" class="ap-muted">-</span>
            <span v-else class="ap-chips">
              <span
                v-for="format in formatsOf(report)"
                :key="format"
                class="ap-chip ap-chip-static"
              >
                {{ reportFormatLabel(format) }}
              </span>
            </span>
          </td>
          <td>
            <span class="ap-badge" :class="reportStatusMeta(report.status).badge">
              {{ reportStatusMeta(report.status).label }}
            </span>
          </td>
          <td>
            <template v-if="report.summary && report.summary.total_interfaces > 0">
              <span
                class="ap-badge"
                :class="report.summary.pass ? 'ap-badge-success' : 'ap-badge-danger'"
              >
                {{ report.summary.pass_rate }}%
              </span>
              <span class="ap-muted rate-hint">
                {{ report.summary.passed_interfaces }}/{{ report.summary.total_interfaces }} 接口
              </span>
            </template>
            <span v-else-if="report.source === 'external'" class="ap-badge ap-badge-info">
              外部导入
            </span>
            <span v-else class="ap-muted">未配置阈值</span>
          </td>
          <td>
            <RouterLink
              v-if="report.task_id"
              class="ap-link"
              :to="{ name: 'task-detail', params: { taskId: report.task_id } }"
            >
              #{{ report.task_id }}
            </RouterLink>
            <span v-else class="ap-muted">无（孤立归档）</span>
          </td>
          <td>{{ formatDateTime(report.created_at ?? '') }}</td>
          <td>
            <div class="ap-actions">
              <RouterLink
                class="ap-btn ap-btn-ghost ap-btn-sm"
                :to="{ name: 'report-detail', params: { reportId: report.id } }"
              >
                查看
              </RouterLink>
              <button
                v-for="format in formatsOf(report)"
                :key="format"
                type="button"
                class="ap-btn ap-btn-ghost ap-btn-sm"
                @click="emit('export', report, format)"
              >
                导出 {{ reportFormatLabel(format) }}
              </button>
              <button
                v-if="canWrite && report.status === 'generation_failed'"
                type="button"
                class="ap-btn ap-btn-primary ap-btn-sm"
                @click="emit('regenerate', report)"
              >
                重新生成
              </button>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-if="loading" class="ap-table-empty">加载中…</p>
    <p v-else-if="reports.length === 0" class="ap-table-empty">
      暂无报告：任务执行完成后自动生成，或点击「导入报告」归档外部报告
    </p>
  </div>
</template>

<script setup lang="ts">
import { RouterLink } from 'vue-router'
import { formatDateTime } from '@/utils/format'
import {
  artifactFormats,
  reportFormatLabel,
  reportSourceLabel,
  reportStatusMeta,
} from '@/utils/report'
import type { ReportFormat, ReportInfo } from '@/types'

// 报告表格（查看/导出/重新生成；生成中不可导出）。
defineProps<{
  reports: ReportInfo[]
  loading: boolean
  canWrite: boolean
}>()

const emit = defineEmits<{
  (e: 'export', report: ReportInfo, format: ReportFormat): void
  (e: 'regenerate', report: ReportInfo): void
}>()

/** 已产出格式（未生成完成的报告可能只有部分格式）。 */
function formatsOf(report: ReportInfo): ReportFormat[] {
  return artifactFormats(report)
}
</script>

<style scoped>
.report-title {
  font-weight: 600;
}

.report-meta {
  font-size: 12px;
}

.rate-hint {
  margin-left: 6px;
  font-size: 12px;
}
</style>
