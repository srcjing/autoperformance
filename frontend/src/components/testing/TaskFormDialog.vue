<template>
  <div v-if="open" class="ap-dialog-mask" @click.self="onClose">
    <div class="ap-dialog ap-dialog-wide" role="dialog" aria-modal="true">
      <div class="ap-dialog-header">
        <span>新建测试任务</span>
        <button type="button" class="ap-link" :disabled="saving" @click="onClose">关闭</button>
      </div>

      <form class="ap-dialog-body" @submit.prevent="onSubmit">
        <p class="ap-alert ap-alert-hint">
          创建时对「接口范围 + 阈值」做快照，后续接口/阈值修改不影响本任务（历史报告可复现）。
          同一环境同一时刻仅允许一个任务压测（环境互斥）。
        </p>

        <div class="ap-form-row">
          <label class="ap-field">
            <span class="ap-field-label">任务名称 *</span>
            <input
              v-model="name"
              :disabled="saving"
              maxlength="128"
              placeholder="如：订单接口 500 并发回归"
            />
          </label>
          <label class="ap-field">
            <span class="ap-field-label">基于模板</span>
            <select :value="templateId" :disabled="saving" @change="onTemplateChange">
              <option value="">不使用模板</option>
              <option v-for="template in templates" :key="template.id" :value="template.id">
                {{ template.name }}
              </option>
            </select>
          </label>
        </div>

        <p v-if="templateHint" class="ap-alert ap-alert-hint">{{ templateHint }}</p>

        <section class="cfg-section">
          <h3 class="cfg-title">1. 被测环境 *</h3>
          <EnvPicker
            v-model="environmentId"
            label="环境（测试中/已下线环境不可选）"
            :disabled="saving"
          />
        </section>

        <section class="cfg-section">
          <h3 class="cfg-title">2. 接口范围 *（可跨产品多接口）</h3>
          <InterRangePicker
            v-model="interfaceIds"
            v-model:product-ids="productIds"
            :disabled="saving"
          />
          <p class="ap-muted">
            已选 {{ interfaceIds.length }} 个接口；创建时固化接口与阈值快照，接口上限 500 个。
          </p>
        </section>

        <section class="cfg-section">
          <h3 class="cfg-title">3. 测试流程参数</h3>
          <FlowConfig v-model="flow" :disabled="saving" />
        </section>

        <section class="cfg-section">
          <h3 class="cfg-title">4. 报告输出</h3>
          <ReportConfig v-model="reportConfig" :disabled="saving" />
        </section>

        <section class="cfg-section">
          <h3 class="cfg-title">5. 环境清理</h3>
          <CleanConfig v-model="cleanConfig" :disabled="saving" />
        </section>

        <label class="check">
          <input v-model="autoStart" type="checkbox" :disabled="saving" />
          <span>创建后立即执行（auto_start；需后端编排器就绪）</span>
        </label>

        <p v-if="displayError" class="ap-alert ap-alert-error" role="alert">{{ displayError }}</p>

        <div class="ap-dialog-footer">
          <button type="button" class="ap-btn ap-btn-ghost" :disabled="saving" @click="onClose">
            取消
          </button>
          <button type="submit" class="ap-btn ap-btn-primary" :disabled="saving">
            {{ saving ? '创建中…' : autoStart ? '创建并执行' : '创建任务' }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import CleanConfig from './CleanConfig.vue'
import EnvPicker from './EnvPicker.vue'
import FlowConfig from './FlowConfig.vue'
import InterRangePicker from './InterRangePicker.vue'
import ReportConfig from './ReportConfig.vue'
import {
  createDefaultCleanConfig,
  createDefaultFlow,
  createDefaultReportConfig,
  normalizeCleanConfig,
  normalizeFlow,
  normalizeReportConfig,
  toCleanPayload,
  toFlowPayload,
  validateCleanConfig,
  validateFlow,
  validateReportConfig,
} from '@/utils/testing'
import type {
  CleanConfig as CleanConfigModel,
  CreateTaskPayload,
  FlowConfig as FlowConfigModel,
  ReportConfig as ReportConfigModel,
  TestTemplateInfo,
} from '@/types'

// 任务创建弹窗（五要素：环境 / 接口范围 / 流程 / 报告 / 清理 + 可选模板）。
// 模板预填后可覆盖，不影响原模板；提交结构与 api-contract §2.4 POST /tasks 一致。
const props = defineProps<{
  open: boolean
  templates: TestTemplateInfo[]
  /** 从模板列表「创建任务」跳转时带入的模板 ID。 */
  initialTemplateId?: string
  saving: boolean
  errorMessage?: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'submit', payload: CreateTaskPayload): void
}>()

