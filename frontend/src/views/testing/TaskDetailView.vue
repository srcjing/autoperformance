<template>
  <div class="ap-page">
    <div class="ap-page-header">
      <div class="title-block">
        <h2 class="ap-page-title">
          任务详情
          <span v-if="task" class="task-name">· {{ task.name }}</span>
        </h2>
        <div class="title-links">
          <RouterLink to="/testing/tasks" class="ap-link">返回任务列表</RouterLink>
          <span class="ap-muted">#{{ taskId }}</span>
        </div>
      </div>

      <div v-if="task" class="ap-actions">
        <button
          v-if="canWrite && canStartTask(task.status)"
          type="button"
          class="ap-btn ap-btn-primary"
          :disabled="acting"
          @click="onStart"
        >
          开始执行
        </button>
        <button
          v-if="canWrite && canCancelTask(task.status)"
          type="button"
          class="ap-btn ap-btn-danger"
          :disabled="acting"
          @click="onCancel"
        >
          停止任务
        </button>
        <button
          v-if="canWrite && canRetryTask(task.status)"
          type="button"
          class="ap-btn ap-btn-ghost"
          :disabled="acting"
          @click="onRetry"
        >
          重试
        </button>
        <button
          v-if="canWrite && canCleanupTask(task.status)"
          type="button"
          class="ap-btn ap-btn-danger"
          :disabled="acting"
          @click="onCleanup"
        >
          确认清理
        </button>
        <button
          type="button"
          class="ap-btn ap-btn-ghost"
          :disabled="store.taskLoading"
          @click="refresh()"
        >
          刷新
        </button>
      </div>
    </div>

    <p v-if="errorMessage" class="ap-alert ap-alert-error" role="alert">{{ errorMessage }}</p>
    <p v-if="store.taskLoading && !task" class="ap-card ap-table-empty">加载中…</p>

    <template v-if="task">
      <div class="ap-card summary">
        <div class="summary-item">
          <span class="summary-label">状态</span>
          <span class="ap-badge" :class="statusMeta.badge">{{ statusMeta.label }}</span>
        </div>
        <div class="summary-item">
          <span class="summary-label">环境</span>
          <span>{{ envNames[task.environment_id] ?? `#${task.environment_id}` }}</span>
        </div>
        <div class="summary-item">
          <span class="summary-label">目标 Host</span>
          <span>{{ task.target_host || '-' }}</span>
        </div>
        <div class="summary-item">
          <span class="summary-label">接口范围</span>
          <span>{{ task.interface_ids.length }} 个</span>
        </div>
        <div class="summary-item">
          <span class="summary-label">并发</span>
          <span>{{ concurrencyText }}</span>
        </div>
        <div class="summary-item">
          <span class="summary-label">来源模板</span>
          <span>{{ task.template_id ? `#${task.template_id}` : '未使用' }}</span>
        </div>
        <div class="summary-item">
          <span class="summary-label">创建时间</span>
          <span>{{ formatDateTime(task.created_at ?? '') }}</span>
        </div>
        <div class="summary-item">
          <span class="summary-label">开始时间</span>
          <span>{{ formatDateTime(task.started_at ?? '') }}</span>
        </div>
        <div class="summary-item">
          <span class="summary-label">结束时间</span>
          <span>{{ formatDateTime(task.finished_at ?? '') }}</span>
        </div>
        <div class="summary-item">
          <span class="summary-label">实时通道</span>
          <span class="ap-badge" :class="store.realtimeConnected ? 'ap-badge-success' : 'ap-badge-muted'">
            {{ store.realtimeConnected ? '已连接' : '未连接（轮询兜底）' }}
          </span>
        </div>
      </div>

      <div v-if="showCleanupAlert" class="ap-alert cleanup-alert">
        <div>
          <strong>{{ cleanupAlertTitle }}：</strong>任务失败/取消后已保留现场（环境未自动清理）。
          确认后将按清理配置执行（{{ cleanSummary }}），成功后任务结束。
        </div>
        <button
          v-if="canWrite && canCleanupTask(task.status)"
          type="button"
          class="ap-btn ap-btn-danger"
          :disabled="acting"
          @click="onCleanup"
        >
          确认清理
        </button>
      </div>

      <p v-if="store.realtimeError && !store.realtimeConnected" class="ap-alert ap-alert-hint">
        实时通道提示：{{ store.realtimeError }}
      </p>

      <TaskSteps :status="task.status" :has-result="Boolean(task.result)" />

      <div class="realtime-grid">
        <RealtimeLog
          :logs="store.liveLogs"
          :connected="store.realtimeConnected"
          :hint="logHint"
          @clear="store.clearLogs"
        />
        <MetricPanel
          :metrics="metrics"
          :evaluation="evaluation"
          :duration-seconds="durationSeconds"
        />
      </div>

      <div class="ap-card">
        <h3 class="cfg-title">报告与清理配置</h3>
        <div class="config-grid">
          <div class="summary-item">
            <span class="summary-label">报告格式</span>
            <span>{{ reportFormats }}</span>
          </div>
          <div class="summary-item">
            <span class="summary-label">清理动作</span>
            <span>{{ cleanSummary }}</span>
          </div>
          <div class="summary-item">
            <span class="summary-label">清理时机</span>
            <span>{{ task.clean_config?.timing === 'manual' ? '人工确认' : '自动（成功即执行）' }}</span>
          </div>
        </div>
      </div>

      <div class="ap-card">
        <h3 class="cfg-title">接口范围快照（创建时固化，INV-5）</h3>
        <table v-if="task.interface_range_snapshot.length > 0" class="ap-table">
          <thead>
            <tr>
              <th>接口</th>
              <th>产品</th>
              <th>方法</th>
              <th>URL</th>
              <th>认证头（仅名称）</th>
              <th>状态</th>
              <th>快照时间</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="snapshot in task.interface_range_snapshot" :key="snapshot.interface_id">
              <td>{{ snapshot.name }}</td>
              <td>{{ snapshot.product_name || `#${snapshot.product_id}` }}</td>
              <td><span class="ap-badge">{{ snapshot.method }}</span></td>
              <td class="url">{{ snapshot.url || '-' }}</td>
              <td>
                <span v-if="snapshot.auth_headers.length === 0" class="ap-muted">无</span>
                <span v-else class="ap-chips">
                  <span
                    v-for="header in snapshot.auth_headers"
                    :key="header"
                    class="ap-chip ap-chip-static"
                  >
                    {{ header }}（已加密）
                  </span>
                </span>
              </td>
              <td>{{ snapshot.status }}</td>
              <td>{{ formatDateTime(snapshot.captured_at) }}</td>
            </tr>
          </tbody>
        </table>
        <p v-else class="ap-table-empty">暂无接口快照</p>
      </div>

      <div class="ap-card">
        <h3 class="cfg-title">阈值快照（评估口径，INV-2）</h3>
        <table v-if="task.threshold_snapshot.length > 0" class="ap-table">
          <thead>
            <tr>
              <th>接口</th>
              <th>指标</th>
              <th>条件</th>
              <th>阈值</th>
              <th>单位</th>
              <th>启用</th>
            </tr>
          </thead>
          <tbody>
            <tr
              v-for="(item, index) in task.threshold_snapshot"
              :key="`${item.interface_id}-${item.metric}-${index}`"
            >
              <td>{{ item.interface_name || `接口 #${item.interface_id}` }}</td>
              <td>{{ metricLabel(item.metric) }}</td>
              <td>{{ item.operator === 'ge' ? '≥' : '≤' }}</td>
              <td>{{ item.value }}</td>
              <td>{{ item.unit || '-' }}</td>
              <td>
                <span class="ap-badge" :class="item.enabled ? 'ap-badge-success' : 'ap-badge-muted'">
                  {{ item.enabled ? '启用' : '停用' }}
                </span>
              </td>
            </tr>
          </tbody>
        </table>
        <p v-else class="ap-table-empty">
          接口未配置阈值：评估结论为「未配置阈值」，不计入达标率分母
        </p>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import MetricPanel from '@/components/testing/MetricPanel.vue'
