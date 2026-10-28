<template>
  <div class="step-editor">
    <div class="step-head">
      <span class="ap-field-label">流程步骤 *（按顺序执行，共 {{ steps.length }} 步）</span>
      <button type="button" class="ap-btn ap-btn-ghost ap-btn-sm" :disabled="disabled" @click="addStep">
        + 添加步骤
      </button>
    </div>

    <p class="ap-muted step-hint">
      作用域决定步骤与变量的执行频率：<b>前置</b>（整个测试周期一次，如登录取 token）、<b>主流程</b>（每次迭代执行，构成压测流量）、<b>每请求前置</b>（每个主流程步骤前执行，如刷新签名）。
      路径 / 请求头 / 请求体的值可用 <code v-pre>{{变量名}}</code> 引用上一步提取的变量。
    </p>

    <p v-if="steps.length === 0" class="ap-alert ap-alert-hint">
      流程测试至少需要一个「主流程」步骤；若需要先登录，请先添加一个「前置」步骤并提取 token。
    </p>

    <div v-for="(step, index) in steps" :key="index" class="step-card">
      <div class="step-grid">
        <label class="ap-field">
          <span class="ap-field-label">第 {{ index + 1 }} 步接口 *</span>
          <select :value="step.interface_id" :disabled="disabled" @change="onInterface(index, $event)">
            <option value="">请选择接口</option>
            <option v-for="item in interfaces" :key="item.id" :value="item.id">
              {{ optionLabel(item) }}
            </option>
          </select>
        </label>

        <label class="ap-field">
          <span class="ap-field-label">作用域 *</span>
          <select :value="step.scope" :disabled="disabled" @change="onScope(index, $event)">
            <option v-for="scope in SCOPE_OPTIONS" :key="scope.value" :value="scope.value">
              {{ scope.label }}
            </option>
          </select>
        </label>

        <label class="ap-field">
          <span class="ap-field-label">步骤名</span>
          <input
            :value="step.name ?? ''"
            :disabled="disabled"
            placeholder="如 登录"
            maxlength="64"
            @input="onName(index, $event)"
          />
        </label>

        <div class="step-actions">
          <button type="button" class="ap-btn ap-btn-ghost ap-btn-sm" :disabled="disabled || index === 0" @click="move(index, -1)">
            上移
          </button>
          <button
            type="button"
            class="ap-btn ap-btn-ghost ap-btn-sm"
            :disabled="disabled || index === steps.length - 1"
            @click="move(index, 1)"
          >
            下移
          </button>
          <button type="button" class="ap-btn ap-btn-danger ap-btn-sm" :disabled="disabled" @click="removeStep(index)">
            删除
          </button>
        </div>
      </div>

      <div class="step-grid">
        <label class="ap-field">
          <span class="ap-field-label">路径覆盖（可选，留空用接口默认路径）</span>
          <input
            :value="step.path ?? ''"
            :disabled="disabled"
            placeholder="/api/order?token={{token}}"
            @input="onPath(index, $event)"
          />
        </label>
        <label class="ap-field">
          <span class="ap-field-label">当前可用变量</span>
          <span class="var-list">{{ availableVars(index) || '暂无（上一步提取后可用）' }}</span>
        </label>
      </div>

      <details class="step-extra">
        <summary>请求体 / 请求头覆盖（可选）</summary>
        <label class="ap-field">
          <span class="ap-field-label">请求体（支持占位符）</span>
          <textarea
            :value="step.body ?? ''"
            rows="3"
            :disabled="disabled"
            placeholder='{"token":"{{token}}"}'
            @input="onBody(index, $event)"
          />
        </label>
        <label class="ap-field">
          <span class="ap-field-label">请求头（每行「名称: 值」，值支持占位符）</span>
          <textarea
            :value="headersText(step)"
            rows="2"
            :disabled="disabled"
            placeholder="Authorization: Bearer {{token}}"
            @input="onHeaders(index, $event)"
          />
        </label>
      </details>

      <div class="extract-block">
        <div class="extract-head">
          <span class="ap-field-label">变量提取（从本步响应提取）</span>
          <button type="button" class="ap-btn ap-btn-ghost ap-btn-sm" :disabled="disabled" @click="addRule(index)">
            + 提取变量
          </button>
        </div>
        <div v-for="(rule, ruleIndex) in step.extract ?? []" :key="ruleIndex" class="extract-row">
          <input
            :value="rule.name"
            class="rule-name"
            :disabled="disabled"
            placeholder="变量名"
            maxlength="64"
            @input="onRule(index, ruleIndex, 'name', $event)"
          />
          <select
            :value="rule.source"
            class="rule-source"
            :disabled="disabled"
            @change="onRule(index, ruleIndex, 'source', $event)"
          >
            <option value="jsonpath">JSONPath</option>
            <option value="regex">正则</option>
          </select>
          <input
            :value="rule.expr"
            class="rule-expr"
            :disabled="disabled"
            :placeholder="rule.source === 'regex' ? 'token&quot;:&quot;([^&quot;]+)' : '$.data.token'"
            @input="onRule(index, ruleIndex, 'expr', $event)"
          />
          <input
            v-if="rule.source === 'regex'"
            :value="rule.group ?? 0"
            class="rule-group"
            type="number"
            min="0"
            max="9"
            :disabled="disabled"
            title="捕获组"
            @input="onRule(index, ruleIndex, 'group', $event)"
          />
          <button
            type="button"
            class="ap-btn ap-btn-danger ap-btn-sm"
            :disabled="disabled"
            @click="removeRule(index, ruleIndex)"
          >
            删除
          </button>
        </div>
        <p v-if="(step.extract ?? []).length === 0" class="ap-muted extract-empty">
          未配置提取；后续步骤无法引用本步响应。
        </p>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { ExtractRule, FlowStep, FlowStepScope, InterfaceOption } from '@/types'

