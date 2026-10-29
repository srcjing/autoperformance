<template>
  <div v-if="open" class="ap-dialog-mask">
    <div class="ap-dialog ssh-import" role="dialog" aria-modal="true">
      <div class="ap-dialog-header">
        <span>批量导入 SSH/Host</span>
        <button type="button" class="ap-link" :disabled="saving" @click="emit('close')">关闭</button>
      </div>

      <form class="ap-dialog-body" @submit.prevent="onSubmit">
        <p class="hint">
          支持 JSON 数组或 CSV（首行表头）文件；重复的「类型 + 主机 + 端口」记录自动跳过，
          非法行会被忽略并在导入结果中给出样例。SSH 私钥在导入后加密存储。
        </p>
        <pre class="example">JSON: [{"type":"host","host":"h1.example.com","host_entries":["10.0.0.1 h1.example.com"]}]
CSV : type,host,port,user,auth_type,host_entries,description</pre>

        <label class="ap-field">
          <span class="ap-field-label">导入文件 *（.json / .csv）</span>
          <input type="file" accept=".json,.csv" @change="onFileChange" />
        </label>
        <p v-if="file" class="file-meta ap-muted">已选择：{{ file.name }}</p>

        <div v-if="result" class="result">
          <p class="result-line">
            共 {{ result.total }} 行：新增 <strong>{{ result.created }}</strong> ·
            跳过 <strong>{{ result.skipped }}</strong> ·
            非法 <strong>{{ result.invalid }}</strong>
          </p>
          <ul v-if="result.errors.length > 0" class="result-errors">
            <li v-for="(item, index) in result.errors" :key="index">{{ item }}</li>
          </ul>
        </div>

        <p v-if="localError" class="ap-alert ap-alert-error" role="alert">{{ localError }}</p>
        <p v-else-if="errorMessage" class="ap-alert ap-alert-error" role="alert">{{ errorMessage }}</p>

        <div class="ap-dialog-footer">
          <button type="button" class="ap-btn ap-btn-ghost" :disabled="saving" @click="emit('close')">
            {{ result ? '完成' : '取消' }}
          </button>
          <button type="submit" class="ap-btn ap-btn-primary" :disabled="saving || !file">
            {{ saving ? '导入中…' : '开始导入' }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import type { SshHostImportResult } from '@/types'

// SSH/Host 批量导入弹窗（文件类型前端预检，最终以服务端 24002/10003 为准）。
const props = defineProps<{
  open: boolean
  saving: boolean
  errorMessage: string
  result: SshHostImportResult | null
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'submit', file: File): void
}>()

const ALLOWED_EXT = ['.json', '.csv']

const file = ref<File | null>(null)
const localError = ref('')

watch(
  () => props.open,
  (value) => {
    if (!value) {
      file.value = null
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
  const lower = selected.name.toLowerCase()
  if (!ALLOWED_EXT.some((ext) => lower.endsWith(ext))) {
    localError.value = '仅支持 .json / .csv 文件'
    file.value = null
    input.value = ''
    return
  }
  file.value = selected
}


function onSubmit(): void {
  if (props.saving) return
  if (!file.value) {
    localError.value = '请选择导入文件'
    return
  }
  localError.value = ''
  emit('submit', file.value)
}
</script>

<style scoped>
.ssh-import {
  width: 620px;
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

.example {
  margin: 0;
  padding: 8px 12px;
  border-radius: 6px;
  background: #f3f4f6;
  color: #374151;
  font-size: 11px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
}

.file-meta {
  margin: 0;
  font-size: 12px;
}

.result {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 8px 12px;
  border-radius: 6px;
  background: #f0fdf4;
  font-size: 12px;
}

.result-line {
  margin: 0;
  color: #166534;
}

.result-errors {
  margin: 0;
  padding-left: 18px;
  color: #b91c1c;
}
</style>
