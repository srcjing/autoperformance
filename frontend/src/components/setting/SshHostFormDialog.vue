<template>
  <div v-if="open" class="ap-dialog-mask">
    <div class="ap-dialog ssh-form" role="dialog" aria-modal="true">
      <div class="ap-dialog-header">
        <span>{{ isEdit ? '编辑 SSH/Host' : '添加 SSH/Host' }}</span>
        <button type="button" class="ap-link" :disabled="saving" @click="emit('close')">关闭</button>
      </div>

      <form class="ap-dialog-body" @submit.prevent="onSubmit">
        <div class="ap-form-row">
          <label class="ap-field">
            <span class="ap-field-label">记录类型 *</span>
            <select v-model="type" :disabled="saving" @change="onTypeChange">
              <option value="ssh">SSH 信任（远程执行）</option>
              <option value="host">Host 解析（域名 ↔ IP）</option>
            </select>
          </label>
          <label class="ap-field">
            <span class="ap-field-label">主机名 / IP *</span>
            <input v-model="host" :disabled="saving" maxlength="128" placeholder="如：10.0.0.8 / api.example.com" />
          </label>
        </div>

        <template v-if="type === 'ssh'">
          <div class="ap-form-row">
            <label class="ap-field">
              <span class="ap-field-label">SSH 端口</span>
              <input v-model.number="port" type="number" min="1" max="65535" :disabled="saving" placeholder="默认 22" />
            </label>
            <label class="ap-field">
              <span class="ap-field-label">SSH 用户 *</span>
              <input v-model="user" :disabled="saving" maxlength="64" placeholder="如：autoperf" />
            </label>
            <label class="ap-field">
              <span class="ap-field-label">认证方式 *</span>
              <select v-model="authType" :disabled="saving">
                <option value="key">密钥（推荐）</option>
                <option value="password">密码</option>
              </select>
            </label>
          </div>

          <label v-if="authType === 'key'" class="ap-field">
            <span class="ap-field-label">
              SSH 私钥{{ isEdit ? '（留空表示保持原私钥）' : ' *' }}
            </span>
            <textarea
              v-model="keyText"
              :disabled="saving"
              rows="5"
              spellcheck="false"
              placeholder="-----BEGIN OPENSSH PRIVATE KEY-----&#10;...&#10;-----END OPENSSH PRIVATE KEY-----"
            />
          </label>
          <p v-if="isEdit && item?.has_key" class="ap-alert ap-alert-hint">
            已保存加密私钥；留空提交即沿用原私钥（页面不回显密钥内容）。
          </p>
        </template>

        <label class="ap-field">
          <span class="ap-field-label">
            Host 解析内容{{ type === 'host' ? ' *' : '（可选）' }}
          </span>
          <textarea
            v-model="entriesText"
            :disabled="saving"
            rows="3"
            placeholder="每行一条：10.0.0.11 host-a.example.com host-a"
          />
          <span class="ap-muted">格式：IP/域名 + 空格 + 映射域名（可多条域名），不支持任何 shell 字符。</span>
        </label>

        <label class="ap-field">
          <span class="ap-field-label">说明</span>
          <textarea v-model="description" :disabled="saving" rows="2" maxlength="500" />
        </label>

        <p v-if="displayError" class="ap-alert ap-alert-error" role="alert">{{ displayError }}</p>
        <p v-else-if="isEdit" class="ap-alert ap-alert-hint">
          当前版本号 {{ item?.version }}：保存时若已被他人修改会提示并发冲突，请刷新后重试。
        </p>

        <div class="ap-dialog-footer">
          <button type="button" class="ap-btn ap-btn-ghost" :disabled="saving" @click="emit('close')">
            取消
          </button>
          <button type="submit" class="ap-btn ap-btn-primary" :disabled="saving">
            {{ saving ? '保存中…' : '保存' }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { SshAuthType, SshHostInfo, SshHostPayload, SshHostType } from '@/types'

// SSH/Host 表单（type=host|ssh；SSH 私钥仅上行，编辑时留空即沿用原密文）。
const props = defineProps<{
  open: boolean
  item: SshHostInfo | null
  saving: boolean
  /** 服务端返回的业务错误（10302/10402 等），由列表页透传。 */
  errorMessage?: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'submit', payload: SshHostPayload): void
}>()

const type = ref<SshHostType>('ssh')
const host = ref('')
const port = ref<number | ''>(22)
const user = ref('')
const authType = ref<SshAuthType>('key')
const keyText = ref('')
const entriesText = ref('')
const description = ref('')
const formError = ref('')

const isEdit = computed(() => props.item !== null)
const displayError = computed(() => props.errorMessage ?? formError.value)

watch(
  () => props.open,
  (open) => {
    if (open) reset()
  },
  { immediate: true },
)

function reset(): void {
  const item = props.item
  type.value = item?.type ?? 'ssh'
  host.value = item?.host ?? ''
  port.value = item?.port || 22
  user.value = item?.user ?? ''
  authType.value = item?.auth_type === 'password' ? 'password' : 'key'
  keyText.value = '' // 密钥永不回显
  entriesText.value = (item?.host_entries ?? []).join('\n')
  description.value = item?.description ?? ''
  formError.value = ''
}

function onTypeChange(): void {
  if (type.value === 'host') {
    port.value = ''
    user.value = ''
    authType.value = 'key'
    keyText.value = ''
  } else if (!port.value) {
    port.value = 22
  }
}


function onSubmit(): void {
  if (!host.value.trim()) {
    formError.value = '请填写主机名/IP'
    return
  }

  const entries = entriesText.value
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line.length > 0)

  if (type.value === 'host' && entries.length === 0) {
    formError.value = 'Host 解析记录至少填写一条解析内容'
    return
  }
  if (type.value === 'ssh' && !user.value.trim()) {
    formError.value = 'SSH 类型必须填写 SSH 用户'
    return
  }
  const portValue = Number(port.value || 0)
  if (type.value === 'ssh' && (portValue < 1 || portValue > 65535)) {
    formError.value = 'SSH 端口需在 1~65535 之间'
    return
  }
  if (type.value === 'ssh' && authType.value === 'key' && !isEdit.value && !keyText.value.trim()) {
    formError.value = '密钥认证需粘贴 SSH 私钥'
    return
  }

  const payload: SshHostPayload = {
    type: type.value,
    host: host.value.trim(),
    description: description.value.trim(),
    host_entries: entries,
  }
  if (type.value === 'ssh') {
    payload.port = portValue
    payload.user = user.value.trim()
    payload.auth_type = authType.value
    const key = keyText.value.trim()
    if (authType.value === 'key' && key) payload.key = `${key}\n`
  } else {
    payload.auth_type = ''
  }
  if (props.item) payload.version = props.item.version

  formError.value = ''
  emit('submit', payload)
}
</script>

<style scoped>
.ssh-form {
  width: 680px;
}
</style>
