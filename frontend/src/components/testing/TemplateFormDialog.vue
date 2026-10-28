<template>
  <div v-if="open" class="ap-dialog-mask" @click.self="onClose">
    <div class="ap-dialog ap-dialog-wide" role="dialog" aria-modal="true">
      <div class="ap-dialog-header">
        <span>{{ isEdit ? '编辑模板' : '新建模板' }}</span>
        <button type="button" class="ap-link" :disabled="saving" @click="onClose">关闭</button>
      </div>

      <form class="ap-dialog-body" @submit.prevent="onSubmit">
        <p class="ap-alert ap-alert-hint">
          模板 = 预填好参数的五要素流程（环境 / 接口范围 / 流程 / 报告 / 清理）。
          基于模板创建任务时可覆盖参数，且不影响原模板。
        </p>

        <div class="ap-form-row">
          <label class="ap-field">
            <span class="ap-field-label">模板名称 *</span>
            <input
              v-model="name"
              :disabled="saving"
              maxlength="128"
              placeholder="如：登录接口 500 并发基线"
            />
          </label>
          <label class="ap-field">
            <span class="ap-field-label">描述</span>
            <input
              v-model="description"
              :disabled="saving"
              maxlength="500"
              placeholder="用途/注意事项（可选）"
            />
          </label>
        </div>

        <section class="cfg-section">
          <h3 class="cfg-title">1. 默认环境与接口范围</h3>
          <EnvPicker
            v-model="envIds"
            label="默认环境（可多选；基于模板创建任务时默认取第一个）"
            multiple
            :disabled="saving"
          />
          <div class="ap-field">
            <span class="ap-field-label">默认接口范围（可跨产品多接口）</span>
            <InterRangePicker
              v-model="interfaceIds"
              v-model:product-ids="productIds"
              :disabled="saving"
              @update:selected-interfaces="selectedInterfaces = $event"
            />
          </div>
        </section>

        <section class="cfg-section">
          <h3 class="cfg-title">2. 测试流程参数</h3>
          <FlowConfig v-model="flow" :interfaces="selectedInterfaces" :disabled="saving" />
        </section>

        <section class="cfg-section">
          <h3 class="cfg-title">3. 报告输出</h3>
          <ReportConfig v-model="reportConfig" :disabled="saving" />
        </section>

        <section class="cfg-section">
          <h3 class="cfg-title">4. 环境清理</h3>
          <CleanConfig v-model="cleanConfig" :disabled="saving" />
        </section>

        <p v-if="displayError" class="ap-alert ap-alert-error" role="alert">{{ displayError }}</p>
        <p v-else-if="isEdit" class="ap-alert ap-alert-hint">
          当前版本号 {{ template?.version }}：保存时若已被他人修改会提示并发冲突（10402），请刷新后重试。
        </p>

        <div class="ap-dialog-footer">
          <button type="button" class="ap-btn ap-btn-ghost" :disabled="saving" @click="onClose">
            取消
          </button>
          <button type="submit" class="ap-btn ap-btn-primary" :disabled="saving">
            {{ saving ? '保存中…' : '保存模板' }}
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
	FlowConfig as FlowConfigModel,
	InterfaceOption,
	ReportConfig as ReportConfigModel,
	TestTemplateInfo,
	TestTemplatePayload,
} from '@/types'

// 模板新建/编辑弹窗（TemplateForm → EnvPicker / InterRangePicker / FlowConfig / CleanConfig）。
// 配置校验与后端 domain 白名单一致（INV-9），服务端错误（10003/10402/23005）由列表页透传展示。
const props = defineProps<{
  open: boolean
  template: TestTemplateInfo | null
  saving: boolean
  /** 服务端返回的业务错误，由列表页透传。 */
  errorMessage?: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'submit', payload: TestTemplatePayload): void
}>()

const name = ref('')
const description = ref('')
const envIds = ref<string[]>([])
const interfaceIds = ref<string[]>([])
const productIds = ref<string[]>([])
const selectedInterfaces = ref<InterfaceOption[]>([])
const flow = ref<FlowConfigModel>(createDefaultFlow())
const reportConfig = ref<ReportConfigModel>(createDefaultReportConfig())
const cleanConfig = ref<CleanConfigModel>(createDefaultCleanConfig())
const formError = ref('')

const isEdit = computed(() => props.template !== null)
const displayError = computed(() => props.errorMessage || formError.value)

watch(
  () => props.open,
  (open) => {
    if (open) reset()
  },
  { immediate: true },
)

function reset(): void {
  const template = props.template
  name.value = template?.name ?? ''
  description.value = template?.description ?? ''
  envIds.value = [...(template?.env_ids ?? [])]
  interfaceIds.value = [...(template?.interface_ids ?? [])]
  productIds.value = []
  selectedInterfaces.value = []
  flow.value = template ? normalizeFlow(template.flow) : createDefaultFlow()
  reportConfig.value = template
    ? normalizeReportConfig(template.report_config)
    : createDefaultReportConfig()
  cleanConfig.value = template
    ? normalizeCleanConfig(template.clean_config)
    : createDefaultCleanConfig()
  formError.value = ''
}

function onClose(): void {
  if (props.saving) return
  emit('close')
}

function onSubmit(): void {
  if (!name.value.trim()) {
    formError.value = '请填写模板名称'
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

  const payload: TestTemplatePayload = {
    name: name.value.trim(),
    env_ids: envIds.value,
    interface_ids: interfaceIds.value,
    flow: toFlowPayload(flow.value),
    report_config: { formats: [...reportConfig.value.formats] },
    clean_config: toCleanPayload(cleanConfig.value),
    description: description.value.trim(),
  }
  if (props.template) payload.version = props.template.version

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
</style>
