<template>
  <div class="ap-card">
    <table class="ap-table">
      <thead>
        <tr>
          <th>时间</th>
          <th>任务</th>
          <th>方法</th>
          <th>URL</th>
          <th>状态</th>
          <th>错误码</th>
          <th>耗时</th>
          <th>明细</th>
        </tr>
      </thead>
      <tbody>
        <template v-for="log in logs" :key="log.id">
          <tr>
            <td class="time-col">{{ formatDateTime(log.occurred_at) }}</td>
            <td>
              <RouterLink
                v-if="log.task_id"
                class="ap-link"
                :to="{ name: 'task-detail', params: { taskId: log.task_id } }"
              >
                #{{ log.task_id }}
              </RouterLink>
              <span v-else class="ap-muted">-</span>
              <div v-if="log.interface_id" class="ap-muted log-meta">接口 #{{ log.interface_id }}</div>
            </td>
            <td>
              <span class="ap-badge" :class="httpMethodBadge(log.method).badge">
                {{ httpMethodBadge(log.method).label }}
              </span>
            </td>
            <td class="url-col mono">{{ log.url || '-' }}</td>
            <td>
              <span class="ap-badge" :class="httpStatusBadge(log.response_status).badge">
                {{ httpStatusBadge(log.response_status).label }}
              </span>
            </td>
            <td class="mono">
              <span v-if="log.error_code" class="error-code">{{ log.error_code }}</span>
              <span v-else class="ap-muted">-</span>
            </td>
            <td class="mono">{{ formatDurationMs(log.duration_ms) }}</td>
            <td>
              <button type="button" class="ap-btn ap-btn-ghost ap-btn-sm" @click="toggle(log.id)">
                {{ expanded.has(log.id) ? '收起' : '查看' }}
              </button>
            </td>
          </tr>
          <tr v-if="expanded.has(log.id)" class="detail-row">
            <td colspan="8">
              <div class="detail-grid">
                <div>
                  <div class="ap-field-label">请求入参（已脱敏，≤4KB）</div>
                  <pre class="payload">{{ log.request_body || '（无请求体）' }}</pre>
                </div>
                <div>
                  <div class="ap-field-label">响应内容（已脱敏，≤4KB）</div>
                  <pre class="payload">{{ log.response_body || '（引擎未采集响应体）' }}</pre>
                </div>
                <div class="span-2">
                  <div class="ap-field-label">错误信息</div>
                  <pre class="payload">{{ log.error_message || '-' }}</pre>
                </div>
              </div>
            </td>
          </tr>
        </template>
      </tbody>
    </table>
    <p v-if="loading" class="ap-table-empty">加载中…</p>
    <p v-else-if="logs.length === 0" class="ap-table-empty">
      暂无请求详情：任务执行完成后按日志设置采样写入，可在「日志设置」调整启用类型与保留策略
    </p>
  </div>
</template>

<script setup lang="ts">
import { reactive } from 'vue'
import { RouterLink } from 'vue-router'
import { formatDateTime } from '@/utils/format'
import { formatDurationMs, httpMethodBadge, httpStatusBadge } from '@/utils/log'
import type { RequestLogInfo } from '@/types'

// 请求详情表格（逐请求明细：时间/任务/方法/URL/状态/错误码/耗时 + 可展开入参与响应摘要）。
defineProps<{
  logs: RequestLogInfo[]
  loading: boolean
}>()

// expanded 记录已展开的明细行（按日志 ID），避免一次渲染大量请求体。
const expanded = reactive(new Set<string>())

function toggle(id: string): void {
  if (expanded.has(id)) expanded.delete(id)
  else expanded.add(id)
}
</script>

<style scoped>
.time-col {
  white-space: nowrap;
  font-size: 12px;
  color: #4b5563;
}

.mono {
  font-family: Consolas, Monaco, monospace;
  font-size: 12px;
}

.url-col {
  max-width: 420px;
  word-break: break-all;
  color: #374151;
}

.log-meta {
  font-size: 11px;
}

.error-code {
  color: #b91c1c;
}

.detail-row td {
  background: #f9fafb;
}

.detail-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.detail-grid .span-2 {
  grid-column: span 2;
}

.payload {
  margin: 2px 0 0;
  padding: 8px;
  max-height: 220px;
  overflow: auto;
  background: #fff;
  border: 1px solid #e5e7eb;
  border-radius: 6px;
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
