<template>
  <div class="ap-page">
    <div class="ap-page-header">
      <div class="title-block">
        <h2 class="ap-page-title">测试任务</h2>
        <span class="ap-muted">
          五要素编排（环境 / 接口范围 / 流程 / 报告 / 清理）；创建时固化接口与阈值快照
        </span>
      </div>
      <button v-if="canWrite" type="button" class="ap-btn ap-btn-primary" @click="openCreate()">
        新建任务
      </button>
    </div>

    <form class="ap-card ap-toolbar" @submit.prevent="search">
      <label class="ap-field">
        <span class="ap-field-label">任务名称</span>
        <input v-model="filters.keyword" placeholder="关键字检索" maxlength="128" />
      </label>
      <label class="ap-field">
        <span class="ap-field-label">状态</span>
        <select v-model="filters.status">
          <option value="">全部状态</option>
          <option v-for="item in TASK_STATUS_META" :key="item.value" :value="item.value">
            {{ item.label }}
          </option>
        </select>
      </label>
      <label class="ap-field">
        <span class="ap-field-label">环境</span>
        <select v-model="filters.envId">
          <option value="">全部环境</option>
          <option v-for="item in envOptions" :key="item.id" :value="item.id">
            {{ item.name }}
          </option>
        </select>
      </label>
      <label class="ap-field">
        <span class="ap-field-label">创建时间起</span>
        <input v-model="filters.createdFrom" type="date" />
      </label>
      <label class="ap-field">
        <span class="ap-field-label">创建时间止</span>
        <input v-model="filters.createdTo" type="date" />
      </label>
      <div class="ap-actions">
        <button type="submit" class="ap-btn ap-btn-primary" :disabled="store.loading">查询</button>
        <button type="button" class="ap-btn ap-btn-ghost" @click="resetFilters">重置</button>
      </div>
    </form>

    <p v-if="errorMessage" class="ap-alert ap-alert-error" role="alert">{{ errorMessage }}</p>
    <p v-if="envError" class="ap-alert ap-alert-hint">{{ envError }}</p>

    <TaskTable
      :tasks="store.tasks"
      :loading="store.loading"
      :can-write="canWrite"
      :env-names="envNames"
      @start="onStart"
      @cancel="onCancel"
      @retry="onRetry"
      @cleanup="onCleanup"
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

    <TaskFormDialog
      :open="dialogOpen"
      :templates="store.templates"
      :initial-template-id="initialTemplateId"
      :saving="saving"
      :error-message="dialogError"
      @close="closeDialog"
      @submit="onSubmit"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import TaskFormDialog from '@/components/testing/TaskFormDialog.vue'
import TaskTable from '@/components/testing/TaskTable.vue'
import { useEnvironmentOptions } from '@/composables/useEnvironmentOptions'
import { useAuthStore } from '@/stores/auth'
import { useTaskStore } from '@/stores/task'
import { describeTaskError, TASK_STATUS_META } from '@/utils/testing'
import type { CreateTaskPayload, TaskQuery, TestTaskInfo } from '@/types'

// 测试任务列表页（PRD §3.3）：筛选 + 分页 + 五要素创建 + 状态驱动操作。
// 操作按钮只做前端显隐，最终状态机校验在后端（非法流转 23001、环境占用 23002）。
const store = useTaskStore()
const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const { options: envOptions, names: envNames, error: envError, load: loadEnvOptions } =
  useEnvironmentOptions()

const canWrite = computed(() => auth.hasPerm('testing.task', 'write'))

const filters = reactive({
  keyword: '',
  status: '',
  envId: '',
  createdFrom: '',
  createdTo: '',
})

const errorMessage = ref('')
const dialogOpen = ref(false)
const dialogError = ref('')
const initialTemplateId = ref('')
const saving = ref(false)
const acting = ref(false)

const totalPages = computed(() =>
  Math.max(1, Math.ceil(store.total / Math.max(store.pageSize, 1))),
)

/** 日期（YYYY-MM-DD）→ RFC3339（后端 created_from/created_to 按 UTC 解析）。 */
function toRfc3339(date: string, endOfDay: boolean): string | undefined {
  if (!date) return undefined
  return `${date}T${endOfDay ? '23:59:59' : '00:00:00'}Z`
}

