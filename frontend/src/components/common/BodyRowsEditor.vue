<template>
  <div class="body-rows">
    <div class="rows-head">
      <span class="ap-field-label">{{ label || '请求体（键值行，路径支持嵌套如 attrs.ouId / list[0].id）' }}</span>
      <div class="rows-actions">
        <button type="button" class="ap-btn ap-btn-ghost ap-btn-sm" :disabled="disabled" @click="openImport">
          粘贴 JSON 识别
        </button>
        <button type="button" class="ap-btn ap-btn-ghost ap-btn-sm" :disabled="disabled" @click="addRow">
          + 添加参数
        </button>
        <button type="button" class="ap-btn ap-btn-ghost ap-btn-sm" :disabled="disabled" @click="showPreview = !showPreview">
          {{ showPreview ? '收起预览' : '预览 JSON' }}
        </button>
      </div>
    </div>

    <div v-if="variables && variables.length > 0" class="var-hint">
      <span class="ap-muted">可用变量（点击插入到当前输入框）：</span>
      <button
        v-for="name in variables"
        :key="name"
        type="button"
        class="var-chip"
        :disabled="disabled"
        @click="insertVar(name)"
      >
        {{ name }}
      </button>
    </div>

    <div v-if="importOpen" class="import-box">
      <textarea
        v-model="importText"
        rows="4"
        :disabled="disabled"
        placeholder='粘贴 JSON，例如 {"attrs":{"ouId":"local"},"list":[{"id":1}]}'
      />
      <div class="import-actions">
        <button type="button" class="ap-btn ap-btn-primary ap-btn-sm" :disabled="disabled" @click="applyImport">
          识别并填充
        </button>
        <button type="button" class="ap-btn ap-btn-ghost ap-btn-sm" :disabled="disabled" @click="importOpen = false">
          取消
        </button>
        <span v-if="importError" class="import-error">{{ importError }}</span>
      </div>
    </div>

    <table v-if="rows.length > 0" class="ap-table body-table">
      <thead>
        <tr>
          <th class="col-enable">启用</th>
          <th class="col-path">参数路径</th>
          <th class="col-type">类型</th>
          <th>值</th>
          <th class="col-op">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(row, index) in rows" :key="index" :class="{ 'row-disabled': !row.enabled }">
          <td class="col-enable">
            <input v-model="row.enabled" type="checkbox" :disabled="disabled" @change="onChange" />
          </td>
          <td class="col-path">
            <input
              v-model="row.path"
              :disabled="disabled"
              placeholder="attrs.ouId"
              @focus="focusRef = $event.target as HTMLInputElement"
              @input="onChange"
            />
          </td>
          <td class="col-type">
            <select v-model="row.type" :disabled="disabled" @change="onChange">
              <option value="string">字符串</option>
              <option value="number">数字</option>
              <option value="boolean">布尔</option>
              <option value="null">null</option>
              <option value="json">JSON 片段</option>
            </select>
          </td>
          <td>
            <input
              v-model="row.value"
              :disabled="disabled || row.type === 'null'"
              :placeholder="row.type === 'json' ? '{} 或 []' : '值，支持 {{变量}}'"
              @focus="focusRef = $event.target as HTMLInputElement"
              @input="onChange"
            />
            <span v-if="usedVars(row.value).length" class="cell-vars">
              <span v-for="name in usedVars(row.value)" :key="name" class="var-ref">{{ name }}</span>
            </span>
          </td>
          <td class="col-op">
            <button type="button" class="ap-btn ap-btn-danger ap-btn-sm" :disabled="disabled" @click="removeRow(index)">
              删除
            </button>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-else class="ap-muted">暂无请求体参数，可「添加参数」或「粘贴 JSON 识别」。</p>

    <p v-if="error" class="ap-alert ap-alert-error">{{ error }}</p>

    <div v-if="showPreview" class="preview">
      <div class="ap-field-label">生成的 JSON</div>
      <pre class="preview-body">{{ previewText || '（空）' }}</pre>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { buildJson, jsonTextToRows, type BodyRow } from '@/utils/bodyRows'

// 请求体行式编辑器：一行 = 一个叶子值，路径表达嵌套（attrs.ouId / list[0].id）。
const props = defineProps<{
  /** JSON 文本（空串表示无请求体）。 */
  modelValue: string
  disabled?: boolean
  label?: string
  variables?: string[]
}>()

