<template>
  <div class="flow-config">
    <div class="cfg-grid">
      <label class="ap-field">
        <span class="ap-field-label">并发模式 *</span>
        <select :value="modelValue.concurrency_mode" :disabled="disabled" @change="onModeChange">
          <option v-for="mode in CONCURRENCY_MODE_OPTIONS" :key="mode.value" :value="mode.value">
            {{ mode.label }}
          </option>
        </select>
      </label>

      <label v-if="modelValue.concurrency_mode !== 'ramp'" class="ap-field">
        <span class="ap-field-label">目标并发（VU，正整数）*</span>
        <input
          :value="modelValue.target_vus ?? ''"
          type="number"
          min="1"
          step="1"
          :disabled="disabled"
          placeholder="如 500"
          @input="onTargetVus"
        />
      </label>

      <label v-if="modelValue.concurrency_mode !== 'ramp'" class="ap-field">
        <span class="ap-field-label">持续时间 *（如 300s / 5m）</span>
        <input
          :value="modelValue.duration"
          :disabled="disabled"
          placeholder="300s"
          @input="onTextInput('duration', $event)"
        />
      </label>
    </div>

    <p class="ap-muted mode-hint">{{ modeHint }}</p>

    <div v-if="modelValue.concurrency_mode === 'ramp'" class="ramp-block">
      <table class="ap-table ramp-table">
        <thead>
          <tr>
            <th>档位</th>
            <th>并发（VU）</th>
            <th>时长</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(stage, index) in modelValue.ramp_stages" :key="index">
            <td>第 {{ index + 1 }} 档</td>
            <td>
              <input
                :value="stage.vus ?? ''"
                type="number"
                min="1"
                step="1"
                class="cell-input"
                :disabled="disabled"
                @input="onStageVus(index, $event)"
              />
            </td>
            <td>
              <input
                :value="stage.duration"
                class="cell-input"
                placeholder="60s"
                :disabled="disabled"
                @input="onStageDuration(index, $event)"
              />
            </td>
            <td>
              <button
                type="button"
                class="ap-btn ap-btn-danger ap-btn-sm"
                :disabled="disabled"
                @click="removeStage(index)"
              >
                删除
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <div class="ramp-foot">
        <button
          type="button"
          class="ap-btn ap-btn-ghost ap-btn-sm"
          :disabled="disabled || modelValue.ramp_stages.length >= 20"
          @click="addStage"
        >
          添加档位
        </button>
        <span class="ap-muted">档位上限 20 个；总时长 {{ rampTotal }}</span>
      </div>
    </div>

    <div class="cfg-grid">
      <label class="ap-field">
        <span class="ap-field-label">预热时间（可选，如 10s）</span>
        <input
          :value="modelValue.warmup"
          :disabled="disabled"
          placeholder="10s"
          @input="onTextInput('warmup', $event)"
        />
      </label>
      <label class="ap-field">
        <span class="ap-field-label">请求超时（可选，如 5s）</span>
        <input
          :value="modelValue.timeout"
          :disabled="disabled"
          placeholder="5s"
          @input="onTextInput('timeout', $event)"
        />
      </label>
      <label class="ap-field">
        <span class="ap-field-label">思考时间（可选，如 0.5s）</span>
        <input
          :value="modelValue.think_time"
          :disabled="disabled"
          placeholder="0.5s"
          @input="onTextInput('think_time', $event)"
        />
      </label>
      <label class="ap-field">
        <span class="ap-field-label">采集间隔（如 5s）</span>
        <input :value="collection.interval" :disabled="disabled" @input="onCollectionInterval" />
      </label>
    </div>

    <div class="cfg-block">
      <div class="cfg-block-head">
        <span class="ap-field-label">参数化数据源</span>
        <label class="check">
          <input
            type="checkbox"
            :checked="Boolean(modelValue.parameterization)"
            :disabled="disabled"
            @change="toggleParameterization"
          />
          <span>启用（CSV/JSON 变量绑定）</span>
        </label>
      </div>
      <div v-if="modelValue.parameterization" class="cfg-grid">
        <label class="ap-field">
          <span class="ap-field-label">数据源类型</span>
          <select
            :value="modelValue.parameterization.source"
            :disabled="disabled"
            @change="onParamSource"
          >
            <option value="csv">CSV</option>
            <option value="json">JSON</option>
          </select>
        </label>
        <label class="ap-field">
          <span class="ap-field-label">相对路径（禁止 .. 与绝对路径）</span>
          <input
            :value="modelValue.parameterization.path"
            :disabled="disabled"
            placeholder="data/users.csv"
            @input="onParamPath"
          />
        </label>
      </div>
    </div>

    <div class="cfg-block">
      <span class="ap-field-label">准备阶段（透传快照）</span>
      <div class="cfg-grid">
        <label class="check">
          <input
            type="checkbox"
            :checked="preparation.connectivity_check"
            :disabled="disabled"
            @change="togglePreparation('connectivity_check', $event)"
          />
          <span>环境连通性检查</span>
        </label>
        <label class="check">
          <input
            type="checkbox"
            :checked="preparation.interface_ready_check"
            :disabled="disabled"
            @change="togglePreparation('interface_ready_check', $event)"
          />
          <span>接口就绪检查</span>
        </label>
        <label class="ap-field">
          <span class="ap-field-label">检查失败处理</span>
          <select :value="preparation.on_failure" :disabled="disabled" @change="onFailureChange">
            <option value="abort">中止任务</option>
            <option value="continue">继续执行</option>
          </select>
        </label>
      </div>
    </div>

    <div class="cfg-block">
      <span class="ap-field-label">采集指标（写入报告与阈值评估口径）</span>
      <div class="ap-chips">
        <button
          v-for="metric in COLLECTION_METRIC_VALUES"
          :key="metric"
          type="button"
          class="ap-chip"
          :class="{ active: collection.metrics.includes(metric) }"
          :disabled="disabled"
          @click="toggleMetric(metric)"
        >
          {{ metricLabel(metric) }}
        </button>
      </div>
    </div>

    <div class="cfg-block">
      <span class="ap-field-label">请求详情</span>
      <label class="cfg-check">
        <input
          type="checkbox"
          :checked="captureBody"
          :disabled="disabled"
          @change="onCaptureBodyChange"
        />
        <span
          >采集响应体（<b>压测性能开销较大，默认不采集</b>；开启后请求详情记录响应内容）</span
        >
      </label>
    </div>

    <p class="ap-alert ap-alert-hint">
      参数白名单：并发数须为正整数且不超过平台 max_vus；时长仅支持 ms/s/m/h（如 300s、5m）。
      非法取值由后端拦截并返回 23004 / 10003。
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { metricLabel } from '@/utils/threshold'
import {
  COLLECTION_METRIC_VALUES,
  CONCURRENCY_MODE_OPTIONS,
  durationSeconds,
  formatSeconds,
} from '@/utils/testing'
import type {
  ConcurrencyMode,
  FlowCollectionConfig,
  FlowConfig,
  FlowPreparationConfig,
  RampStage,
  ThresholdMetric,
} from '@/types'