import RealtimeLog from '@/components/testing/RealtimeLog.vue'
import TaskSteps from '@/components/testing/TaskSteps.vue'
import { useEnvironmentOptions } from '@/composables/useEnvironmentOptions'
import { useAuthStore } from '@/stores/auth'
import { useTaskStore, type TaskAction } from '@/stores/task'
import { formatDateTime } from '@/utils/format'
import { metricLabel } from '@/utils/threshold'
import {
  CLEAN_ACTION_OPTIONS,
  canCancelTask,
  canCleanupTask,
  canRetryTask,
  canStartTask,
  describeTaskError,
  isActiveTaskStatus,
  taskStatusMeta,
} from '@/utils/testing'
import type { TaskEvaluation } from '@/types'

// 任务详情页（PRD §3.3 任务执行/监控）：阶段进度 + 实时日志 + 指标判定 + 快照回看。
// 实时数据由 useTaskStore.attachRealtime（内部 useWebSocket → task:{id}）写入；
// WebSocket 不可用时降级为 8 秒状态轮询（仅刷新任务详情，不伪造实时日志）。
const route = useRoute()
const store = useTaskStore()
const auth = useAuthStore()
const { names: envNames, load: loadEnvOptions } = useEnvironmentOptions()

const errorMessage = ref('')
const acting = ref(false)

const taskId = computed(() => String(route.params.taskId ?? ''))
const task = computed(() => store.currentTask)
const canWrite = computed(() => auth.hasPerm('testing.task', 'write'))
const statusMeta = computed(() => taskStatusMeta(task.value?.status ?? ''))

const metrics = computed<Record<string, number> | null>(
  () => store.liveMetrics ?? task.value?.result?.metrics ?? null,
)
const evaluation = computed<TaskEvaluation | null>(
  () => store.liveEvaluation ?? task.value?.result?.evaluation ?? null,
)
const durationSeconds = computed(() => task.value?.result?.duration_seconds ?? 0)