const name = ref('')
const templateId = ref('')
const environmentId = ref('')
const interfaceIds = ref<string[]>([])
const productIds = ref<string[]>([])
const flow = ref<FlowConfigModel>(createDefaultFlow())
const reportConfig = ref<ReportConfigModel>(createDefaultReportConfig())
const cleanConfig = ref<CleanConfigModel>(createDefaultCleanConfig())
const autoStart = ref(false)
const formError = ref('')
const templateHint = ref('')

const displayError = computed(() => props.errorMessage || formError.value)

watch(
  () => props.open,
  (open) => {
    if (open) reset()
  },
  { immediate: true },
)

// 模板列表异步到位后补一次预填（例如从模板页跳转时列表尚未加载完）。
watch(
  () => props.templates,
  () => {
    if (props.open && templateId.value && !templateHint.value) applyTemplate(templateId.value)
  },
)

function reset(): void {
  name.value = ''
  templateId.value = props.initialTemplateId ?? ''
  environmentId.value = ''
  interfaceIds.value = []
  productIds.value = []
  flow.value = createDefaultFlow()
  reportConfig.value = createDefaultReportConfig()
  cleanConfig.value = createDefaultCleanConfig()
  autoStart.value = false
  formError.value = ''
  templateHint.value = ''
  if (templateId.value) applyTemplate(templateId.value)
}

/** 用模板参数预填表单（参数可覆盖，不影响原模板）。 */
function applyTemplate(id: string): void {
  const template = props.templates.find((item) => item.id === id)
  if (!template) return
  environmentId.value = template.env_ids[0] ?? ''
  interfaceIds.value = [...template.interface_ids]
  flow.value = normalizeFlow(template.flow)
  reportConfig.value = normalizeReportConfig(template.report_config)
  cleanConfig.value = normalizeCleanConfig(template.clean_config)
  if (!name.value.trim()) name.value = template.name
  templateHint.value = `已按模板「${template.name}」预填参数，可覆盖；保存不会修改原模板。`
}

function onTemplateChange(event: Event): void {
  templateId.value = (event.target as HTMLSelectElement).value
  if (!templateId.value) {
    templateHint.value = ''
    return
  }
  applyTemplate(templateId.value)
}

function onClose(): void {
  if (props.saving) return
  emit('close')
}

function onSubmit(): void {
  if (!name.value.trim()) {
    formError.value = '请填写任务名称'
    return
  }
  if (!environmentId.value) {
    formError.value = '请选择被测环境'
    return
  }
  if (interfaceIds.value.length === 0) {
    formError.value = '请至少选择一个接口（后端校验码 23003）'
    return
  }
  const flowError = validateFlow(flow.value)
  if (flowError) {
    formError.value = flowError
    return
  }
  const reportError = validateReportConfig(reportConfig.value)
  if (reportError) {
    formError.value = reportError
    return
  }
  const cleanError = validateCleanConfig(cleanConfig.value)
  if (cleanError) {
    formError.value = cleanError
    return
  }

  const payload: CreateTaskPayload = {
    name: name.value.trim(),
    environment_id: environmentId.value,
    interface_ids: interfaceIds.value,
    flow: toFlowPayload(flow.value),
    report_config: { formats: [...reportConfig.value.formats] },
    clean_config: toCleanPayload(cleanConfig.value),
    auto_start: autoStart.value,
  }
  if (productIds.value.length > 0) payload.product_ids = productIds.value
  if (templateId.value) payload.template_id = templateId.value

  formError.value = ''
  emit('submit', payload)
}
</script>

<style scoped>
.ap-dialog-wide {
  width: 900px;
}

.cfg-section {
  display: flex;
  flex-direction: column;
  gap: 10px;
  padding: 10px 12px;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
}

.cfg-title {
  margin: 0;
  font-size: 14px;
  color: #1f2933;
}

.check {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: #374151;
  cursor: pointer;
}
</style>
