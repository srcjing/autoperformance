<template>
  <tr>
    <td>
      <select :value="modelValue.metric" :disabled="disabled" @change="onMetricChange">
        <option v-for="meta in metricOptions" :key="meta.value" :value="meta.value">
          {{ meta.label }}
        </option>
      </select>
    </td>
    <td>
      <span class="operator">{{ operatorLabel(modelValue.operator) }}</span>
    </td>
    <td>
      <input
        :value="modelValue.value"
        type="number"
        min="0"
        step="0.01"
        :disabled="disabled"
        @input="onValueInput"
      />
    </td>
    <td>
      <input :value="modelValue.unit" maxlength="16" :disabled="disabled" @input="onUnitInput" />
    </td>
    <td class="center">
      <input
        type="checkbox"
        :checked="modelValue.enabled !== false"
        :disabled="disabled"
        @change="onEnabledChange"
      />
    </td>
    <td>
      <button type="button" class="ap-btn ap-btn-danger ap-btn-sm" :disabled="disabled" @click="emit('remove')">
        移除
      </button>
    </td>
  </tr>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { THRESHOLD_METRICS, metricMeta, operatorLabel } from '@/utils/threshold'
import type { ThresholdMetric, ThresholdOperator, ThresholdPayload } from '@/types'

// 单条阈值行（指标 + 方向 + 阈值 + 单位 + 启用）。
// 方向由指标唯一决定（PRD §9.1）：切换指标时自动同步方向与默认单位，避免 22003 非法组合。
const props = defineProps<{
  modelValue: ThresholdPayload
  /** 已被其它行占用的指标（同一接口同一指标只允许一条）。 */
  usedMetrics: ThresholdMetric[]
  disabled: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: ThresholdPayload): void
  (e: 'remove'): void
}>()

const metricOptions = computed(() =>
  THRESHOLD_METRICS.filter(
    (meta) => meta.value === props.modelValue.metric || !props.usedMetrics.includes(meta.value),
  ),
)

function update(patch: Partial<ThresholdPayload>): void {
  emit('update:modelValue', { ...props.modelValue, ...patch })
}

function onMetricChange(event: Event): void {
  const metric = (event.target as HTMLSelectElement).value as ThresholdMetric
  const meta = metricMeta(metric)
  const operator: ThresholdOperator = meta?.operator ?? 'le'
  update({ metric, operator, unit: meta?.unit ?? '' })
}

function onValueInput(event: Event): void {
  update({ value: Number((event.target as HTMLInputElement).value) })
}

function onUnitInput(event: Event): void {
  update({ unit: (event.target as HTMLInputElement).value })
}

function onEnabledChange(event: Event): void {
  update({ enabled: (event.target as HTMLInputElement).checked })
}
</script>

<style scoped>
.operator {
  font-size: 12px;
  color: #4b5563;
  white-space: nowrap;
}

.center {
  text-align: center;
}
</style>
