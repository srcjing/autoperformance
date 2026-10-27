<template>
  <div class="ap-card">
    <table class="ap-table">
      <thead>
        <tr>
          <th>任务名称</th>
          <th>环境</th>
          <th>状态</th>
          <th>接口</th>
          <th>并发</th>
          <th>创建时间</th>
          <th>开始 / 结束</th>
          <th>操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="task in tasks" :key="task.id">
          <td>
            <RouterLink
              class="ap-link task-name"
              :to="{ name: 'task-detail', params: { taskId: task.id } }"
            >
              {{ task.name }}
            </RouterLink>
            <div class="ap-muted task-meta">
              #{{ task.id }}<template v-if="task.template_id"> · 模板 {{ task.template_id }}</template>
            </div>
          </td>
          <td>{{ envName(task.environment_id) }}</td>
          <td>
            <span class="ap-badge" :class="taskStatusMeta(task.status).badge">
              {{ taskStatusMeta(task.status).label }}
            </span>
          </td>
          <td>{{ task.interface_ids.length }}</td>
          <td>{{ concurrencyText(task) }}</td>
          <td>{{ formatDateTime(task.created_at ?? '') }}</td>
          <td class="time-col">
            <div>开始 {{ formatDateTime(task.started_at ?? '') }}</div>
            <div>结束 {{ formatDateTime(task.finished_at ?? '') }}</div>
          </td>
          <td>
            <div class="ap-actions">
              <button
                v-if="canWrite && canStartTask(task.status)"
                type="button"
                class="ap-btn ap-btn-primary ap-btn-sm"
                @click="emit('start', task)"
              >
                开始
              </button>
              <button
                v-if="canWrite && canCancelTask(task.status)"
                type="button"
                class="ap-btn ap-btn-danger ap-btn-sm"
                @click="emit('cancel', task)"
              >
                停止
              </button>
              <button
                v-if="canWrite && canRetryTask(task.status)"
                type="button"
                class="ap-btn ap-btn-ghost ap-btn-sm"
                @click="emit('retry', task)"
              >
                重试
              </button>
              <button
                v-if="canWrite && canCleanupTask(task.status)"
                type="button"
                class="ap-btn ap-btn-danger ap-btn-sm"
                @click="emit('cleanup', task)"
              >
                确认清理
              </button>
              <RouterLink
                class="ap-btn ap-btn-ghost ap-btn-sm"
                :to="{ name: 'task-detail', params: { taskId: task.id } }"
              >
                详情
              </RouterLink>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-if="!loading && tasks.length === 0" class="ap-table-empty">
      暂无测试任务，点击「新建任务」开始编排
    </p>
    <p v-if="loading" class="ap-table-empty">加载中…</p>
  </div>
</template>

<script setup lang="ts">
import { RouterLink } from 'vue-router'
import { formatDateTime } from '@/utils/format'
import {
  canCancelTask,
  canCleanupTask,
  canRetryTask,
  canStartTask,
  taskStatusMeta,
} from '@/utils/testing'
import type { TestTaskInfo } from '@/types'

// 任务表格（状态驱动的操作显隐：开始/停止/重试/确认清理 + 详情）。
const props = defineProps<{
  tasks: TestTaskInfo[]
  loading: boolean
  canWrite: boolean
  /** 环境 ID → 名称（无 env.list:read 权限时为空，回退展示 ID）。 */
  envNames: Record<string, string>
}>()

const emit = defineEmits<{
  (e: 'start', task: TestTaskInfo): void
  (e: 'cancel', task: TestTaskInfo): void
  (e: 'retry', task: TestTaskInfo): void
  (e: 'cleanup', task: TestTaskInfo): void
}>()

function envName(id: string): string {
  return props.envNames[id] ?? `#${id}`
}

/** 并发摘要（固定/脉冲取 target_vus；阶梯取峰值档位）。 */
function concurrencyText(task: TestTaskInfo): string {
  const flow = task.flow
  if (!flow) return '-'
  if (flow.concurrency_mode === 'ramp') {
    const peak = (flow.ramp_stages ?? []).reduce(
      (max, stage) => Math.max(max, stage.vus ?? 0),
      0,
    )
    return peak > 0 ? `阶梯 ↑${peak}` : '-'
  }
  return flow.target_vus ? `${flow.target_vus} VU` : '-'
}
</script>

<style scoped>
.task-name {
  font-weight: 600;
}

.task-meta {
  font-size: 12px;
}

.time-col {
  font-size: 12px;
  color: #4b5563;
  white-space: nowrap;
}
</style>
