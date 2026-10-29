<template>
  <div class="ap-card">
    <table class="ap-table">
      <thead>
        <tr>
          <th>时间</th>
          <th>任务</th>
          <th>序号</th>
          <th>级别</th>
          <th>内容</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="log in logs" :key="log.id">
          <td class="time-col">{{ formatDateTime(log.created_at) }}</td>
          <td>
            <RouterLink
              v-if="log.task_id"
              class="ap-link"
              :to="{ name: 'task-detail', params: { taskId: log.task_id } }"
            >
              #{{ log.task_id }}
            </RouterLink>
            <span v-else class="ap-muted">-</span>
          </td>
          <td class="mono">{{ log.seq }}</td>
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
      暂无测试日志：任务执行过程中由编排器按 seq 增量写入
    </p>
  </div>
</template>

<script setup lang="ts">
import { RouterLink } from 'vue-router'
import { formatDateTime } from '@/utils/format'
import { logLevelMeta } from '@/utils/log'
import type { TestLogInfo } from '@/types'

// 测试日志表格（按任务定位 + 增量 seq + 级别着色，只读）。
defineProps<{
  logs: TestLogInfo[]
  loading: boolean
}>()
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

.detail-col {
  font-size: 12px;
  color: #4b5563;
  word-break: break-all;
}
</style>