const concurrencyText = computed(() => {
  const flow = task.value?.flow
  if (!flow) return '-'
  if (flow.concurrency_mode === 'ramp') {
    const peak = (flow.ramp_stages ?? []).reduce(
      (max, stage) => Math.max(max, stage.vus ?? 0),
      0,
    )
    return peak > 0 ? `阶梯 ↑${peak} VU` : '-'
  }
  return flow.target_vus ? `${flow.target_vus} VU` : '-'
})

const reportFormats = computed(() => {
  const formats = task.value?.report_config?.formats ?? []
  return formats.length > 0 ? formats.map((format) => format.toUpperCase()).join(' / ') : '全量（默认）'
})

const cleanSummary = computed(() => {
  const config = task.value?.clean_config
  if (!config) return '-'
  const labels = config.actions
    .filter((action) => action !== 'script')
    .map((action) => CLEAN_ACTION_OPTIONS.find((item) => item.value === action)?.label ?? action)
  if (config.scripts.length > 0) labels.push(`脚本 ×${config.scripts.length}`)
  return labels.join('、') || '-'
})

/** 失败/取消/待确认时展示「保留现场」提示（确认清理按钮仅 cleanup_pending 可用）。 */
const showCleanupAlert = computed(() => {
  const status = task.value?.status
  return status === 'cleanup_pending' || status === 'failed' || status === 'cancelled'
})

const cleanupAlertTitle = computed(() =>
  task.value?.status === 'cleanup_pending' ? '清理待确认' : '已保留现场',
)

const logHint = computed(() =>
  store.realtimeConnected
    ? '暂无日志：任务开始执行后将实时推送（服务端已脱敏）'
    : '实时通道未连接（已降级为状态轮询）：检查登录状态/网络后刷新页面',
)

// ===================== 状态轮询兜底 =====================

let pollTimer: number | null = null

function stopPolling(): void {
  if (pollTimer !== null) {
    window.clearInterval(pollTimer)
    pollTimer = null
  }
}

/** 实时通道不可用且任务仍在推进时，每 8 秒轮询一次任务详情。 */
function ensurePolling(): void {
  const current = task.value
  if (!current) {
    stopPolling()
    return
  }
  const shouldPoll =
    (isActiveTaskStatus(current.status) || current.status === 'cleanup_pending') &&
    !store.realtimeConnected
  if (!shouldPoll) {
    stopPolling()
    return
  }
  if (pollTimer === null) {
    pollTimer = window.setInterval(() => {
      void refresh(true)
    }, 8000)
  }
}

async function refresh(silent = false): Promise<void> {
  if (!taskId.value) return
  try {
    await store.fetchTask(taskId.value, silent)
    errorMessage.value = ''
  } catch (err) {
    errorMessage.value = describeTaskError(err, '加载任务详情失败')
  } finally {
    ensurePolling()
  }
}

// ===================== 操作 =====================

async function runAction(action: TaskAction, confirmText: string): Promise<void> {
  const current = task.value
  if (!current || acting.value) return
  if (!window.confirm(confirmText)) return

  acting.value = true
  errorMessage.value = ''
  try {
    await store.runTaskAction(current.id, action)
    if (action === 'start' || action === 'retry') store.resetLive()
    await refresh(true)
  } catch (err) {
    errorMessage.value = describeTaskError(err, '任务操作失败')
  } finally {
    acting.value = false
  }
}

function onStart(): void {
  void runAction('start', `确认开始任务「${task.value?.name ?? ''}」？将占用环境并拉起压测。`)
}

function onCancel(): void {
  void runAction(
    'cancel',
    `确认停止任务「${task.value?.name ?? ''}」？k6 子进程将被终止，环境保留待确认清理。`,
  )
}

function onRetry(): void {
  void runAction('retry', '确认重试？将复用原接口/阈值快照重新执行。')
}

function onCleanup(): void {
  void runAction('cleanup', '确认执行环境清理？清理成功后任务结束。')
}

watch(
  () => task.value?.status,
  () => {
    ensurePolling()
  },
)

watch(
  () => store.realtimeConnected,
  () => {
    ensurePolling()
  },
)

onMounted(async () => {
  await loadEnvOptions()
  if (!taskId.value) {
    errorMessage.value = '缺少任务 ID，请从任务列表进入详情页'
    return
  }
  // 从其他任务详情跳转时先清掉残留详情，避免短暂展示错误数据。
  if (store.currentTask && store.currentTask.id !== taskId.value) {
    store.currentTask = null
  }
  await refresh()
  store.attachRealtime(taskId.value)
  ensurePolling()
})

onUnmounted(() => {
  stopPolling()
  store.detachRealtime()
})
</script>

<style scoped>
.title-block {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.task-name {
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

.cleanup-alert {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
  background: #fef2f2;
  color: #b91c1c;
  border: 1px solid #fecaca;
}

.realtime-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.15fr) minmax(0, 1fr);
  gap: 16px;
  align-items: start;
}

@media (max-width: 1280px) {
  .realtime-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}

.cfg-title {
  margin: 0 0 10px;
  font-size: 14px;
  color: #1f2933;
}

.config-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
  gap: 10px;
}

.url {
  max-width: 320px;
  font-size: 12px;
  color: #4b5563;
  word-break: break-all;
}
</style>