const emit = defineEmits<{ (e: 'update:modelValue', value: string): void }>()

const rows = ref<BodyRow[]>([])
const error = ref('')
const showPreview = ref(false)
const importOpen = ref(false)
const importText = ref('')
const importError = ref('')
const focusRef = ref<HTMLInputElement | null>(null)
const VAR_RE = /\{\{\s*([A-Za-z0-9_]+)\s*\}\}/g

const previewText = computed(() => {
  const result = buildJson(rows.value)
  if (result.error) return ''
  if (result.value === null) return ''
  return JSON.stringify(result.value, null, 2)
})

watch(
  () => props.modelValue,
  (next) => {
    const current = previewText.value
    const target = (next ?? '').trim()
    if (!target && !current) return
    if (target === current) return
    const parsed = jsonTextToRows(target)
    if (parsed.error) {
      // 外部值无法解析（如历史脏数据）时不覆盖用户正在编辑的行
      return
    }
    rows.value = parsed.rows
    error.value = ''
  },
  { immediate: true },
)

function onChange(): void {
  const result = buildJson(rows.value)
  if (result.error) {
    error.value = result.error
    return
  }
  error.value = ''
  const text = result.value === null ? '' : JSON.stringify(result.value, null, 2)
  emit('update:modelValue', text)
}

function addRow(): void {
  rows.value = [...rows.value, { path: '', value: '', type: 'string', enabled: true }]
}

function removeRow(index: number): void {
  rows.value = rows.value.filter((_, i) => i !== index)
  onChange()
}

function openImport(): void {
  importOpen.value = true
  importError.value = ''
}

function applyImport(): void {
  const parsed = jsonTextToRows(importText.value)
  if (parsed.error) {
    importError.value = parsed.error
    return
  }
  rows.value = parsed.rows
  importError.value = ''
  importOpen.value = false
  importText.value = ''
  onChange()
}

function usedVars(text: string): string[] {
  const names = new Set<string>()
  if (!text) return []
  for (const match of text.matchAll(VAR_RE)) names.add(match[1])
  return [...names]
}

function insertVar(name: string): void {
  const token = `{{${name}}}`
  const el = focusRef.value
  if (!el) {
    const last = rows.value[rows.value.length - 1]
    if (last) last.value = `${last.value}${token}`
    onChange()
    return
  }
  const start = el.selectionStart ?? el.value.length
  const end = el.selectionEnd ?? start
  el.value = el.value.slice(0, start) + token + el.value.slice(end)
  el.dispatchEvent(new Event('input'))
  requestAnimationFrame(() => {
    el.focus()
    const pos = start + token.length
    el.setSelectionRange(pos, pos)
  })
}
</script>

<style scoped>
.body-rows {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.rows-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}
.rows-actions {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
}
.var-hint {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  font-size: 12px;
}
.var-chip {
  border: 1px solid #7aa7ff;
  background: #eef3ff;
  color: #1d4ed8;
  border-radius: 12px;
  padding: 1px 10px;
  font-size: 12px;
  cursor: pointer;
}
.var-ref {
  display: inline-block;
  background: #e8f0ff;
  color: #1d4ed8;
  border: 1px solid #b9ccff;
  border-radius: 4px;
  padding: 0 4px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  margin-right: 4px;
}
.cell-vars {
  display: block;
  margin-top: 2px;
}
.import-box {
  border: 1px dashed #c7d2fe;
  border-radius: 8px;
  padding: 8px;
  background: #f8faff;
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.import-box textarea {
  width: 100%;
  box-sizing: border-box;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
}
.import-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
.import-error {
  color: #b42318;
  font-size: 12px;
}
.body-table input,
.body-table select {
  width: 100%;
  min-width: 0;
  box-sizing: border-box;
}
.body-table .col-enable {
  width: 56px;
  text-align: center;
}
.body-table .col-path {
  width: 26%;
}
.body-table .col-type {
  width: 110px;
}
.body-table .col-op {
  width: 88px;
}
.body-table tr.row-disabled input:not([type='checkbox']):not(:disabled) {
  opacity: 0.55;
  text-decoration: line-through;
}
.preview {
  border-top: 1px dashed #e2e8f0;
  padding-top: 6px;
}
.preview-body {
  margin: 4px 0 0;
  max-height: 220px;
  overflow: auto;
  background: #f8fafc;
  border: 1px solid #e2e8f0;
  border-radius: 6px;
  padding: 8px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-all;
}
</style>
