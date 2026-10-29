import { defineStore } from 'pinia'
import { ref, watch, type WatchStopHandle } from 'vue'
import {
  cancelTask,
  cleanupTask,
  createTask,
  createTemplate,
  deleteTemplate,
  getTask,
  listTasks,
  listTemplates,
  retryTask,
  startTask,
  updateTemplate,
} from '@/api/testing'
import { createWebSocket, type WebSocketClient } from '@/composables/useWebSocket'
import type {
  CreateTaskPayload,
  TaskEvaluation,
  TaskLogEvent,
  TaskMetricsEvent,
  TaskQuery,
  TaskStatus,
  TaskStatusEvent,
  TemplateQuery,
  TestTaskInfo,
  TestTemplateInfo,
  TestTemplatePayload,
} from '@/types'
import { isActiveTaskStatus } from '@/utils/testing'

// 测试任务 Store（frontend-structure.md §3 / §3.1）：
//   - 模板/任务列表的跨页面共享数据；
//   - 任务详情 + 实时数据（liveLogs/liveMetrics/liveEvaluation/liveStatus）由 useWebSocket 订阅
//     `task:{id}` 频道写入，组件只读不直接持有 WebSocket。
// 页面局部状态（弹窗开关/表单草稿/筛选条件）留在组件。

/** 任务执行类动作（对齐 api-contract §2.4 的四个动词子资源）。 */
export type TaskAction = 'start' | 'cancel' | 'retry' | 'cleanup'

/** 实时日志本地保留上限（超出丢弃最旧记录，避免长时间任务撑爆内存）。 */
const MAX_LIVE_LOGS = 1000