// 流程参数配置（PRD §3.3.6 五阶段：准备 → 压测 → 采集评估 → 报告 → 清理）。
// 压测段参数走后端 domain.FlowConfig 白名单校验（INV-9）；准备/采集段为透传快照。
const props = defineProps<{
  modelValue: FlowConfig
  disabled?: boolean
}>()

const emit = defineEmits<{ (e: 'update:modelValue', value: FlowConfig): void }>()

const preparation = computed<FlowPreparationConfig>(
  () =>
    props.modelValue.preparation ?? {
      connectivity_check: true,
      interface_ready_check: true,
      on_failure: 'abort',
    },
)

const collection = computed<FlowCollectionConfig>(
  () => props.modelValue.collection ?? { interval: '5s', metrics: [] },
)

const modeHint = computed(
  () =>
    CONCURRENCY_MODE_OPTIONS.find((item) => item.value === props.modelValue.concurrency_mode)
      ?.hint ?? '',
)

const rampTotal = computed(() => {
  const total = props.modelValue.ramp_stages.reduce(
    (sum, stage) => sum + (durationSeconds(stage.duration) ?? 0),
    0,
  )
  return total > 0 ? formatSeconds(total) : '-'
})

function patch(next: Partial<FlowConfig>): void {
  emit('update:modelValue', { ...props.modelValue, ...next })
}

