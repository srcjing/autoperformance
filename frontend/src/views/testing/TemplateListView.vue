<template>
  <div class="ap-page">
    <div class="ap-page-header">
      <div class="title-block">
        <h2 class="ap-page-title">预设模板</h2>
        <span class="ap-muted">模板 = 预填五要素的流程，可一键创建任务并覆盖参数</span>
      </div>
      <button v-if="canWrite" type="button" class="ap-btn ap-btn-primary" @click="openCreate">
        新建模板
      </button>
    </div>

    <form class="ap-card ap-toolbar" @submit.prevent="search">
      <label class="ap-field">
        <span class="ap-field-label">模板名称</span>
        <input v-model="keyword" placeholder="关键字检索" maxlength="64" />
      </label>
      <div class="ap-actions">
        <button type="submit" class="ap-btn ap-btn-primary" :disabled="store.templateLoading">
          查询
        </button>
        <button type="button" class="ap-btn ap-btn-ghost" @click="resetFilters">重置</button>
      </div>
    </form>

    <p v-if="errorMessage" class="ap-alert ap-alert-error" role="alert">{{ errorMessage }}</p>

    <TemplateTable
      :templates="store.templates"
      :loading="store.templateLoading"
      :can-write="canWrite"
      :can-create-task="canTaskWrite"
      @edit="openEdit"
      @remove="onRemove"
      @create-task="createTaskFrom"
    />

    <div class="ap-pagination">
      <span>共 {{ store.templateTotal }} 条，第 {{ store.templatePage }} / {{ totalPages }} 页</span>
      <button
        type="button"
        class="ap-btn ap-btn-ghost ap-btn-sm"
        :disabled="store.templatePage <= 1 || store.templateLoading"
        @click="goPage(store.templatePage - 1)"
      >
        上一页
      </button>
      <button
        type="button"
        class="ap-btn ap-btn-ghost ap-btn-sm"
        :disabled="store.templatePage >= totalPages || store.templateLoading"
        @click="goPage(store.templatePage + 1)"
      >
        下一页
      </button>
    </div>

    <TemplateFormDialog
      :open="dialogOpen"
      :template="editing"
      :saving="saving"
      :error-message="dialogError"
      @close="closeDialog"
      @submit="onSubmit"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import TemplateFormDialog from '@/components/testing/TemplateFormDialog.vue'
import TemplateTable from '@/components/testing/TemplateTable.vue'
import { useAuthStore } from '@/stores/auth'
import { useTaskStore } from '@/stores/task'
import { describeTaskError } from '@/utils/testing'
import type { TestTemplateInfo, TestTemplatePayload } from '@/types'

// 预设模板页（PRD §3.3 测试模板）：模板 CRUD + 基于模板创建任务。
// 数据经 useTaskStore.templates 共享；删除为逻辑删除，历史任务快照不受影响。
const store = useTaskStore()
const auth = useAuthStore()
const router = useRouter()

const canWrite = computed(() => auth.hasPerm('testing.template', 'write'))
const canTaskWrite = computed(() => auth.hasPerm('testing.task', 'write'))

const keyword = ref('')
const errorMessage = ref('')

const dialogOpen = ref(false)
const dialogError = ref('')
const editing = ref<TestTemplateInfo | null>(null)
const saving = ref(false)

const totalPages = computed(() =>
  Math.max(1, Math.ceil(store.templateTotal / Math.max(store.templatePageSize, 1))),
)

/** 按关键字加载指定页（默认第 1 页）。 */
async function load(page = 1): Promise<void> {
  errorMessage.value = ''
  try {
    await store.fetchTemplates({
      keyword: keyword.value.trim() || undefined,
      page,
      page_size: 20,
    })
  } catch (err) {
    errorMessage.value = describeTaskError(err, '加载模板列表失败')
  }
}

function search(): void {
  void load(1)
}

function resetFilters(): void {
  keyword.value = ''
  void load(1)
}

function goPage(page: number): void {
  void load(page)
}

function openCreate(): void {
  editing.value = null
  dialogError.value = ''
  dialogOpen.value = true
}

function openEdit(template: TestTemplateInfo): void {
  editing.value = template
  dialogError.value = ''
  dialogOpen.value = true
}

function closeDialog(): void {
  if (saving.value) return
  dialogOpen.value = false
  editing.value = null
  dialogError.value = ''
}

async function onSubmit(payload: TestTemplatePayload): Promise<void> {
  if (saving.value) return
  saving.value = true
  dialogError.value = ''
  const wasEdit = editing.value !== null
  try {
    await store.saveTemplate(editing.value?.id ?? null, payload)
    dialogOpen.value = false
    editing.value = null
    await load(wasEdit ? store.templatePage : 1)
  } catch (err) {
    // 10003 参数非法 / 10402 乐观锁冲突 / 10302 重名。
    dialogError.value = describeTaskError(err, '保存模板失败')
  } finally {
    saving.value = false
  }
}

async function onRemove(template: TestTemplateInfo): Promise<void> {
  if (
    !window.confirm(`确认删除模板「${template.name}」？删除为逻辑删除，已创建任务不受影响。`)
  ) {
    return
  }
  errorMessage.value = ''
  try {
    await store.removeTemplate(template.id)
    await load(store.templatePage)
  } catch (err) {
    errorMessage.value = describeTaskError(err, '删除模板失败')
  }
}

/** 基于模板创建任务：跳转任务列表并自动打开创建弹窗（带模板 ID）。 */
function createTaskFrom(template: TestTemplateInfo): void {
  void router.push({ name: 'task-list', query: { template_id: template.id } })
}

onMounted(() => {
  void load(1)
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
