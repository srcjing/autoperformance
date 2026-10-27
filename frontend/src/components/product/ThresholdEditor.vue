<template>
  <div v-if="open" class="ap-dialog-mask" @click.self="onClose">
    <div class="ap-dialog" role="dialog" aria-modal="true">
      <div class="ap-dialog-header">
        <span>阈值配置 · {{ item?.name ?? '' }}</span>
        <button type="button" class="ap-link" :disabled="saving" @click="onClose">关闭</button>
      </div>

      <form class="ap-dialog-body" @submit.prevent="onSubmit">
        <p class="ap-alert ap-alert-hint">
          判定口径（PRD §9.2）：接口所有「已启用」阈值全部达标才算该接口达标；
          响应时间类与错误率只能「≤ 越小越好」，吞吐量/成功率/并发数只能「≥ 越大越好」。
        </p>

        <table class="ap-table threshold-table">
          <thead>
            <tr>
              <th>指标</th>
              <th>方向</th>
              <th>阈值</th>
              <th>单位</th>
              <th class="center">启用</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <MetricRow
              v-for="(row, index) in rows"
              :key="`${row.metric}-${index}`"
              v-model="rows[index]"
              :used-metrics="usedMetrics"
              :disabled="saving || loading"
              @remove="removeRow(index)"
            />
          </tbody>
        </table>
        <p v-if="!loading && rows.length === 0" class="ap-table-empty">
          未配置阈值：该接口将标记为「未配置阈值」，不计入达标率分母
        </p>
        <p v-if="loading" class="ap-table-empty">加载中…</p>

        <div class="ap-actions">
          <button
            type="button"
            class="ap-btn ap-btn-ghost"
            :disabled="saving || loading || rows.length >= THRESHOLD_METRICS.length"
            @click="addRow"
          >
            添加指标
          </button>
        </div>

        <p v-if="displayError" class="ap-alert ap-alert-error" role="alert">{{ displayError }}</p>

        <div class="ap-dialog-footer">
          <button type="button" class="ap-btn ap-btn-ghost" :disabled="saving" @click="onClose">取消</button>
          <button type="submit" class="ap-btn ap-btn-primary" :disabled="saving || loading">
            {{ saving ? '保存中…' : '保存（全量替换）' }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import MetricRow from './MetricRow.vue'
import { THRESHOLD_METRICS } from '@/utils/threshold'
import type {
  InterfaceInfo,
	ThresholdInfo,
	ThresholdPayload,
} from '@/types'

// 阈值编辑器（InterTable → ThresholdEditor → MetricRow）。
// 保存为全量替换（PUT /interfaces/{id}/thresholds），先移除再新增。
const props = defineProps<{
  open: boolean
  item: InterfaceInfo | null
  thresholds: ThresholdInfo[]
  loading: boolean
  saving: boolean
  /** 服务端返回的业务错误（22003 等），由列表页透传。 */
  errorMessage?: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'submit', rows: ThresholdPayload[]): void
}>()

const rows = ref<ThresholdPayload[]>([])
const formError = ref('')
const displayError = computed(() => props.errorMessage ?? formError.value)
const usedMetrics = computed(() => rows.value.map((row) => row.metric))

watch(
  () => props.open,
  (open) => {
    if (open) reset()
  },
  { immediate: true },
)

watch(
  () => props.thresholds,
  () => {
    if (props.open) reset()
  },
)

function reset(): void {
  rows.value = props.thresholds.map((item) => ({
    metric: item.metric,
    operator: item.operator,
    value: item.value,
    unit: item.unit,
    enabled: item.enabled,
  }))
  formError.value = ''
}

function addRow(): void {
  const free = THRESHOLD_METRICS.find((meta) => !usedMetrics.value.includes(meta.value))
  if (!free) return
  rows.value = [
    ...rows.value,
    { metric: free.value, operator: free.operator, value: 0, unit: free.unit, enabled: true },
  ]
}

function removeRow(index: number): void {
  rows.value = rows.value.filter((_, i) => i !== index)
}

function onClose(): void {
  if (props.saving) return
  emit('close')
}

function onSubmit(): void {
  for (const row of rows.value) {
    if (!Number.isFinite(row.value) || row.value < 0) {
      formError.value = '阈值必须为非负数值'
      return
    }
    // 与服务端一致：错误率/成功率为百分比口径 0~100（PRD §9.1）。
    if ((row.metric === 'error_rate' || row.metric === 'success_rate') && row.value > 100) {
      formError.value = `${row.metric === 'error_rate' ? '错误率' : '成功率'}需在 0~100 之间`
      return
    }
  }
  formError.value = ''
  emit('submit', rows.value.map((row) => ({ ...row })))
}
</script>

<style scoped>
.threshold-table {
  font-size: 13px;
}

.center {
  text-align: center;
}
</style>
