<template>
  <div class="ap-card metric-panel">
    <div class="metric-head">
      <h3 class="metric-title">采集指标与达标判定</h3>
      <div class="metric-meta">
        <span v-if="evalMeta" class="ap-badge" :class="evalMeta.badge">{{ evalMeta.label }}</span>
        <span v-if="durationText" class="ap-muted">执行时长 {{ durationText }}</span>
      </div>
    </div>

    <div v-if="hasMetrics" class="metric-grid">
      <div v-for="metric in metricCards" :key="metric.key" class="metric-card">
        <div class="metric-label">{{ metric.label }}</div>
        <div class="metric-value">
          {{ metric.text }}<span class="metric-unit">{{ metric.unit }}</span>
        </div>
      </div>
    </div>
    <p v-else class="ap-alert ap-alert-hint">
      暂无采集指标：任务执行到「采集」阶段后展示 P95/P99/错误率等归一化指标。
    </p>

    <table v-if="evalItems.length > 0" class="ap-table eval-table">
      <thead>
        <tr>
          <th>接口</th>
          <th>指标</th>
          <th>条件</th>
          <th>阈值</th>
          <th>实测</th>
          <th>结论</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(item, index) in evalItems" :key="`${item.interface_id}-${item.metric}-${index}`">
          <td>{{ item.interface_name || `接口 #${item.interface_id}` }}</td>
          <td>{{ metricLabel(item.metric) }}</td>
          <td>{{ operatorText(item.operator) }}</td>
          <td>{{ item.threshold }}</td>
          <td>{{ item.has_actual ? formatMetricValue(item.actual) : '-' }}</td>
          <td>
            <span v-if="!item.has_actual" class="ap-badge ap-badge-muted">缺数据</span>
            <span
              v-else
              class="ap-badge"
              :class="item.pass ? 'ap-badge-success' : 'ap-badge-danger'"
            >
              {{ item.pass ? '达标' : '未达标' }}
            </span>
            <div v-if="item.reason" class="ap-muted reason">{{ item.reason }}</div>
          </td>
        </tr>
      </tbody>
    </table>

    <p v-else-if="evaluation" class="ap-alert ap-alert-hint">
      {{ evalMeta?.label }}：{{ evalMeta?.hint }}
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { metricLabel } from '@/utils/threshold'
import { formatMetricValue, formatSeconds } from '@/utils/testing'
import type { EvaluationItem, TaskEvaluation } from '@/types'

// 指标与阈值判定面板（PRD §9.1/§9.2 / INV-2：全部启用阈值达标才算达标）。
// 数据来源：实时 metrics/evaluation 事件，或任务完成后 result.metrics / result.evaluation。
interface MetricMeta {
  key: string
  label: string
  unit: string
}

const METRIC_METAS: readonly MetricMeta[] = [
  { key: 'avg_rt', label: '平均响应时间', unit: 'ms' },
  { key: 'p95', label: 'P95 响应时间', unit: 'ms' },
  { key: 'p99', label: 'P99 响应时间', unit: 'ms' },
  { key: 'max_rt', label: '最大响应时间', unit: 'ms' },
  { key: 'throughput', label: '吞吐量', unit: 'req/s' },
  { key: 'error_rate', label: '错误率', unit: '%' },
  { key: 'success_rate', label: '成功率', unit: '%' },
  { key: 'concurrency', label: '并发数', unit: '个' },
]

const EVAL_META: Record<string, { label: string; badge: string; hint: string }> = {
  pass: { label: '全部阈值达标', badge: 'ap-badge-success', hint: '所有已启用阈值均达标' },
  fail: { label: '存在未达标阈值', badge: 'ap-badge-danger', hint: '存在未达标条目，详见下表' },
  unconfigured: {
    label: '未配置阈值',
    badge: 'ap-badge-muted',
    hint: '接口未配置启用阈值，不计入达标率分母',
  },
  missing: {
    label: '指标数据缺失',
    badge: 'ap-badge-warn',
    hint: '阈值存在但缺少对应采集指标，无法判定',
  },
}

const props = defineProps<{
  metrics?: Record<string, number> | null
  evaluation?: TaskEvaluation | null
  durationSeconds?: number
}>()

const hasMetrics = computed(() => {
  const metrics = props.metrics
  return Boolean(metrics) && Object.keys(metrics ?? {}).length > 0
})

const metricCards = computed(() =>
  METRIC_METAS.filter((meta) => typeof props.metrics?.[meta.key] === 'number').map((meta) => ({
    ...meta,
    text: formatMetricValue(props.metrics?.[meta.key]),
  })),
)

const evalItems = computed<EvaluationItem[]>(() => props.evaluation?.items ?? [])

const evalMeta = computed(() => {
  if (!props.evaluation) return null
  return EVAL_META[props.evaluation.state] ?? EVAL_META.missing
})

const durationText = computed(() =>
  props.durationSeconds && props.durationSeconds > 0 ? formatSeconds(props.durationSeconds) : '',
)

/** 阈值方向文案（未知方向按 ≤ 展示）。 */
function operatorText(operator: string): string {
  return operator === 'ge' ? '≥' : '≤'
}
</script>

<style scoped>
.metric-panel {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-width: 0;
}

.metric-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
}

.metric-title {
  margin: 0;
  font-size: 14px;
  color: #1f2933;
}

.metric-meta {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 12px;
}

.metric-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
  gap: 10px;
}

.metric-card {
  padding: 10px 12px;
  border-radius: 8px;
  background: #f9fafb;
  border: 1px solid #eef0f3;
}

.metric-label {
  font-size: 12px;
  color: #6b7280;
}

.metric-value {
  margin-top: 4px;
  font-size: 18px;
  font-weight: 600;
  color: #1f2933;
}

.metric-unit {
  margin-left: 3px;
  font-size: 11px;
  font-weight: normal;
  color: #9ca3af;
}

.eval-table {
  font-size: 13px;
}

.reason {
  font-size: 11px;
}
</style>