const captureBody = computed(() => props.modelValue.capture_response_body === true)
function onCaptureBodyChange(event: Event): void {
  patch({ capture_response_body: (event.target as HTMLInputElement).checked })
}

function onModeChange(event: Event): void {
  const value = (event.target as HTMLSelectElement).value
  const mode: ConcurrencyMode = value === 'ramp' || value === 'pulse' ? value : 'fixed'
  patch({ concurrency_mode: mode })
}

function onTargetVus(event: Event): void {
  const raw = (event.target as HTMLInputElement).value
  patch({ target_vus: raw === '' ? null : Number(raw) })
}

function onTextInput(
  field: 'duration' | 'warmup' | 'timeout' | 'think_time',
  event: Event,
): void {
  const next: FlowConfig = { ...props.modelValue }
  next[field] = (event.target as HTMLInputElement).value
  emit('update:modelValue', next)
}

function updateStage(index: number, next: Partial<RampStage>): void {
  patch({
    ramp_stages: props.modelValue.ramp_stages.map((stage, i) =>
      i === index ? { ...stage, ...next } : stage,
    ),
  })
}

function onStageVus(index: number, event: Event): void {
  const raw = (event.target as HTMLInputElement).value
  updateStage(index, { vus: raw === '' ? null : Number(raw) })
}

function onStageDuration(index: number, event: Event): void {
  updateStage(index, { duration: (event.target as HTMLInputElement).value })
}

function addStage(): void {
  const stages = props.modelValue.ramp_stages
  const last = stages[stages.length - 1]
  patch({
    ramp_stages: [...stages, { vus: last?.vus ?? 100, duration: last?.duration ?? '60s' }],
  })
}

function removeStage(index: number): void {
  patch({ ramp_stages: props.modelValue.ramp_stages.filter((_, i) => i !== index) })
}

function toggleParameterization(event: Event): void {
  const enabled = (event.target as HTMLInputElement).checked
  patch({ parameterization: enabled ? { source: 'csv', path: '' } : null })
}

function onParamSource(event: Event): void {
  const source = (event.target as HTMLSelectElement).value === 'json' ? 'json' : 'csv'
  patch({ parameterization: { source, path: props.modelValue.parameterization?.path ?? '' } })
}

function onParamPath(event: Event): void {
  const path = (event.target as HTMLInputElement).value
  patch({ parameterization: { source: props.modelValue.parameterization?.source ?? 'csv', path } })
}

function patchPreparation(next: Partial<FlowPreparationConfig>): void {
  patch({ preparation: { ...preparation.value, ...next } })
}

function togglePreparation(
  key: 'connectivity_check' | 'interface_ready_check',
  event: Event,
): void {
  const checked = (event.target as HTMLInputElement).checked
  const next = { ...preparation.value }
  next[key] = checked
  patch({ preparation: next })
}

function onFailureChange(event: Event): void {
  const value = (event.target as HTMLSelectElement).value === 'continue' ? 'continue' : 'abort'
  patchPreparation({ on_failure: value })
}

function patchCollection(next: Partial<FlowCollectionConfig>): void {
  patch({ collection: { ...collection.value, ...next } })
}

function onCollectionInterval(event: Event): void {
  patchCollection({ interval: (event.target as HTMLInputElement).value })
}

function toggleMetric(metric: ThresholdMetric): void {
  const metrics = collection.value.metrics.includes(metric)
    ? collection.value.metrics.filter((item) => item !== metric)
    : [...collection.value.metrics, metric]
  patchCollection({ metrics })
}
</script>

<style scoped>
.flow-config {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.cfg-grid {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
}

.cfg-grid > .ap-field {
  flex: 1;
  min-width: 180px;
}

.cfg-check {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: #374151;
  cursor: pointer;
}
.cfg-check input {
  accent-color: var(--ap-primary);
}

.cfg-block {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 8px 10px;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  background: #fcfcfd;
}

.cfg-block-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
}

.mode-hint {
  margin: 0;
  font-size: 12px;
}

.ramp-block {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.ramp-table .cell-input {
  width: 100%;
  padding: 5px 8px;
  border: 1px solid #d1d5db;
  border-radius: 6px;
  font-size: 13px;
  font-family: inherit;
}

.ramp-foot {
  display: flex;
  align-items: center;
  gap: 12px;
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
