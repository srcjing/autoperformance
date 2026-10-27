<template>
  <div v-if="open" class="ap-dialog-mask" @click.self="onMaskClick">
    <div class="ap-dialog cert-import" role="dialog" aria-modal="true">
      <div class="ap-dialog-header">
        <span>导入证书</span>
        <button type="button" class="ap-link" :disabled="saving" @click="emit('close')">关闭</button>
      </div>

      <form class="ap-dialog-body" @submit.prevent="onSubmit">
        <p class="hint">
          证书与私钥文件内容由服务端 AES-256-GCM 信封加密存储，接口响应与页面均不回显密钥材料；
          有效期留空时按证书内容自动回填。
        </p>

        <label class="ap-field">
          <span class="ap-field-label">证书文件 *（PEM / CRT / CER）</span>
          <input type="file" accept=".pem,.crt,.cer" @change="onCertChange" />
        </label>
        <p v-if="certFile" class="file-meta ap-muted">已选择：{{ certFile.name }}</p>

        <label class="ap-field">
          <span class="ap-field-label">私钥文件（可选，PEM；导入后加密保存）</span>
          <input type="file" accept=".pem,.key" @change="onKeyChange" />
        </label>
        <p v-if="keyFile" class="file-meta ap-muted">已选择：{{ keyFile.name }}</p>

        <div class="ap-form-row">
          <label class="ap-field">
            <span class="ap-field-label">证书名称 *</span>
            <input v-model="name" :disabled="saving" maxlength="128" placeholder="如：网关服务证书" />
          </label>
          <label class="ap-field">
            <span class="ap-field-label">颁发机构</span>
            <input v-model="issuer" :disabled="saving" maxlength="128" placeholder="CA 名称" />
          </label>
        </div>

        <div class="ap-form-row">
          <label class="ap-field">
            <span class="ap-field-label">生效日期</span>
            <input v-model="validFrom" type="date" :disabled="saving" />
          </label>
          <label class="ap-field">
            <span class="ap-field-label">失效日期</span>
            <input v-model="validTo" type="date" :disabled="saving" />
          </label>
        </div>

        <label class="ap-field">
          <span class="ap-field-label">关联服务</span>
          <input v-model="associatedService" :disabled="saving" maxlength="128" placeholder="如：api-gateway" />
        </label>

        <p v-if="localError" class="ap-alert ap-alert-error" role="alert">{{ localError }}</p>
        <p v-else-if="errorMessage" class="ap-alert ap-alert-error" role="alert">{{ errorMessage }}</p>

        <div class="ap-dialog-footer">
          <button type="button" class="ap-btn ap-btn-ghost" :disabled="saving" @click="emit('close')">
            取消
          </button>
          <button type="submit" class="ap-btn ap-btn-primary" :disabled="saving">
            {{ saving ? '导入中…' : '导入' }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import type { CertificateImportPayload } from '@/types'

// 证书导入弹窗（前端仅做文件扩展名预检，合法性以服务端 10003 为准）。
const props = defineProps<{
  open: boolean
  saving: boolean
  errorMessage: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'submit', payload: CertificateImportPayload): void
}>()

const CERT_EXT = ['.pem', '.crt', '.cer']
const KEY_EXT = ['.pem', '.key']

const certFile = ref<File | null>(null)
const keyFile = ref<File | null>(null)
const name = ref('')
const issuer = ref('')
const validFrom = ref('')
const validTo = ref('')
const associatedService = ref('')
const localError = ref('')

watch(
  () => props.open,
  (value) => {
    if (!value) reset()
  },
)

function reset(): void {
  certFile.value = null
  keyFile.value = null
  name.value = ''
  issuer.value = ''
  validFrom.value = ''
  validTo.value = ''
  associatedService.value = ''
  localError.value = ''
}

function onCertChange(event: Event): void {
  const input = event.target as HTMLInputElement
  const selected = input.files?.[0] ?? null
  localError.value = ''
  if (!selected) {
    certFile.value = null
    return
  }
  const lower = selected.name.toLowerCase()
  if (!CERT_EXT.some((ext) => lower.endsWith(ext))) {
    localError.value = '证书文件仅支持 .pem / .crt / .cer'
    certFile.value = null
    input.value = ''
    return
  }
  certFile.value = selected
}

function onKeyChange(event: Event): void {
  const input = event.target as HTMLInputElement
  const selected = input.files?.[0] ?? null
  localError.value = ''
  if (!selected) {
    keyFile.value = null
    return
  }
  const lower = selected.name.toLowerCase()
  if (!KEY_EXT.some((ext) => lower.endsWith(ext))) {
    localError.value = '私钥文件仅支持 .pem / .key'
    keyFile.value = null
    input.value = ''
    return
  }
  keyFile.value = selected
}

function onMaskClick(): void {
  if (!props.saving) emit('close')
}

function onSubmit(): void {
  if (props.saving) return
  if (!certFile.value) {
    localError.value = '请选择证书文件'
    return
  }
  if (!name.value.trim()) {
    localError.value = '请填写证书名称'
    return
  }
  if (validFrom.value && validTo.value && validTo.value < validFrom.value) {
    localError.value = '失效日期不能早于生效日期'
    return
  }
  localError.value = ''
  emit('submit', {
    name: name.value.trim(),
    issuer: issuer.value.trim() || undefined,
    valid_from: validFrom.value || undefined,
    valid_to: validTo.value || undefined,
    associated_service: associatedService.value.trim() || undefined,
    cert_file: certFile.value,
    key_file: keyFile.value,
  })
}
</script>

<style scoped>
.cert-import {
  width: 580px;
}

.hint {
  margin: 0;
  padding: 8px 12px;
  border-radius: 6px;
  background: #f9fafb;
  color: #6b7280;
  font-size: 12px;
  line-height: 1.6;
}

.file-meta {
  margin: 0;
  font-size: 12px;
}
</style>
