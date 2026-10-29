import { ApiError } from '@/api/http'
import type { ReportFormat, ReportInfo, ThresholdMetric } from '@/types'
import { metricLabel } from './threshold'

// 报告管理展示元数据与错误文案（权限点 report.list:read/write）。

/** 徽标元数据（类名以 ap-badge-* 复用全局样式）。 */
export interface BadgeMeta {
  label: string
  badge: string
}

/** 报告状态（generating / ready / generation_failed）。 */
export const REPORT_STATUS_META: Record<string, BadgeMeta> = {
  generating: { label: '生成中', badge: 'ap-badge-info' },
  ready: { label: '已生成', badge: 'ap-badge-success' },
  generation_failed: { label: '生成失败', badge: 'ap-badge-danger' },
}

/** 报告来源展示文案。 */
export const REPORT_SOURCE_LABEL: Record<string, string> = {
  internal: '平台生成',
  external: '外部导入',
}

/** 报告格式展示文案。 */
export const REPORT_FORMAT_LABEL: Record<string, string> = {
  html: 'HTML',
  pdf: 'PDF',
  word: 'Word',
}

/** 摘要结论状态（evaluator.State + imported）。 */
export const REPORT_STATE_META: Record<string, BadgeMeta> = {
  pass: { label: '整体达标', badge: 'ap-badge-success' },
  fail: { label: '存在不达标项', badge: 'ap-badge-danger' },
  missing: { label: '数据缺失', badge: 'ap-badge-warn' },
  unconfigured: { label: '未配置阈值', badge: 'ap-badge-muted' },
  imported: { label: '外部导入', badge: 'ap-badge-info' },
}

/** 报告状态元数据（未知状态兜底展示原值）。 */
export function reportStatusMeta(status: string): BadgeMeta {
  return REPORT_STATUS_META[status] ?? { label: status || '-', badge: 'ap-badge-muted' }
}

/** 摘要结论元数据。 */
export function reportStateMeta(state?: string): BadgeMeta {
  if (!state) return { label: '-', badge: 'ap-badge-muted' }
  return REPORT_STATE_META[state] ?? { label: state, badge: 'ap-badge-muted' }
}

/** 格式展示文案。 */
export function reportFormatLabel(format: string): string {
  return REPORT_FORMAT_LABEL[format] ?? format
}

/** 来源展示文案。 */
export function reportSourceLabel(source: string): string {
  return REPORT_SOURCE_LABEL[source] ?? source
}

/** 指标中文名（与阈值指标一致）。 */
export function reportMetricLabel(metric: string): string {
  return metricLabel(metric as ThresholdMetric) || metric
}

/** 文件大小展示。 */
export function formatBytes(size: number): string {
  if (!size || size <= 0) return '-'
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / (1024 * 1024)).toFixed(1)} MB`
}

/** 报告已产出的格式列表（保持 html/pdf/word 固定顺序）。 */
export function artifactFormats(report: ReportInfo | null): ReportFormat[] {
  if (!report) return []
  const order: ReportFormat[] = ['html', 'pdf', 'word']
  return order.filter((format) => report.artifacts.some((artifact) => artifact.format === format))
}

/** 是否包含指定格式产物。 */
export function hasArtifact(report: ReportInfo | null, format: ReportFormat): boolean {
  return Boolean(report?.artifacts?.some((artifact) => artifact.format === format))
}

/** 报告相关错误文案（按业务码分支）。 */
export function describeReportError(error: unknown, fallback: string): string {
  if (!(error instanceof ApiError)) return fallback
  switch (error.code) {
    case 24001:
      return '报告文件不存在或已被清理，请尝试重新生成后再查看'
    case 24002:
      return '上传文件类型不允许：仅支持 HTML / PDF / DOCX，且文件内容需与扩展名一致'
    case 24003:
      return '上传文件过大，请压缩或拆分后重试'
    case 24004:
      return '报告生成失败（PDF 引擎不可用或生成超时），可稍后重新生成'
    case 10401:
      return '报告正在生成中或状态不允许该操作，请刷新后重试'
    case 10301:
      return '资源不存在或已被删除'
    default:
      return error.message || fallback
  }
}
