<template>
  <div class="ap-page">
    <div class="ap-page-header">
      <h2 class="ap-page-title">SSH 与 Host 管理</h2>
      <div v-if="canWrite" class="ap-actions">
        <button type="button" class="ap-btn ap-btn-ghost" @click="openImport">批量导入</button>
        <button type="button" class="ap-btn ap-btn-primary" @click="openCreate">添加</button>
      </div>
    </div>

    <form class="ap-card ap-toolbar" @submit.prevent="search">
      <label class="ap-field">
        <span class="ap-field-label">主机/说明</span>
        <input v-model="keyword" placeholder="关键字检索" maxlength="128" />
      </label>
      <label class="ap-field">
        <span class="ap-field-label">记录类型</span>
        <select v-model="typeFilter">
          <option value="">全部</option>
          <option value="ssh">SSH 信任</option>
          <option value="host">Host 解析</option>
        </select>
      </label>
      <div class="ap-actions">
        <button type="submit" class="ap-btn ap-btn-primary" :disabled="loading">查询</button>
        <button type="button" class="ap-btn ap-btn-ghost" @click="reset">重置</button>
      </div>
    </form>

    <p class="ap-muted hints">
      SSH 私钥仅上行并由服务端加密保存（响应仅返回「已加密保存」标记）；Host 解析内容与可信主机供测试任务清理阶段使用。
    </p>

    <p v-if="errorMessage" class="ap-alert ap-alert-error" role="alert">{{ errorMessage }}</p>

    <SshHostTable
      :hosts="store.sshHosts"
      :loading="loading"
      :can-write="canWrite"
      @edit="openEdit"
      @remove="onRemove"
    />

    <div v-if="store.sshHostTotal > store.sshHostPageSize" class="ap-pagination">
      <button
        type="button"
        class="ap-btn ap-btn-ghost ap-btn-sm"
        :disabled="page <= 1 || loading"
        @click="changePage(page - 1)"
      >
        上一页
      </button>
      <span class="ap-muted">第 {{ page }} 页 / 共 {{ totalPages }} 页（{{ store.sshHostTotal }} 条）</span>
      <button
        type="button"
        class="ap-btn ap-btn-ghost ap-btn-sm"
        :disabled="page >= totalPages || loading"
        @click="changePage(page + 1)"
      >
        下一页
      </button>
    </div>

    <SshHostFormDialog
      :open="formOpen"
      :item="editing"
      :saving="saving"
      :error-message="dialogError"
      @close="closeForm"
      @submit="onSubmit"
    />

    <SshImportDialog
      :open="importOpen"
      :saving="importing"
      :error-message="importError"
      :result="importResult"
      @close="closeImport"
      @submit="onImport"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import SshHostFormDialog from '@/components/setting/SshHostFormDialog.vue'
import SshHostTable from '@/components/setting/SshHostTable.vue'
import SshImportDialog from '@/components/setting/SshImportDialog.vue'
import { useAuthStore } from '@/stores/auth'
import { useSettingStore } from '@/stores/setting'
import type { SshHostImportResult, SshHostInfo, SshHostPayload } from '@/types'
import { describeSettingError } from '@/utils/setting'

// SSH 与 Host 管理页（PRD §3.6.1 / api-contract §2.7）：列表 + 添加/修改/删除 + 批量导入。
const store = useSettingStore()
const auth = useAuthStore()

const canWrite = computed(() => auth.hasPerm('setting.ssh', 'write'))
const loading = computed(() => store.sshHostLoading)
const page = computed(() => store.sshHostPage)
const totalPages = computed(() =>
  Math.max(1, Math.ceil(store.sshHostTotal / (store.sshHostPageSize || 20))),
)

const keyword = ref('')
const typeFilter = ref('')
const errorMessage = ref('')

const formOpen = ref(false)
const editing = ref<SshHostInfo | null>(null)
const saving = ref(false)
const dialogError = ref('')

const importOpen = ref(false)
const importing = ref(false)
const importError = ref('')
const importResult = ref<SshHostImportResult | null>(null)

async function load(targetPage = 1): Promise<void> {
  errorMessage.value = ''
  try {
    await store.fetchSshHosts({
      keyword: keyword.value.trim() || undefined,
      type: typeFilter.value || undefined,
      page: targetPage,
      page_size: store.sshHostPageSize || 20,
    })
  } catch (err) {
    errorMessage.value = describeSettingError(err, '加载 SSH/Host 列表失败')
  }
}

function search(): void {
  void load(1)
}

function reset(): void {
  keyword.value = ''
  typeFilter.value = ''
  void load(1)
}

function changePage(target: number): void {
  void load(target)
}

function openCreate(): void {
  editing.value = null
  dialogError.value = ''
  formOpen.value = true
}

function openEdit(host: SshHostInfo): void {
  editing.value = host
  dialogError.value = ''
  formOpen.value = true
}

function closeForm(): void {
  if (saving.value) return
  formOpen.value = false
  editing.value = null
  dialogError.value = ''
}

async function onSubmit(payload: SshHostPayload): Promise<void> {
  if (saving.value) return
  saving.value = true
  dialogError.value = ''
  const isEdit = editing.value !== null
  try {
    await store.saveSshHost(editing.value?.id ?? null, payload)
    formOpen.value = false
    editing.value = null
    await load(isEdit ? page.value : 1)
  } catch (err) {
    // 10302 重复 / 10402 并发冲突 / 10003 参数非法。
    dialogError.value = describeSettingError(err, '保存 SSH/Host 失败')
  } finally {
    saving.value = false
  }
}

async function onRemove(host: SshHostInfo): Promise<void> {
  if (!window.confirm(`确认删除「${host.host}${host.port ? `:${host.port}` : ''}」？删除为逻辑删除。`)) {
    return
  }
  errorMessage.value = ''
  try {
    await store.removeSshHost(host.id)
    await load(page.value)
  } catch (err) {
    // 26005 已被环境引用。
    errorMessage.value = describeSettingError(err, '删除 SSH/Host 失败')
  }
}

function openImport(): void {
  importError.value = ''
  importResult.value = null
  importOpen.value = true
}

function closeImport(): void {
  if (importing.value) return
  importOpen.value = false
  importResult.value = null
  importError.value = ''
}

async function onImport(file: File): Promise<void> {
  if (importing.value) return
  importing.value = true
  importError.value = ''
  try {
    importResult.value = await store.uploadSshHosts(file)
    await load(1)
  } catch (err) {
    importError.value = describeSettingError(err, '批量导入失败')
  } finally {
    importing.value = false
  }
}

onMounted(() => {
  void load(1)
})
</script>

<style scoped>
.hints {
  margin: 8px 0;
  font-size: 12px;
  line-height: 1.6;
}
</style>
