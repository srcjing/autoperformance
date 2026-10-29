<template>
  <div v-if="open" class="ap-dialog-mask">
    <div class="ap-dialog import-dialog">
      <div class="ap-dialog-header">
        <span>导入请求</span>
        <button type="button" class="ap-link" @click="close">关闭</button>
      </div>
      <div class="ap-dialog-body">
        <p class="ap-muted">
          支持两种粘贴方式：<b>原始 HTTP 报文</b>（请求行 + 请求头 + 空行 + 请求体），或<b>直接粘贴 JSON</b>（仅填充请求体）。
        </p>
        <textarea
          v-model="text"
          rows="12"
          class="import-input"
          placeholder="POST /manage/uiapi/v1/identity/user/create HTTP/1.1&#10;Host: 10.52.78.246:8443&#10;Content-Type: application/json&#10;csrfToken: {{token}}&#10;&#10;{&quot;attrs&quot;:{&quot;ouId&quot;:&quot;local&quot;}}"
        />
        <p v-if="error" class="ap-alert ap-alert-error">{{ error }}</p>
        <p v-if="summary" class="ap-alert ap-alert-hint">{{ summary }}</p>
      </div>
      <div class="ap-dialog-footer">
        <button type="button" class="ap-btn ap-btn-ghost" @click="close">取消</button>
        <button type="button" class="ap-btn ap-btn-primary" @click="parse">识别并填充</button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'

// 导入请求：解析「原始 HTTP 报文」或直接 JSON，回填到表单（方法/地址/请求头/请求体）。
defineProps<{ open: boolean }>()
const emit = defineEmits<{
  (e: 'close'): void
  (e: 'fill', payload: { method?: string; url?: string; headers?: Record<string, string>; body?: string }): void
}>()

const text = ref('')
const error = ref('')
const summary = ref('')

function close(): void {
  emit('close')
}

function parse(): void {
  error.value = ''
  summary.value = ''
  const raw = text.value.trim()
  if (!raw) {
    error.value = '请先粘贴内容'
    return
  }

  // 1) 纯 JSON：只填请求体
  if (raw.startsWith('{') || raw.startsWith('[')) {
    try {
      JSON.parse(raw)
    } catch (err) {
      error.value = `JSON 解析失败：${(err as Error).message}`
      return
    }
    emit('fill', { body: raw })
    summary.value = '已识别为 JSON 请求体'
    text.value = ''
    return
  }

  // 2) 原始 HTTP 报文
  const normalized = raw.replace(/\r\n/g, '\n')
  const sep = normalized.indexOf('\n\n')
  const head = sep >= 0 ? normalized.slice(0, sep) : normalized
  const body = sep >= 0 ? normalized.slice(sep + 2) : ''
  const lines = head.split('\n').filter((line) => line.trim())
  const requestLine = lines.shift() ?? ''
  const match = requestLine.match(/^([A-Za-z]+)\s+(\S+)\s+HTTP\/[\d.]+$/)
  if (!match) {
    error.value = '无法识别请求行，格式应为「POST /path HTTP/1.1」'
    return
  }
  const method = match[1].toUpperCase()
  let target = match[2]

  const headers: Record<string, string> = {}
  for (const line of lines) {
    const pos = line.indexOf(':')
    if (pos <= 0) continue
    const name = line.slice(0, pos).trim()
    const value = line.slice(pos + 1).trim()
    if (!name) continue
    headers[name] = value
  }

  const host = headers.Host || headers.host || ''
  delete headers.Host
  delete headers.host
  delete headers['Content-Length']
  delete headers['content-length']

  let url = target
  if (!/^https?:\/\//i.test(target)) {
    if (!host) {
      error.value = '请求报文缺少 Host 头，无法拼接完整地址'
      return
    }
    const scheme = /:80$/.test(host) ? 'http' : 'https'
    url = `${scheme}://${host}${target.startsWith('/') ? '' : '/'}${target}`
  }

  const trimmedBody = body.trim()
  if (trimmedBody && !trimmedBody.startsWith('{') && !trimmedBody.startsWith('[')) {
    error.value = '请求体不是 JSON；当前仅支持 JSON 请求体（表单类型请手动配置）'
    return
  }

  emit('fill', { method, url, headers, body: trimmedBody })
  summary.value = `已识别：${method} ${url}，请求头 ${Object.keys(headers).length} 个`
  text.value = ''
}
</script>

<style scoped>
.import-dialog {
  width: min(760px, 92vw);
}
.import-input {
  width: 100%;
  box-sizing: border-box;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  line-height: 1.5;
}
</style>
