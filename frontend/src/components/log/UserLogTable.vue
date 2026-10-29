<template>
  <div class="ap-card">
    <table class="ap-table">
      <thead>
        <tr>
          <th>时间</th>
          <th>操作人</th>
          <th>动作</th>
          <th>关联对象</th>
          <th>结果</th>
          <th>IP</th>
          <th>详情</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="log in logs" :key="log.id">
          <td class="time-col">{{ formatDateTime(log.created_at) }}</td>
          <td>
            <span v-if="log.admin_username">{{ log.admin_username }}</span>
            <span v-else class="ap-muted">{{ log.admin_id ? `#${log.admin_id}` : '匿名' }}</span>
            <div class="ap-muted log-meta">#{{ log.id }}</div>
          </td>
          <td>{{ auditActionLabel(log.action) }}</td>
          <td>
            <template v-if="log.resource_type">
              <span>{{ auditResourceLabel(log.resource_type) }}</span>
              <span v-if="log.resource_id" class="ap-muted"> #{{ log.resource_id }}</span>
            </template>
            <span v-else class="ap-muted">-</span>
          </td>
          <td>
            <span class="ap-badge" :class="auditResultMeta(log.result).badge">
              {{ auditResultMeta(log.result).label }}
            </span>
            <span class="ap-badge level-badge" :class="logLevelMeta(log.level).badge">
              {{ logLevelMeta(log.level).label }}
            </span>
          </td>
          <td class="mono">{{ log.ip || '-' }}</td>
          <td class="detail-col">{{ log.detail || '-' }}</td>
        </tr>
      </tbody>
    </table>
    <p v-if="loading" class="ap-table-empty">加载中…</p>
    <p v-else-if="logs.length === 0" class="ap-table-empty">
      暂无用户日志：登录、增删改、执行/停止任务等操作会自动记入审计链
    </p>
  </div>
</template>

<script setup lang="ts">
import { formatDateTime } from '@/utils/format'
import { auditActionLabel, auditResourceLabel, auditResultMeta, logLevelMeta } from '@/utils/log'
import type { UserLogInfo } from '@/types'

// 用户日志表格（5W1H 只读展示；无删除/修改入口，DES-T-006）。
defineProps<{
  logs: UserLogInfo[]
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

.level-badge {
  margin-left: 6px;
}

.mono {
  font-family: Consolas, Monaco, monospace;
  font-size: 12px;
}

.detail-col {
  max-width: 420px;
  font-size: 12px;
  color: #4b5563;
  word-break: break-all;
}
</style>