// 流程步骤编辑器：步骤顺序 + 作用域 + 请求覆盖 + 响应变量提取（PRD §3.3.7）。
const props = defineProps<{
  modelValue: FlowStep[]
  interfaces: InterfaceOption[]
  disabled?: boolean
}>()

const emit = defineEmits<{ (e: 'update:modelValue', value: FlowStep[]): void }>()

const SCOPE_OPTIONS: { value: FlowStepScope; label: string }[] = [
  { value: 'once_per_test', label: '前置：整个测试周期一次' },
  { value: 'once_per_iteration', label: '主流程：每次迭代执行' },
  { value: 'every_request', label: '每请求前置：每个主流程步骤前' },
]

const steps = computed(() => props.modelValue ?? [])

function emitSteps(next: FlowStep[]): void {
  emit(
    'update:modelValue',
    next.map((step, index) => ({ ...step, seq: index + 1 })),
  )
}

function patchStep(index: number, patch: Partial<FlowStep>): void {
  emitSteps(steps.value.map((step, i) => (i === index ? { ...step, ...patch } : step)))
}

function optionLabel(item: InterfaceOption): string {
  const prefix = item.product_name ? `${item.product_name} · ` : ''
  const suffix = item.method ? ` [${item.method}]` : ''
  return `${prefix}${item.name}${suffix}`
}

function addStep(): void {
  emitSteps([
    ...steps.value,
    { seq: steps.value.length + 1, name: '', interface_id: '', scope: 'once_per_iteration', path: '' },
  ])
}

function removeStep(index: number): void {
  emitSteps(steps.value.filter((_, i) => i !== index))
}

function move(index: number, delta: number): void {
  const target = index + delta
  if (target < 0 || target >= steps.value.length) return
  const next = [...steps.value]
  const [item] = next.splice(index, 1)
  next.splice(target, 0, item)
  emitSteps(next)
}

function onInterface(index: number, event: Event): void {
  patchStep(index, { interface_id: (event.target as HTMLSelectElement).value })
}

function onScope(index: number, event: Event): void {
  patchStep(index, { scope: (event.target as HTMLSelectElement).value as FlowStepScope })
}

