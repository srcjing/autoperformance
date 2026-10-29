<template>
  <div class="ap-page">
    <div class="ap-page-header">
      <h2 class="ap-page-title">环境列表</h2>
      <button v-if="canWrite" type="button" class="ap-btn ap-btn-primary" @click="openCreate">
        新建环境
      </button>
    </div>

    <form class="ap-card ap-toolbar" @submit.prevent="search">
      <label class="ap-field">
        <span class="ap-field-label">名称/编码</span>
        <input v-model="filters.keyword" placeholder="关键字检索" maxlength="64" />
      </label>
      <label class="ap-field">
        <span class="ap-field-label">分类</span>
        <select v-model="filters.categoryId">
          <option value="">全部分类</option>
          <option v-for="item in store.categories" :key="item.id" :value="item.id">{{ item.name }}</option>
        </select>
      </label>
      <label class="ap-field">
        <span class="ap-field-label">状态</span>
        <select v-model="filters.status">
          <option value="">全部状态</option>
          <option value="pending">待测试</option>
          <option value="testing">测试中</option>
          <option value="disabled">已下线</option>
        </select>
      </label>
      <div class="ap-field tag-filter">
        <span class="ap-field-label">标签（多选取交集）</span>
        <TagSelector v-model="filters.tagIds" :tags="store.tags" />
      </div>
      <div class="ap-actions">
        <button type="submit" class="ap-btn ap-btn-primary" :disabled="store.loading">查询</button>
        <button type="button" class="ap-btn ap-btn-ghost" @click="resetFilters">重置</button>
      </div>
    </form>

    <p v-if="errorMessage" class="ap-alert ap-alert-error" role="alert">{{ errorMessage }}</p>

    <EnvTable
      :environments="store.environments"
      :loading="store.loading"
      :can-write="canWrite"
      @edit="openEdit"
      @toggle="onToggleStatus"
      @remove="onRemove"
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

    <EnvFormDialog
      :open="dialogOpen"
      :environment="editing"
      :categories="store.categories"
      :tags="store.tags"
      :saving="saving"
      :error-message="dialogError"
      @close="closeDialog"
      @submit="onSubmit"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ApiError } from '@/api/http'
import EnvFormDialog from '@/components/environment/EnvFormDialog.vue'
import EnvTable from '@/components/environment/EnvTable.vue'
import TagSelector from '@/components/environment/TagSelector.vue'
import { useAuthStore } from '@/stores/auth'
import { useEnvironmentStore } from '@/stores/environment'
import type { EnvironmentInfo, EnvironmentPayload } from '@/types'

// 环境列表页（PRD §3.1）：筛选（名称/分类/标签/状态）+ CRUD + 停用/启用。
// 数据经 useEnvironmentStore 共享；停用/删除受状态机限制（testing → 21004）。
const store = useEnvironmentStore()
const auth = useAuthStore()

const canWrite = computed(() => auth.hasPerm('env.list', 'write'))
const filters = reactive({
  keyword: '',
  categoryId: '',
  tagIds: [] as string[],
  status: '',
})

const errorMessage = ref('')
const dialogOpen = ref(false)
const dialogError = ref('')
const editing = ref<EnvironmentInfo | null>(null)
const saving = ref(false)

const totalPages = computed(() =>
  Math.max(1, Math.ceil(store.total / Math.max(store.pageSize, 1))),
)

/** 按当前筛选条件加载指定页（默认第 1 页）。 */
async function load(page = 1): Promise<void> {
  errorMessage.value = ''
  try {
    await store.fetchList({
      keyword: filters.keyword.trim(),
      category_id: filters.categoryId || undefined,
      tag_ids: filters.tagIds,
      status: filters.status || undefined,
      page,
      page_size: 20,
    })
  } catch (err) {
    errorMessage.value = err instanceof ApiError ? err.message : '加载环境列表失败'
  }
}

function search(): void {
  void load(1)
}

function resetFilters(): void {
  filters.keyword = ''
  filters.categoryId = ''
  filters.tagIds = []
  filters.status = ''
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

function openEdit(env: EnvironmentInfo): void {
  editing.value = env
  dialogError.value = ''
  dialogOpen.value = true
}

function closeDialog(): void {
  if (saving.value) return
  dialogOpen.value = false
  editing.value = null
  dialogError.value = ''
}

async function onSubmit(payload: EnvironmentPayload): Promise<void> {
  if (saving.value) return
  saving.value = true
  dialogError.value = ''
  const wasEdit = editing.value !== null
  try {
    await store.saveEnvironment(editing.value?.id ?? null, payload)
    dialogOpen.value = false
    editing.value = null
    // 编辑保持当前页，新建回到第 1 页（列表按 ID 升序）。
    await Promise.all([load(wasEdit ? store.page : 1), store.fetchTags()])
  } catch (err) {
    // 21001 编码重复 / 10402 乐观锁冲突 / 10301 分类或标签不存在。
    dialogError.value = err instanceof ApiError ? err.message : '保存环境失败'
  } finally {
    saving.value = false
  }
}

async function onToggleStatus(env: EnvironmentInfo): Promise<void> {
  const target = env.status === 'disabled' ? 'pending' : 'disabled'
  const action = target === 'pending' ? '启用' : '停用'
  if (!window.confirm(`确认${action}环境「${env.name}」？`)) return

  errorMessage.value = ''
  try {
    await store.setStatus(env.id, target)
    await load(store.page)
  } catch (err) {
    // 21004 被任务占用 / 10401 当前状态不允许该操作。
    errorMessage.value = err instanceof ApiError ? err.message : `${action}环境失败`
  }
}

async function onRemove(env: EnvironmentInfo): Promise<void> {
  if (!window.confirm(`确认删除环境「${env.name}」？删除为逻辑删除，历史关联数据保留。`)) return

  errorMessage.value = ''
  try {
    await store.removeEnvironment(env.id)
    await Promise.all([load(store.page), store.fetchTags()])
  } catch (err) {
    errorMessage.value = err instanceof ApiError ? err.message : '删除环境失败'
  }
}

onMounted(async () => {
  try {
    await Promise.all([store.fetchCategories(), store.fetchTags()])
  } catch {
    errorMessage.value = '加载分类/标签失败，请稍后重试'
  }
  await load(1)
})
</script>

<style scoped>
.tag-filter {
  min-width: 240px;
  max-width: 420px;
}
</style>
