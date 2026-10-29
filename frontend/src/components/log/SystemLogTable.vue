<template>
  <div class="ap-card">
    <table class="ap-table">
      <thead>
        <tr>
          <th>时间</th>
          <th>事件</th>
          <th>级别</th>
          <th>内容</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="log in logs" :key="log.id">
          <td class="time-col">{{ formatDateTime(log.created_at) }}</td>
          <td>
            <span class="ap-badge" :class="systemEventMeta(log.event).badge">
              {{ systemEventMeta(log.event).label }}
            </span>
            <div class="ap-muted log-meta">#{{ log.id }}</div>
          </td>
          <td>
            <span class="ap-badge" :class="logLevelMeta(log.level).badge">
              {{ logLevelMeta(log.level).label }}
            </span>
          </td>
          <td class="detail-col">{{ log.message || '-' }}</td>
        </tr>
      </tbody>
    </table>
    <p v-if="loading" class="ap-table-empty">加载中…</p>
    <p v-else-if="logs.length === 0" class="ap-table-empty">
      暂无系统日志：服务启动/重启/停止时自动记录运行状态
    </p>
  </div>
</template>

<script setup lang="ts">
import { formatDateTime } from '@/utils/format'
import { logLevelMeta, systemEventMeta } from '@/utils/log'
import type { SystemLogInfo } from '@/types'

// 系统日志表格（服务运行状态只读展示）。
defineProps<{
  logs: SystemLogInfo[]
  loading: boolean
}>()
</script>

<style scoped>
.time-col {
  white-space: nowrap;
  font-size: 12px;
  color: #4b5563;
}

.log-meta {
  font-size: 12px;
}

.detail-col {
  font-size: 12px;
  color: #4b5563;
  word-break: break-all;
}
</style>
