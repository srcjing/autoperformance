<template>
  <div v-if="open" class="ap-dialog-mask" @click.self="onClose">
    <div class="ap-dialog" role="dialog" aria-modal="true">
      <div class="ap-dialog-header">
        <span>{{ isEdit ? '编辑接口' : '新建接口' }}</span>
        <button type="button" class="ap-link" :disabled="saving" @click="onClose">关闭</button>
      </div>

      <form class="ap-dialog-body" @submit.prevent="onSubmit">
        <div class="import-bar">
          <span class="ap-muted">可从原始 HTTP 报文或 JSON 一键识别并填充</span>
          <button type="button" class="ap-btn ap-btn-ghost ap-btn-sm" :disabled="saving" @click="importOpen = true">
            导入请求
          </button>
        </div>

        <section class="form-section">
          <h4 class="section-title">1. 基本信息</h4>
        <div class="ap-form-row">
          <label class="ap-field">
            <span class="ap-field-label">接口名称 *</span>
            <input v-model="name" :disabled="saving" placeholder="如：创建订单" maxlength="128" />
          </label>
          <label class="ap-field method-field">
            <span class="ap-field-label">请求方法 *</span>
            <select v-model="method" :disabled="saving">
              <option v-for="item in methods" :key="item" :value="item">{{ item }}</option>
            </select>
          </label>
        </div>

        <div class="ap-form-row address-row">
          <label class="ap-field scheme-field">
            <span class="ap-field-label">协议 *</span>
            <select v-model="scheme" :disabled="saving">
              <option value="http">http</option>
              <option value="https">https</option>
            </select>
          </label>
          <label class="ap-field port-field">
            <span class="ap-field-label">端口 *（1-65535）</span>
            <input
              v-model.number="port"
              :disabled="saving"
              type="number"
              min="1"
              max="65535"
              placeholder="443"
            />
          </label>
          <label class="ap-field path-field">
            <span class="ap-field-label">路径 *（以 / 开头）</span>
            <input
              v-model="path"
              :disabled="saving"
              placeholder="/api/order/create"
              maxlength="2048"
            />
          </label>
        </div>
        <p class="ap-alert ap-alert-hint">
          接口不配置 host：创建测试任务时从所选环境的 hosts 中任选一个作为压测 host，
          完整地址 = 协议://host:端口 + 路径。
        </p>
        </section>

        <section class="form-section">
          <h4 class="section-title">2. 请求头</h4>
        <div class="ap-field">
          <HeaderRowsEditor v-model="headers" :disabled="saving" label="请求头（勾选启用 / 键 / 值，可粘贴识别）" />
        </div>
        <p class="ap-alert ap-alert-hint">
          认证头（Authorization / Token / Cookie / X-Api-Key 等）由服务端 AES-256-GCM 加密保存，页面与接口响应均不展示明文。
          编辑时认证头值留空即沿用已保存的值；删除该行即删除该认证头。
        </p>
        </section>

        <section class="form-section">
          <h4 class="section-title">3. 请求体</h4>
        <div class="ap-field">
          <BodyRowsEditor
            v-model="bodyText"
            :disabled="saving"
            label="请求体参数（点「+ 嵌套」添加子级参数）"
          />
          <span class="ap-muted">
            流程测试中请求体 / 路径可用占位符（如 <code v-pre>{{token}}</code>）引用上一步提取的变量。
          </span>
        </div>
        </section>

        <section class="form-section">
          <h4 class="section-title">4. 超时与描述</h4>
        <div class="ap-form-row">
          <label class="ap-field">
            <span class="ap-field-label">建连超时（ms）</span>
            <input v-model.number="connectMS" type="number" min="0" :disabled="saving" />
          </label>
          <label class="ap-field">
            <span class="ap-field-label">读取超时（ms）</span>
            <input v-model.number="readMS" type="number" min="0" :disabled="saving" />
          </label>
          <label class="ap-field">
            <span class="ap-field-label">总超时（ms）</span>
            <input v-model.number="totalMS" type="number" min="0" :disabled="saving" />
          </label>
        </div>

        <label class="ap-field">
          <span class="ap-field-label">描述</span>
          <textarea v-model="description" :disabled="saving" rows="2" maxlength="500" />
        </label>
        </section>

        <p v-if="displayError" class="ap-alert ap-alert-error" role="alert">{{ displayError }}</p>
        <p v-else-if="isEdit" class="ap-alert ap-alert-hint">
          当前版本号 {{ item?.version }}：保存时若已被他人修改会提示并发冲突，请刷新后重试。
        </p>

        <div class="ap-dialog-footer">
          <button type="button" class="ap-btn ap-btn-ghost" :disabled="saving" @click="onClose">取消</button>
          <button type="submit" class="ap-btn ap-btn-primary" :disabled="saving">
            {{ saving ? '保存中…' : '保存' }}
          </button>
        </div>
      </form>

      <RequestImportDialog :open="importOpen" @close="importOpen = false" @fill="onImportFill" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import BodyRowsEditor from '@/components/common/BodyRowsEditor.vue'
