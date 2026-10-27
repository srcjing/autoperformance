<template>
  <div v-if="open && cert" class="ap-dialog-mask" @click.self="onMaskClick">
    <div class="ap-dialog reinit-dialog" role="dialog" aria-modal="true">
      <div class="ap-dialog-header">
        <span>重新初始化内置证书</span>
        <button type="button" class="ap-link" :disabled="saving" @click="emit('close')">关闭</button>
      </div>

      <div class="ap-dialog-body">
        <p class="hint">
          重新初始化仅更新该内置自签证书的有效期（自今日起顺延一年），不会重签证书、不更换私钥。
          证书内容与私钥保持加密存储。
        </p>

        <div class="cert-summary">
          <div class="summary-item">
            <span class="summary-label">证书名称</span>
            <span>{{ cert.name }}</span>
          </div>
          <div class="summary-item">
            <span class="summary-label">当前有效期</span>
            <span>{{ cert.valid_from || '-' }} → {{ cert.valid_to || '-' }}</span>
          </div>
          <div class="summary-item">
            <span class="summary-label">当前状态</span>
            <span class="ap-badge" :class="certStatusMeta(cert.status).badge">
              {{ certStatusMeta(cert.status).label }}
            </span>
          </div>
        </div>

        <p v-if="errorMessage" class="ap-alert ap-alert-error" role="alert">{{ errorMessage }}</p>

        <div class="ap-dialog-footer">
          <button type="button" class="ap-btn ap-btn-ghost" :disabled="saving" @click="emit('close')">
            取消
          </button>
          <button type="button" class="ap-btn ap-btn-primary" :disabled="saving" @click="emit('confirm')">
            {{ saving ? '初始化中…' : '确认重新初始化' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { CertificateInfo } from '@/types'
import { certStatusMeta } from '@/utils/setting'

// 内置证书重新初始化确认（非内置证书由服务端返回 10401，前端不展示该入口）。
const props = defineProps<{
  open: boolean
  cert: CertificateInfo | null
  saving: boolean
  errorMessage: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'confirm'): void
}>()

function onMaskClick(): void {
  if (!props.saving) emit('close')
}
</script>

<style scoped>
.reinit-dialog {
  width: 520px;
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

.cert-summary {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.summary-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  font-size: 13px;
}

.summary-label {
  color: #6b7280;
}
</style>