export const useTaskStore = defineStore('task', () => {
  // ===================== 模板 =====================
  const templates = ref<TestTemplateInfo[]>([])
  const templateTotal = ref(0)
  const templatePage = ref(1)
  const templatePageSize = ref(20)
  const templateLoading = ref(false)

  // ===================== 任务列表 =====================
  const tasks = ref<TestTaskInfo[]>([])
  const total = ref(0)
  const page = ref(1)
  const pageSize = ref(20)
  const loading = ref(false)

  // ===================== 任务详情 =====================
  const currentTask = ref<TestTaskInfo | null>(null)
  const taskLoading = ref(false)

  // ===================== 实时数据 =====================
  const liveLogs = ref<TaskLogEvent[]>([])
  const liveMetrics = ref<Record<string, number> | null>(null)
  const liveEvaluation = ref<TaskEvaluation | null>(null)
  const liveStatus = ref<TaskStatus | ''>('')
  const realtimeConnected = ref(false)
  const realtimeError = ref('')
  /** 当前已订阅的任务频道 ID（空串表示未建立实时通道）。 */
  const realtimeTaskId = ref('')

  let client: WebSocketClient | null = null
  let disposers: Array<() => void> = []
  let stopConnectedWatch: WatchStopHandle | null = null
  let stopErrorWatch: WatchStopHandle | null = null

  // ===================== 模板 =====================

  /** 模板列表（关键字 + 分页）。 */
  async function fetchTemplates(query: TemplateQuery = {}): Promise<void> {
    templateLoading.value = true
    try {
      const res = await listTemplates(query)
      templates.value = res.list
      templateTotal.value = res.total
      templatePage.value = res.page
      templatePageSize.value = res.page_size
    } finally {
      templateLoading.value = false
    }
  }

  /** 新建/修改模板（id 为空表示新建；修改携带 version 乐观锁）。 */
  async function saveTemplate(
    id: string | null,
    payload: TestTemplatePayload,
  ): Promise<TestTemplateInfo> {
    return id ? await updateTemplate(id, payload) : await createTemplate(payload)
  }

  /** 删除模板（逻辑删除）。 */
  async function removeTemplate(id: string): Promise<void> {
    await deleteTemplate(id)
  }

  // ===================== 任务列表 =====================

  /** 任务列表（筛选 + 分页）。 */
  async function fetchTasks(query: TaskQuery = {}): Promise<void> {
    loading.value = true
    try {
      const res = await listTasks(query)
      tasks.value = res.list
      total.value = res.total
      page.value = res.page
      pageSize.value = res.page_size
    } finally {
      loading.value = false
    }
  }

  /** 创建任务（五要素组装，返回 pending 任务）。 */
  async function create(payload: CreateTaskPayload): Promise<TestTaskInfo> {
    return await createTask(payload)
  }

  /** 任务详情（silent=true 时用于轮询/终态刷新，不触发整页 loading）。 */
  async function fetchTask(id: string, silent = false): Promise<TestTaskInfo> {
    if (!silent) taskLoading.value = true
    try {
      const task = await getTask(id)
      currentTask.value = task
      return task
    } finally {
      if (!silent) taskLoading.value = false
    }
  }

  /** 执行类动作（开始/停止/重试/清理确认），同步刷新详情与列表中的同一任务。 */
  async function runTaskAction(id: string, action: TaskAction): Promise<TestTaskInfo> {
    const task =
      action === 'start'
        ? await startTask(id)
        : action === 'cancel'
          ? await cancelTask(id)
          : action === 'retry'
            ? await retryTask(id)
            : await cleanupTask(id)

    if (currentTask.value?.id === id) currentTask.value = task
    if (tasks.value.some((item) => item.id === id)) {
      tasks.value = tasks.value.map((item) => (item.id === id ? task : item))
    }
    return task
  }

  // ===================== 实时数据 =====================

  /** 追加一条实时日志（保持时间顺序，超上限丢弃最旧）。 */
  function appendLog(event: TaskLogEvent): void {
    const next = liveLogs.value.concat(event)
    liveLogs.value = next.length > MAX_LIVE_LOGS ? next.slice(next.length - MAX_LIVE_LOGS) : next
  }

  /** 仅清空实时日志（保留指标/评估数据）。 */
  function clearLogs(): void {
    liveLogs.value = []
  }

  /** 清空实时数据（重新执行/重试前调用）。 */
  function resetLive(): void {
    liveLogs.value = []
    liveMetrics.value = null
    liveEvaluation.value = null
    liveStatus.value = ''
  }

  /**
   * 建立任务实时订阅（task:{id}）。
   *
   * 事件契约（后端 orchestrator/ws）：
   *   - status：`{status,message}` 状态机推进；
   *   - log：`{task_id,seq,level,message,time}`（服务端已脱敏）；
   *   - metrics：`{p95,p99,error_rate,...}` 归一化指标；
   *   - evaluation：阈值评估结论（整体 + 逐条）。
   */
  function attachRealtime(taskId: string): void {
    detachRealtime()
    resetLive()
    realtimeTaskId.value = taskId

    const ws = createWebSocket()
    client = ws
    realtimeError.value = ''

    disposers = [
      ws.on<TaskStatusEvent>('status', (data) => {
        if (!data?.status) return
        liveStatus.value = data.status
        if (currentTask.value?.id === taskId) {
          currentTask.value = { ...currentTask.value, status: data.status }
        }
        // 到达终态/待确认后拉一次详情，补齐 result / finished_at 等字段。
        if (!isActiveTaskStatus(data.status) && currentTask.value?.id === taskId) {
          void fetchTask(taskId, true).catch(() => {
            // 静默刷新失败不打断页面（轮询兜底）
          })
        }
      }),
      ws.on<TaskLogEvent>('log', (data) => {
        if (!data?.message) return
        if (data.task_id && data.task_id !== taskId) return
        appendLog(data)
      }),
      ws.on<TaskMetricsEvent>('metrics', (data) => {
        if (!data) return
        liveMetrics.value = { ...data }
      }),
      ws.on<TaskEvaluation>('evaluation', (data) => {
        if (!data) return
        liveEvaluation.value = data
      }),
    ]

    stopConnectedWatch = watch(
      ws.connected,
      (value) => {
        realtimeConnected.value = value
      },
      { immediate: true },
    )
    stopErrorWatch = watch(
      ws.lastError,
      (value) => {
        realtimeError.value = value
      },
      { immediate: true },
    )

    ws.subscribe(`task:${taskId}`)
  }

  /** 关闭实时通道（离开任务详情页时调用）。 */
  function detachRealtime(): void {
    for (const dispose of disposers) dispose()
    disposers = []
    stopConnectedWatch?.()
    stopConnectedWatch = null
    stopErrorWatch?.()
    stopErrorWatch = null
    client?.close()
    client = null
    realtimeConnected.value = false
    realtimeTaskId.value = ''
  }

  return {
    // 模板
    templates,
    templateTotal,
    templatePage,
    templatePageSize,
    templateLoading,
    fetchTemplates,
    saveTemplate,
    removeTemplate,
    // 任务列表
    tasks,
    total,
    page,
    pageSize,
    loading,
    fetchTasks,
    create,
    // 任务详情
    currentTask,
    taskLoading,
    fetchTask,
    runTaskAction,
    // 实时
    liveLogs,
    liveMetrics,
    liveEvaluation,
    liveStatus,
    realtimeConnected,
    realtimeError,
    realtimeTaskId,
    clearLogs,
    resetLive,
    attachRealtime,
    detachRealtime,
  }
})