function buildQuery(page: number): TaskQuery {
  return {
    keyword: filters.keyword.trim() || undefined,
    status: filters.status || undefined,
    env_id: filters.envId || undefined,
    created_from: toRfc3339(filters.createdFrom, false),
    created_to: toRfc3339(filters.createdTo, true),
    page,
    page_size: 20,
  }
}

async function load(page = 1): Promise<void> {
  errorMessage.value = ''
  try {
    await store.fetchTasks(buildQuery(page))
  } catch (err) {
    errorMessage.value = describeTaskError(err, '加载任务列表失败')
  }
}

function search(): void {
  void load(1)
}

function resetFilters(): void {
  filters.keyword = ''
  filters.status = ''
  filters.envId = ''
  filters.createdFrom = ''
  filters.createdTo = ''
  void load(1)
}

function goPage(page: number): void {
  void load(page)
}

/** 打开创建弹窗（可由模板列表跳转带入模板 ID）。 */
function openCreate(templateId = ''): void {
  initialTemplateId.value = templateId
  dialogError.value = ''
  dialogOpen.value = true
}

function closeDialog(): void {
  if (saving.value) return
  dialogOpen.value = false
  initialTemplateId.value = ''
  dialogError.value = ''
}

async function onSubmit(payload: CreateTaskPayload): Promise<void> {
  if (saving.value) return
  saving.value = true
  dialogError.value = ''
  try {
    const task = await store.create(payload)
    dialogOpen.value = false
    initialTemplateId.value = ''
    await load(1)
    // 创建后进入详情页，便于立即执行并观察实时日志/指标。
    await router.push({ name: 'task-detail', params: { taskId: task.id } })
  } catch (err) {
    // 23002 环境占用 / 23003 接口范围为空 / 23004 并发非法。
    dialogError.value = describeTaskError(err, '创建任务失败')
  } finally {
    saving.value = false
  }
}

/** 执行类动作统一处理：确认 → 调用 → 刷新列表 → 失败提示业务码。 */
async function runAction(task: TestTaskInfo, action: 'start' | 'cancel' | 'retry' | 'cleanup'): Promise<void> {
  if (acting.value) return
  const confirmText: Record<typeof action, string> = {
    start: `确认开始任务「${task.name}」？将占用环境并拉起压测。`,
    cancel: `确认停止任务「${task.name}」？k6 子进程将被终止，环境保留待确认清理。`,
    retry: `确认重试任务「${task.name}」？将复用原接口/阈值快照重新执行。`,
    cleanup: `确认执行环境清理（任务「${task.name}」）？清理成功后任务结束。`,
  }
  if (!window.confirm(confirmText[action])) return

  acting.value = true
  errorMessage.value = ''
  try {
    await store.runTaskAction(task.id, action)
    if (action === 'start') {
      await router.push({ name: 'task-detail', params: { taskId: task.id } })
      return
    }
    await load(store.page)
  } catch (err) {
    errorMessage.value = describeTaskError(err, '任务操作失败')
  } finally {
    acting.value = false
  }
}

function onStart(task: TestTaskInfo): void {
  void runAction(task, 'start')
}

function onCancel(task: TestTaskInfo): void {
  void runAction(task, 'cancel')
}

function onRetry(task: TestTaskInfo): void {
  void runAction(task, 'retry')
}

function onCleanup(task: TestTaskInfo): void {
  void runAction(task, 'cleanup')
}

onMounted(async () => {
  await loadEnvOptions()
  // 模板下拉数据（创建弹窗「基于模板」来源）。
  try {
    await store.fetchTemplates({ page: 1, page_size: 100 })
  } catch {
    // 模板加载失败不阻断任务列表；弹窗内可「不使用模板」。
  }
  await load(1)

  const templateId = route.query.template_id
  if (typeof templateId === 'string' && templateId) {
    openCreate(templateId)
    // 清理 query，避免刷新后重复弹出。
    await router.replace({ name: 'task-list', query: {} })
  }
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
