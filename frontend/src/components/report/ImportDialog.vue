<template>
  <div v-if="open" class="ap-dialog-mask">
    <div class="ap-dialog import-dialog" role="dialog" aria-label="导入报告">
      <div class="ap-dialog-header">
        <span>导入报告</span>
        <button type="button" class="ap-link" :disabled="saving" @click="emit('close')">关闭</button>
      </div>

      <form class="ap-dialog-body" @submit.prevent="onSubmit">
        <p class="hint">
          支持 HTML / PDF / DOCX；服务端会校验扩展名与文件内容（magic bytes）并限制大小，
          文件以随机名归档。无归属任务的导入将标记为「外部导入」。
        </p>

        <label class="ap-field">
          <span class="ap-field-label">报告文件（必填）</span>
          <input type="file" accept=".html,.htm,.pdf,.docx" @change="onFileChange" />
        </label>
        <p v-if="file" class="file-meta ap-muted">
          已选择：{{ file.name }}（{{ formatBytes(file.size) }}）
        </p>

        <div class="ap-form-row">
          <label class="ap-field">
            <span class="ap-field-label">归属任务 ID（可选）</span>
            <input v-model="taskId" placeholder="留空为孤立归档" inputmode="numeric" />
          </label>
          <label class="ap-field">
            <span class="ap-field-label">归属产品 ID（可选）</span>
            <input v-model="productId" placeholder="可选" inputmode="numeric" />
          </label>
        </div>

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
import { formatBytes } from '@/utils/report'

// 报告导入弹窗（DES-T-001）：文件扩展名前端预校验，最终以服务端双校验为准。
const props = defineProps<{
  open: boolean
  saving: boolean
  errorMessage: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'submit', payload: { file: File; taskId: string; productId: string }): void
}>()

const ALLOWED_EXT = ['.html', '.htm', '.pdf', '.docx']

const file = ref<File | null>(null)
const taskId = ref('')
const productId = ref('')
const localError = ref('')

watch(
  () => props.open,
  (value) => {
    if (!value) {
      file.value = null
      taskId.value = ''
      productId.value = ''
      localError.value = ''
    }
  },
)

function onFileChange(event: Event): void {
  const input = event.target as HTMLInputElement
  const selected = input.files?.[0] ?? null
  localError.value = ''
  if (!selected) {
    file.value = null
    return
  }
  const name = selected.name.toLowerCase()
  if (!ALLOWED_EXT.some((ext) => name.endsWith(ext))) {
    localError.value = '仅支持 .html / .htm / .pdf / .docx 文件'
    file.value = null
    input.value = ''
    return
  }
  file.value = selected
}


function onSubmit(): void {
  if (props.saving) return
  if (!file.value) {
    localError.value = '请选择要导入的报告文件'
    return
  }
  const task = taskId.value.trim()
  const product = productId.value.trim()
  if (task && !/^\d+$/.test(task)) {
    localError.value = '归属任务 ID 必须为数字'
    return
  }
  if (product && !/^\d+$/.test(product)) {
    localError.value = '归属产品 ID 必须为数字'
    return
  }
  localError.value = ''
  emit('submit', { file: file.value, taskId: task, productId: product })
}
</script>

<style scoped>
.import-dialog {
  width: 560px;
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
