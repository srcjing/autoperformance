<template>
  <div class="header-rows">
    <div class="rows-head">
      <span class="ap-field-label">{{ label }}</span>
      <div class="rows-actions">
        <button
          v-if="refCount > 0"
          type="button"
          class="ap-btn ap-btn-ghost ap-btn-sm"
          :class="{ 'is-on': onlyVars }"
          :disabled="disabled"
          @click="onlyVars = !onlyVars"
        >
          {{ onlyVars ? '显示全部' : `仅看变量（${refCount}）` }}
        </button>
        <button type="button" class="ap-btn ap-btn-ghost ap-btn-sm" :disabled="disabled" @click="importOpen = !importOpen">
          粘贴请求头识别
        </button>
        <button type="button" class="ap-btn ap-btn-ghost ap-btn-sm" :disabled="disabled" @click="addRow">
          + 添加请求头
        </button>
      </div>
    </div>

    <div v-if="importOpen" class="import-box">
      <textarea
        v-model="importText"
        rows="4"
        :disabled="disabled"
        placeholder="Content-Type: application/json&#10;csrfToken: {{token}}"
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

    <div class="var-hint">
      <span class="ap-muted">内置动态值（每次请求重新生成，点击插入）：</span>
      <button
        v-for="item in DYNAMIC_VALUE_CHIPS"
        :key="item.token"
        type="button"
        class="var-chip dyn-chip"
        :disabled="disabled"
        :title="item.hint"
        @click="insertRaw(item.token)"
      >
        {{ item.label }}
      </button>
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

    <table v-if="rows.length > 0" class="ap-table header-table">
      <thead>
        <tr>
          <th class="col-enable">启用</th>
          <th class="col-name">键（Header 名）</th>
          <th>值</th>
          <th class="col-op">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="entry in visibleRows"
          :key="entry.index"
          :class="{
            'row-disabled': !entry.row.enabled,
            'row-var': hasUserVar(entry.row.value),
            'row-dyn': !hasUserVar(entry.row.value) && hasDynamic(entry.row.value),
          }"
        >
          <td class="col-enable">
            <input v-model="entry.row.enabled" type="checkbox" :disabled="disabled" @change="emitRows" />
          </td>
          <td class="col-name">
            <input
              v-model="entry.row.name"
              :disabled="disabled"
              placeholder="Content-Type"
              list="ap-common-headers"
              @focus="focusRef = $event.target as HTMLInputElement"
              @input="emitRows"
            />
            <span v-if="hasUserVar(entry.row.value)" class="ref-badge var">变量</span>
            <span v-else-if="hasDynamic(entry.row.value)" class="ref-badge dyn">动态</span>
          </td>
          <td>
            <input
              v-model="entry.row.value"
              :disabled="disabled"
              placeholder="值，支持 {{变量}}"
              @focus="focusRef = $event.target as HTMLInputElement"
              @input="emitRows"
            />
            <span v-if="usedVars(entry.row.value).length" class="cell-vars">
              <span v-for="name in usedVars(entry.row.value)" :key="name" class="var-ref">{{ name }}</span>
            </span>
          </td>
          <td class="col-op">
            <button type="button" class="ap-btn ap-btn-danger ap-btn-sm" :disabled="disabled" @click="removeRow(entry.index)">
              删除
            </button>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-else class="ap-muted">暂无请求头，点击「添加请求头」新增。</p>
    <p v-if="onlyVars && rows.length > 0 && visibleRows.length === 0" class="ap-muted">
      当前没有引用变量或动态值的请求头。
    </p>

    <datalist id="ap-common-headers">
      <option value="Content-Type" />
      <option value="Accept" />
      <option value="Authorization" />
      <option value="Cookie" />
      <option value="csrfToken" />
      <option value="X-Api-Key" />
      <option value="User-Agent" />
      <option value="Referer" />
      <option value="Origin" />
    </datalist>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { DYNAMIC_VALUE_CHIPS } from '@/utils/testing'

// 请求头行式编辑器（Postman 风格：启用 / 键 / 值），值支持 {{变量}} 与点击插入。
const props = defineProps<{
  modelValue: Record<string, string>
  disabled?: boolean
  label?: string
  /** 可插入的变量名（流程步骤场景传入）。 */
  variables?: string[]
}>()

const emit = defineEmits<{ (e: 'update:modelValue', value: Record<string, string>): void }>()

interface HeaderRow {
  name: string
  value: string
  enabled: boolean
}

const rows = ref<HeaderRow[]>([])
const focusRef = ref<HTMLInputElement | null>(null)
const importOpen = ref(false)
const onlyVars = ref(false)

function hasUserVar(value: string): boolean {
  return /\{\{\s*[A-Za-z0-9_]+\s*\}\}/.test(value || '')
}

