import type { ThresholdMetric, ThresholdOperator } from '@/types'

// 阈值指标元数据（与后端 internal/domain.Product 的 thresholdOperators 白名单一致）。
// 方向由指标唯一决定（PRD §9.1），前端据此联动，配合服务端 22003 双重拦截（INV-2）。

export interface MetricMeta {
  value: ThresholdMetric
  label: string
  operator: ThresholdOperator
  unit: string
}

export const THRESHOLD_METRICS: readonly MetricMeta[] = [
  { value: 'avg_rt', label: '平均响应时间', operator: 'le', unit: 'ms' },
  { value: 'p95', label: '95 分位响应时间', operator: 'le', unit: 'ms' },
  { value: 'p99', label: '99 分位响应时间', operator: 'le', unit: 'ms' },
  { value: 'max_rt', label: '最大响应时间', operator: 'le', unit: 'ms' },
  { value: 'error_rate', label: '错误率', operator: 'le', unit: '%' },
  { value: 'throughput', label: '吞吐量', operator: 'ge', unit: 'req/s' },
  { value: 'success_rate', label: '成功率', operator: 'ge', unit: '%' },
  { value: 'concurrency', label: '并发数', operator: 'ge', unit: '个' },
]

/** 方向展示文案（le=≤ 越小越好；ge=≥ 越大越好）。 */
export function operatorLabel(operator: ThresholdOperator): string {
  return operator === 'le' ? '≤ 越小越好' : '≥ 越大越好'
}

/** 按指标取值查元数据。 */
export function metricMeta(metric: string): MetricMeta | undefined {
  return THRESHOLD_METRICS.find((item) => item.value === metric)
}

/** 指标展示名（未知指标原样输出，便于排查历史数据）。 */
export function metricLabel(metric: string): string {
  return metricMeta(metric)?.label ?? metric
}