import HeaderRowsEditor from '@/components/common/HeaderRowsEditor.vue'
import RequestImportDialog from '@/components/common/RequestImportDialog.vue'
import type { HttpMethod, InterfaceInfo, InterfacePayload, InterfaceScheme } from '@/types'

// 接口新建/编辑弹窗（全量字段 + version 乐观锁）。
// headers 为全量替换语义：认证头值留空表示沿用已保存密文（DES-I-001）。
const props = defineProps<{
  open: boolean
  item: InterfaceInfo | null
  saving: boolean
  /** 服务端返回的业务错误（10301/10402 等），由列表页透传。 */
  errorMessage?: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'submit', payload: InterfacePayload): void
}>()

const methods: HttpMethod[] = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE']

const name = ref('')
const method = ref<HttpMethod>('GET')
const scheme = ref<InterfaceScheme>('http')
const port = ref(80)
const path = ref('')
// 请求头（键 → 值）；认证头值留空 = 沿用已保存密文。
const headers = ref<Record<string, string>>({})
const bodyText = ref('')
const importOpen = ref(false)

/** 导入请求 → 回填方法 / 地址 / 请求头 / 请求体。 */
function onImportFill(payload: {
  method?: string
  url?: string
  headers?: Record<string, string>
  body?: string
}): void {
  importOpen.value = false
  if (payload.method) {
    method.value = payload.method as HttpMethod
  }
  if (payload.url) {
    try {
      const parsed = new URL(payload.url)
      scheme.value = parsed.protocol === 'http:' ? 'http' : 'https'
      port.value = Number(parsed.port) || (parsed.protocol === 'http:' ? 80 : 443)
      path.value = `${parsed.pathname}${parsed.search}`
    } catch {
      formError.value = '导入的地址无法解析，请手动填写路径'
    }
  }
  if (payload.headers) {
    headers.value = { ...payload.headers }
  }
  if (payload.body) {
    bodyText.value = payload.body
  }
}
const connectMS = ref(0)
const readMS = ref(0)
const totalMS = ref(0)
const description = ref('')
const formError = ref('')

const displayError = computed(() => props.errorMessage ?? formError.value)
const isEdit = computed(() => props.item !== null)

watch(
  () => props.open,
  (open) => {
    if (open) reset()
  },
  { immediate: true },
)

function reset(): void {
  const item = props.item
  name.value = item?.name ?? ''
  method.value = item?.method ?? 'GET'
  scheme.value = item?.scheme ?? 'http'
  port.value = item?.port ?? (item?.scheme === 'https' ? 443 : 80)
  path.value = item?.path ?? ''
  // 普通头直接回填；认证头仅回填名称（值留空 = 沿用已保存密文）。
  const init: Record<string, string> = { ...(item?.headers ?? {}) }
  for (const header of item?.auth_headers ?? []) {
    if (init[header] === undefined) init[header] = ''
  }
  headers.value = init
  bodyText.value = item?.body === undefined ? '' : JSON.stringify(item.body, null, 2)
  connectMS.value = item?.timeouts?.connect_ms ?? 0
  readMS.value = item?.timeouts?.read_ms ?? 0
  totalMS.value = item?.timeouts?.total_ms ?? 0
  description.value = item?.description ?? ''
  formError.value = ''
}