function onName(index: number, event: Event): void {
  patchStep(index, { name: (event.target as HTMLInputElement).value })
}

function onPath(index: number, event: Event): void {
  patchStep(index, { path: (event.target as HTMLInputElement).value })
}

function onBody(index: number, event: Event): void {
  patchStep(index, { body: (event.target as HTMLTextAreaElement).value })
}

function headersText(step: FlowStep): string {
  return Object.entries(step.headers ?? {})
    .map(([key, value]) => `${key}: ${value}`)
    .join('\n')
}

function onHeaders(index: number, event: Event): void {
  const headers: Record<string, string> = {}
  for (const line of (event.target as HTMLTextAreaElement).value.split('\n')) {
    const text = line.trim()
    if (!text) continue
    const pos = text.indexOf(':')
    if (pos <= 0) continue
    headers[text.slice(0, pos).trim()] = text.slice(pos + 1).trim()
  }
  patchStep(index, { headers })
}

function addRule(index: number): void {
  const step = steps.value[index]
  emitSteps(
    steps.value.map((item, i) =>
      i === index
        ? { ...item, extract: [...(item.extract ?? []), { name: '', source: 'jsonpath', expr: '', group: 0 }] }
        : item,
    ),
  )
  void step
}

function removeRule(index: number, ruleIndex: number): void {
  emitSteps(
    steps.value.map((item, i) =>
      i === index ? { ...item, extract: (item.extract ?? []).filter((_, r) => r !== ruleIndex) } : item,
    ),
  )
}

function onRule(index: number, ruleIndex: number, field: keyof ExtractRule, event: Event): void {
  const target = event.target as HTMLInputElement | HTMLSelectElement
  const raw = target.value
  emitSteps(
    steps.value.map((item, i) => {
      if (i !== index) return item
			const rules: ExtractRule[] = (item.extract ?? []).map((rule, r) => {
				if (r !== ruleIndex) return rule
				if (field === 'group') return { ...rule, group: Number(raw) || 0 }
				if (field === 'source') return { ...rule, source: raw === 'regex' ? 'regex' : 'jsonpath' }
				if (field === 'name') return { ...rule, name: raw }
				return { ...rule, expr: raw }
			})
      return { ...item, extract: rules }
    }),
  )
}

/** 当前步骤可引用的变量：前置步骤全部 + 排在前面的步骤。 */
function availableVars(index: number): string {
  const names = new Set<string>()
  steps.value.forEach((step, i) => {
    if (i === index) return
    const usable = step.scope === 'once_per_test' ? true : i < index
    if (!usable) return
    for (const rule of step.extract ?? []) {
      if (rule.name.trim()) names.add(rule.name.trim())
    }
  })
  return [...names].join('、')
}
</script>

<style scoped>
.step-editor {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.step-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.step-hint {
  margin: 0;
}
.step-card {
  border: 1px solid var(--ap-border, #e2e8f0);
  border-radius: 8px;
  padding: 12px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  background: var(--ap-surface-muted, #fafbfc);
}
.step-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
  align-items: end;
}
.step-actions {
  display: flex;
  gap: 6px;
  align-self: end;
}
.step-extra summary {
  cursor: pointer;
  font-size: 12px;
  color: var(--ap-text-muted, #64748b);
}
.step-extra {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.var-list {
  font-size: 12px;
  color: var(--ap-text-muted, #64748b);
  word-break: break-all;
}
.extract-block {
  border-top: 1px dashed var(--ap-border, #e2e8f0);
  padding-top: 8px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.extract-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.extract-row {
  display: grid;
  grid-template-columns: 140px 120px minmax(0, 1fr) 72px auto;
  gap: 8px;
  align-items: center;
}
.extract-row input,
.extract-row select {
  width: 100%;
}
.extract-empty {
  margin: 0;
}
@media (max-width: 960px) {
  .step-grid {
    grid-template-columns: 1fr;
  }
  .extract-row {
    grid-template-columns: 1fr 1fr;
  }
}
</style>