function hasDynamic(value: string): boolean {
  return /\{\{\s*\$/.test(value || '')
}

const refCount = computed(() => rows.value.filter((row) => hasUserVar(row.value) || hasDynamic(row.value)).length)

const visibleRows = computed(() =>
  rows.value
    .map((row, index) => ({ row, index }))
    .filter((entry) => !onlyVars.value || hasUserVar(entry.row.value) || hasDynamic(entry.row.value)),
)
const importText = ref('')
const importError = ref('')
const VAR_RE = /\{\{\s*([A-Za-z0-9_]+)\s*\}\}/g

function fromModel(model: Record<string, string> | undefined): HeaderRow[] {
  return Object.entries(model ?? {}).map(([name, value]) => ({ name, value, enabled: true }))
}

watch(
  () => props.modelValue,
  (next) => {
    // 外部（导入/回显）变化时重建行；内容一致时不打扰输入。
    const current = build(currentRows())
    const target = build(fromModel(next))
    if (JSON.stringify(current) !== JSON.stringify(target)) {
      rows.value = fromModel(next)
    }
  },
  { immediate: true, deep: true },
)

function currentRows(): HeaderRow[] {
  return rows.value
}

function build(list: HeaderRow[]): Record<string, string> {
  const out: Record<string, string> = {}
  for (const row of list) {
    if (!row.enabled) continue
    const name = row.name.trim()
    if (!name) continue
    out[name] = row.value
  }
  return out
}

function emitRows(): void {
  emit('update:modelValue', build(rows.value))
}

function addRow(): void {
  rows.value = [...rows.value, { name: '', value: '', enabled: true }]
}

function removeRow(index: number): void {
  rows.value = rows.value.filter((_, i) => i !== index)
  emitRows()
}

/** 粘贴请求头识别：每行 `Name: value`（忽略空行与 # 注释），合并到现有头。 */
function applyImport(): void {
  const parsed: Record<string, string> = {}
  for (const rawLine of importText.value.replace(/\r\n/g, '\n').split('\n')) {
    const line = rawLine.trim()
    if (!line || line.startsWith('#') || line.startsWith('//')) continue
    const pos = line.indexOf(':')
    if (pos <= 0) {
      importError.value = `无法识别该行（需 Name: value）：${line.slice(0, 40)}`
      return
    }
    const name = line.slice(0, pos).trim()
    const value = line.slice(pos + 1).trim()
    if (!name) {
      importError.value = '请求头名称不能为空'
      return
    }
    parsed[name] = value
  }
  if (Object.keys(parsed).length === 0) {
    importError.value = '未识别到有效请求头'
    return
  }
  const merged: Record<string, string> = {}
  for (const row of rows.value) {
    const name = row.name.trim()
    if (name) merged[name] = row.value
  }
  Object.assign(merged, parsed)
  rows.value = Object.entries(merged).map(([name, value]) => ({ name, value, enabled: true }))
  importError.value = ''
  importOpen.value = false
  importText.value = ''
  emitRows()
}

function usedVars(text: string): string[] {
  const names = new Set<string>()
  if (!text) return []
  for (const match of text.matchAll(VAR_RE)) names.add(match[1])
  for (const match of text.matchAll(/\{\{\s*\$([A-Za-z][A-Za-z0-9]*)/g)) names.add(`$${match[1]}`)
  return [...names]
}

function insertRaw(token: string): void {
  const el = focusRef.value
  if (!el) {
    const last = rows.value[rows.value.length - 1]
    if (last) { last.value = `${last.value}${token}` }
    emitRows()
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

function insertVar(name: string): void {
  const token = `{{${name}}}`
  const el = focusRef.value
  if (!el) {
    // 未聚焦时追加到最后一个值输入
    const last = rows.value[rows.value.length - 1]
    if (last) last.value = `${last.value}${token}`
    emitRows()
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
.header-rows {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.rows-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.rows-actions {
  display: flex;
  gap: 6px;
  flex-wrap: wrap;
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
.rows-actions .is-on {
  border-color: #2563eb;
  color: #1d4ed8;
  background: #eef3ff;
}
.ref-badge {
  display: inline-block;
  padding: 0 6px;
  border-radius: 10px;
  font-size: 11px;
  line-height: 18px;
  white-space: nowrap;
  margin-left: 4px;
}
.ref-badge.var {
  background: #e8f0ff;
  color: #1d4ed8;
  border: 1px solid #b9ccff;
}
.ref-badge.dyn {
  background: #fff6e8;
  color: #b45309;
  border: 1px solid #f5c26b;
}
.header-table tbody tr.row-var > td:first-child {
  box-shadow: inset 3px 0 0 #2563eb;
}
.header-table tbody tr.row-dyn > td:first-child {
  box-shadow: inset 3px 0 0 #f59e0b;
}
.header-table tbody tr.row-var {
  background: #f7faff;
}
.header-table tbody tr.row-dyn {
  background: #fffcf5;
}
.header-table tbody tr.row-var td.col-name,
.header-table tbody tr.row-dyn td.col-name {
  display: flex;
  align-items: center;
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
</style>
