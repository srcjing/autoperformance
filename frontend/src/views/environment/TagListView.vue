<template>
  <div class="ap-page">
    <div class="ap-page-header">
      <h2 class="ap-page-title">标签维护</h2>
      <button v-if="canWrite" type="button" class="ap-btn ap-btn-primary" @click="openCreate">
        新建标签
      </button>
    </div>

    <form class="ap-card ap-toolbar" @submit.prevent="search">
      <label class="ap-field">
        <span class="ap-field-label">标签名称/说明</span>
        <input v-model="keyword" placeholder="关键字检索" maxlength="64" />
      </label>
      <div class="ap-actions">
        <button type="submit" class="ap-btn ap-btn-primary" :disabled="loading">查询</button>
        <button type="button" class="ap-btn ap-btn-ghost" @click="reset">重置</button>
      </div>
    </form>

    <p v-if="errorMessage" class="ap-alert ap-alert-error" role="alert">{{ errorMessage }}</p>

    <TagTable
      :tags="store.tags"
      :loading="loading"
      :can-write="canWrite"
      @edit="openEdit"
      @remove="onRemove"
      @show-envs="showEnvironments"
    />

    <!-- 标签新建/编辑弹窗（标签字段仅名称/颜色/说明） -->
    <div v-if="dialogOpen" class="ap-dialog-mask">
      <div class="ap-dialog" role="dialog" aria-modal="true">
        <div class="ap-dialog-header">
          <span>{{ editing ? '编辑标签' : '新建标签' }}</span>
          <button type="button" class="ap-link" :disabled="saving" @click="closeDialog">关闭</button>
        </div>
        <form class="ap-dialog-body" @submit.prevent="onSubmit">
          <div class="ap-form-row">
            <label class="ap-field">
              <span class="ap-field-label">标签名称 *</span>
              <input v-model="form.name" :disabled="saving" maxlength="64" placeholder="如：内网" />
            </label>
            <label class="ap-field">
              <span class="ap-field-label">颜色</span>
              <input v-model="form.color" type="color" :disabled="saving" />
            </label>
          </div>
          <label class="ap-field">
            <span class="ap-field-label">说明</span>
            <textarea v-model="form.description" :disabled="saving" rows="2" maxlength="500" />
          </label>

          <p v-if="formError" class="ap-alert ap-alert-error" role="alert">{{ formError }}</p>

          <div class="ap-dialog-footer">
            <button type="button" class="ap-btn ap-btn-ghost" :disabled="saving" @click="closeDialog">
              取消
            </button>
            <button type="submit" class="ap-btn ap-btn-primary" :disabled="saving">
              {{ saving ? '保存中…' : '保存' }}
            </button>
          </div>
        </form>
      </div>
    </div>

    <TagEnvDrawer
      :open="drawerOpen"
      :tag="activeTag"
      :environments="drawerEnvironments"
      :total="drawerTotal"
      :page="drawerPage"
      :page-size="drawerPageSize"
      :loading="drawerLoading"
      @close="closeDrawer"
      @page-change="changeDrawerPage"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ApiError } from '@/api/http'
import TagEnvDrawer from '@/components/environment/TagEnvDrawer.vue'
import TagTable from '@/components/environment/TagTable.vue'
import { useAuthStore } from '@/stores/auth'
import { useEnvironmentStore } from '@/stores/environment'
import type { EnvironmentInfo, TagInfo, TagPayload } from '@/types'

// 标签维护页（PRD §3.1.1）：标签新增/修改/删除 + 关联环境反查。
// 删除受引用保护约束：被环境引用时后端返回 21003，前端提示先解除关联。
const store = useEnvironmentStore()
const auth = useAuthStore()

const canWrite = computed(() => auth.hasPerm('env.tag', 'write'))
const keyword = ref('')
const loading = ref(false)
const errorMessage = ref('')

const dialogOpen = ref(false)
const editing = ref<TagInfo | null>(null)
const saving = ref(false)
const formError = ref('')
const form = reactive<TagPayload>({ name: '', color: '#2563eb', description: '' })

const drawerOpen = ref(false)
const activeTag = ref<TagInfo | null>(null)
const drawerPage = ref(1)
const drawerPageSize = 10
const drawerLoading = ref(false)

const drawerCache = computed(() =>
  activeTag.value ? store.tagEnvMap[activeTag.value.id] : undefined,
)
const drawerEnvironments = computed<EnvironmentInfo[]>(() => drawerCache.value?.list ?? [])
const drawerTotal = computed(() => drawerCache.value?.total ?? 0)

async function fetchTags(): Promise<void> {
  loading.value = true
  errorMessage.value = ''
  try {
    await store.fetchTags(keyword.value.trim())
  } catch (err) {
    errorMessage.value = err instanceof ApiError ? err.message : '加载标签失败'
  } finally {
    loading.value = false
  }
}

function search(): void {
  void fetchTags()
}

function reset(): void {
  keyword.value = ''
  void fetchTags()
}

function openCreate(): void {
  editing.value = null
  form.name = ''
  form.color = '#2563eb'
  form.description = ''
  formError.value = ''
  dialogOpen.value = true
}

function openEdit(tag: TagInfo): void {
  editing.value = tag
  form.name = tag.name
  form.color = tag.color || '#2563eb'
  form.description = tag.description ?? ''
  formError.value = ''
  dialogOpen.value = true
}

function closeDialog(): void {
  if (saving.value) return
  dialogOpen.value = false
  editing.value = null
  formError.value = ''
}

async function onSubmit(): Promise<void> {
  if (saving.value) return
  const name = form.name.trim()
  if (!name) {
    formError.value = '请填写标签名称'
    return
  }

  saving.value = true
  formError.value = ''
  try {
    await store.saveTag(editing.value?.id ?? null, {
      name,
      color: form.color,
      description: (form.description ?? '').trim(),
    })
    dialogOpen.value = false
    editing.value = null
    await fetchTags()
  } catch (err) {
    // 10302 标签重名 / 10301 标签不存在。
    formError.value = err instanceof ApiError ? err.message : '保存标签失败'
  } finally {
    saving.value = false
  }
}

async function onRemove(tag: TagInfo): Promise<void> {
  if (!window.confirm(`确认删除标签「${tag.name}」？删除为逻辑删除。`)) return

  errorMessage.value = ''
  try {
    await store.removeTag(tag.id)
    if (activeTag.value?.id === tag.id) closeDrawer()
    await fetchTags()
  } catch (err) {
    // 21003 标签已被环境引用。
    errorMessage.value = err instanceof ApiError ? err.message : '删除标签失败'
  }
}

function showEnvironments(tag: TagInfo): void {
  activeTag.value = tag
  drawerPage.value = 1
  drawerOpen.value = true
  void loadDrawer()
}

function closeDrawer(): void {
  drawerOpen.value = false
  activeTag.value = null
}

async function loadDrawer(): Promise<void> {
  if (!activeTag.value) return
  drawerLoading.value = true
  errorMessage.value = ''
  try {
    await store.fetchTagEnvironments(activeTag.value.id, drawerPage.value, drawerPageSize)
  } catch (err) {
    errorMessage.value = err instanceof ApiError ? err.message : '加载关联环境失败'
  } finally {
    drawerLoading.value = false
  }
}

function changeDrawerPage(page: number): void {
  drawerPage.value = page
  void loadDrawer()
}

onMounted(fetchTags)
</script>