/** 解析请求头文本（每行 Name: value）。 */
function onClose(): void {
  if (props.saving) return
  emit('close')
}

function onSubmit(): void {
  if (!name.value.trim()) {
    formError.value = '请填写接口名称'
    return
  }
  if (scheme.value !== 'http' && scheme.value !== 'https') {
    formError.value = '协议仅支持 http / https'
    return
  }
  const portValue = Number(port.value)
  if (!Number.isInteger(portValue) || portValue < 1 || portValue > 65535) {
    formError.value = '端口需为 1~65535 的整数'
    return
  }
  const pathValue = path.value.trim()
  if (!pathValue) {
    formError.value = '请填写接口路径'
    return
  }
  if (!pathValue.startsWith('/')) {
    formError.value = '接口路径必须以 / 开头（如 /api/order/create）'
    return
  }
  if (/\s/.test(pathValue)) {
    formError.value = '接口路径不能包含空白字符'
    return
  }

  const timeouts = {
    connect_ms: Number(connectMS.value) || 0,
    read_ms: Number(readMS.value) || 0,
    total_ms: Number(totalMS.value) || 0,
  }
  for (const [key, value] of Object.entries(timeouts)) {
    if (value < 0 || value > 600000) {
      formError.value = `超时配置 ${key} 需在 0~600000 毫秒之间`
      return
    }
  }

  const payload: InterfacePayload = {
    name: name.value.trim(),
    method: method.value,
    scheme: scheme.value,
    port: portValue,
    path: pathValue,
    headers: headers.value,
    timeouts,
    description: description.value.trim(),
  }

  const bodyRaw = bodyText.value.trim()
  if (bodyRaw) {
    try {
      payload.body = JSON.parse(bodyRaw)
    } catch {
      formError.value = '参数模板必须是合法 JSON'
      return
    }
  }
  if (props.item) payload.version = props.item.version

  formError.value = ''
  emit('submit', payload)
}
</script>

<style scoped>
.method-field {
  max-width: 140px;
}
.method-field select {
  min-width: 0;
}

/* 地址行：协议/端口固定窄，路径自适应；空间不足自动换行，避免重叠。 */
.address-row {
  flex-wrap: wrap;
}
.scheme-field {
  flex: 0 0 108px;
  max-width: 108px;
}
.port-field {
  flex: 0 0 128px;
  max-width: 128px;
}
.path-field {
  flex: 1 1 220px;
  min-width: 200px;
}
.form-section {
  border: 1px solid #e5e7eb;
  border-radius: 10px;
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  background: #fff;
}
.form-section .section-title {
  margin: 0;
  font-size: 13px;
  font-weight: 600;
  color: #1f2937;
  padding-bottom: 6px;
  border-bottom: 1px solid #eef2f7;
}
.import-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 6px 8px;
  border: 1px dashed #c7d2fe;
  border-radius: 8px;
  background: #f8faff;
  font-size: 12px;
}
.headers-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 6px;
}
.header-table input {
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
}
.header-table .col-enable {
  width: 56px;
  text-align: center;
}
.header-table .col-name {
  width: 30%;
}
.header-table .col-op {
  width: 88px;
}
.header-table tr.row-disabled input:not([type='checkbox']) {
  opacity: 0.55;
  text-decoration: line-through;
}
/* 覆盖全局 .ap-field input/select 的 min-width:160px，避免挤出容器造成重叠。 */
.scheme-field select,
.port-field input,
.path-field input {
  min-width: 0;
  width: 100%;
  box-sizing: border-box;
}
</style>
