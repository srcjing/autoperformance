import { ApiError } from '@/api/http'
import type {
  CleanAction,
  CleanConfig,
  CleanScript,
  CleanTiming,
  ConcurrencyMode,
  FlowCollectionConfig,
  FlowConfig,
  FlowConfigPayload,
  FlowPreparationConfig,
  RampStage,
  ReportConfig,
  ReportFormat,
  TaskStatus,
  ThresholdMetric,
} from '@/types'

// 测试管理通用工具（PRD §3.3 / §7.2、api-contract §2.4）。
// 职责：状态元数据、流程/报告/清理配置的默认值、归一化与提交前校验。
// 校验口径与后端 domain.FlowConfig.Params / ParseCleanConfig 保持一致（INV-9），
// 前端校验只做提前提示，最终以服务端 10003/23004 等错误码为准。

/** 任务状态元数据（标签 + 徽标样式 + 是否活跃执行中）。 */
export interface TaskStatusMeta {
  value: TaskStatus
  label: string
  badge: string
  active: boolean
}

export const TASK_STATUS_META: readonly TaskStatusMeta[] = [
  { value: 'pending', label: '待执行', badge: 'ap-badge-info', active: false },
  { value: 'running', label: '执行中', badge: 'ap-badge-warn', active: true },
  { value: 'collecting', label: '采集中', badge: 'ap-badge-warn', active: true },
  { value: 'evaluating', label: '评估中', badge: 'ap-badge-warn', active: true },
  { value: 'generating', label: '报告生成中', badge: 'ap-badge-warn', active: true },
  { value: 'finished', label: '已完成', badge: 'ap-badge-success', active: false },
  { value: 'failed', label: '执行失败', badge: 'ap-badge-danger', active: false },
  { value: 'cancelled', label: '已取消', badge: 'ap-badge-muted', active: false },
  { value: 'cleanup_pending', label: '清理待确认', badge: 'ap-badge-danger', active: false },
  { value: 'cleaning', label: '清理中', badge: 'ap-badge-warn', active: true },
  { value: 'ended', label: '已结束', badge: 'ap-badge-muted', active: false },
]

const UNKNOWN_STATUS: TaskStatusMeta = {
  value: 'pending',
  label: '未知状态',
  badge: 'ap-badge-muted',
  active: false,
}

/** 按状态取值查元数据（未知状态原样降级展示，便于排查历史数据）。 */
export function taskStatusMeta(status: string): TaskStatusMeta {
  return TASK_STATUS_META.find((item) => item.value === status) ?? UNKNOWN_STATUS
}

/** 是否处于活跃执行阶段（占用 k6 子进程/环境）。 */
export function isActiveTaskStatus(status: string): boolean {
  return taskStatusMeta(status).active
}

/** 是否可执行（仅 pending，对齐后端 CanStartTask）。 */
export function canStartTask(status: string): boolean {
  return status === 'pending'
}

/** 是否可停止（仅 running，对齐后端 CanCancelTask）。 */
export function canCancelTask(status: string): boolean {
  return status === 'running'
}

/** 是否可重试（failed / cleanup_pending，对齐后端 CanRetryTask）。 */
export function canRetryTask(status: string): boolean {
  return status === 'failed' || status === 'cleanup_pending'
}

/** 是否可人工确认清理（仅 cleanup_pending，对齐后端 CanCleanupTask）。 */
export function canCleanupTask(status: string): boolean {
  return status === 'cleanup_pending'
}

/** 并发模式选项。 */
export const CONCURRENCY_MODE_OPTIONS: readonly {
  value: ConcurrencyMode
  label: string
  hint: string
}[] = [
  { value: 'fixed', label: '固定并发', hint: '恒定 VU 压测：填写目标并发与持续时间' },
  { value: 'ramp', label: '阶梯加压', hint: '按档位逐级升压：填写档位序列与每档时长' },
  { value: 'pulse', label: '脉冲', hint: '模拟瞬时洪峰：峰值并发 + 持续时间' },
]

/** 内置清理动作选项（script 动作由脚本列表隐式补齐，不单独勾选）。 */
export const CLEAN_ACTION_OPTIONS: readonly {
  value: CleanAction
  label: string
  hint: string
}[] = [
  { value: 'restore_config', label: '恢复配置', hint: '还原压测前修改的连接池/限流阈值等配置' },
  { value: 'clean_data', label: '清理数据', hint: '删除压测产生的临时数据/日志/缓存' },
  { value: 'restart_service', label: '重启服务', hint: '压测结束后重启被测服务' },
  { value: 'release_resource', label: '释放资源', hint: '释放端口/连接/占用' },
]

/** 报告格式选项。 */
export const REPORT_FORMAT_OPTIONS: readonly { value: ReportFormat; label: string }[] = [
  { value: 'html', label: 'HTML（支持在线查看）' },
  { value: 'pdf', label: 'PDF（支持在线查看）' },
  { value: 'word', label: 'Word（支持导出）' },
]

const CLEAN_ACTION_VALUES: readonly CleanAction[] = [
  'restore_config',
  'clean_data',
  'restart_service',
  'release_resource',
  'script',
]

/** 时长白名单（与后端 domain.durationRE 一致：数字 + ms/s/m/h）。 */
const DURATION_RE = /^[0-9]{1,7}(\.[0-9]{1,3})?(ms|s|m|h)$/

/** 正整数白名单（与后端 domain.numericRE 一致，拒绝注入型取值）。 */
const POSITIVE_INT_RE = /^[0-9]{1,9}$/

/** 相对路径白名单（参数化数据文件/清理脚本路径）。 */
const SAFE_PATH_RE = /^[A-Za-z0-9._/-]{1,255}$/

const SHA256_RE = /^[a-f0-9]{64}$/i

/** 时长文本是否合法（示例：300s / 5m / 0.5s）。 */
export function isValidDurationText(value: string): boolean {
  return DURATION_RE.test(value.trim())
}

/** 时长文本 → 秒（非法返回 null）。 */
export function durationSeconds(value: string): number | null {
  const text = value.trim()
  if (!DURATION_RE.test(text)) return null
  const match = /^([0-9]+(?:\.[0-9]+)?)(ms|s|m|h)$/.exec(text)
  if (!match) return null
  const amount = Number(match[1])
  const factor = match[2] === 'ms' ? 0.001 : match[2] === 's' ? 1 : match[2] === 'm' ? 60 : 3600
  return amount * factor
}

/** 秒 → 易读时长（用于阶梯加压总时长提示）。 */
export function formatSeconds(total: number): string {
  if (!Number.isFinite(total) || total <= 0) return '-'
  const hours = Math.floor(total / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  const seconds = Math.round(total % 60)
  if (hours > 0) return `${hours}h${minutes}m${seconds}s`
  if (minutes > 0) return `${minutes}m${seconds}s`
  return `${seconds}s`
}

/** 是否为正整数（1 ~ 999999999，后端另受 max_vus 上限约束）。 */
export function isPositiveInt(value: number | null | undefined): boolean {
  return (
    typeof value === 'number' &&
    Number.isInteger(value) &&
    value > 0 &&
    POSITIVE_INT_RE.test(String(value))
  )
}

/** 数值展示（默认保留 2 位小数，非数值显示 '-'）。 */
export function formatMetricValue(value: number | null | undefined, digits = 2): string {
  if (typeof value !== 'number' || !Number.isFinite(value)) return '-'
  return value.toFixed(digits)
}

/** 默认流程配置（"可用即可执行" 的最小示例，避免用户面对空表单）。 */
export function createDefaultFlow(): FlowConfig {
  return {
    concurrency_mode: 'fixed',
    target_vus: 100,
    ramp_stages: [
      { vus: 100, duration: '60s' },
      { vus: 300, duration: '120s' },
    ],
    duration: '300s',
    warmup: '10s',
    timeout: '5s',
    think_time: '0.5s',
    parameterization: null,
    preparation: { connectivity_check: true, interface_ready_check: true, on_failure: 'abort' },
    collection: {
      interval: '5s',
      metrics: ['avg_rt', 'p95', 'p99', 'error_rate', 'throughput', 'success_rate'],
    },
  }
}

/** 默认报告配置（决策 #4：默认全量生成三种格式）。 */
export function createDefaultReportConfig(): ReportConfig {
  return { formats: ['html', 'pdf', 'word'] }
}

	/** 默认清理配置：默认不选任何动作（接口压测默认不修改环境，无需清理）。 */
	export function createDefaultCleanConfig(): CleanConfig {
		return { actions: [], scripts: [], timing: 'auto' }
	}

/** 归一化准备阶段配置（历史数据可能缺字段）。 */
function normalizePreparation(raw: unknown): FlowPreparationConfig {
  const base: FlowPreparationConfig = {
    connectivity_check: true,
    interface_ready_check: true,
    on_failure: 'abort',
  }
  if (!raw || typeof raw !== 'object') return base
  const src = raw as Partial<FlowPreparationConfig>
  return {
    connectivity_check: src.connectivity_check !== false,
    interface_ready_check: src.interface_ready_check !== false,
    on_failure: src.on_failure === 'continue' ? 'continue' : 'abort',
  }
}

/** 归一化采集评估阶段配置。 */
function normalizeCollection(raw: unknown): FlowCollectionConfig {
  const base: FlowCollectionConfig = {
    interval: '5s',
    metrics: ['avg_rt', 'p95', 'p99', 'error_rate', 'throughput', 'success_rate'],
  }
  if (!raw || typeof raw !== 'object') return base
  const src = raw as Partial<FlowCollectionConfig>
  return {
    interval: typeof src.interval === 'string' && src.interval ? src.interval : base.interval,
    metrics: Array.isArray(src.metrics) && src.metrics.length > 0 ? [...src.metrics] : base.metrics,
  }
}

/** 服务端流程配置 → 表单模型（容错处理缺字段/空值）。 */
export function normalizeFlow(raw: unknown): FlowConfig {
  const base = createDefaultFlow()
  if (!raw || typeof raw !== 'object') return base
  const src = raw as Partial<FlowConfigPayload>
  const mode: ConcurrencyMode =
    src.concurrency_mode === 'ramp' || src.concurrency_mode === 'pulse'
      ? src.concurrency_mode
      : 'fixed'
  const stages: RampStage[] = Array.isArray(src.ramp_stages)
    ? src.ramp_stages.map((stage) => ({
        vus: typeof stage.vus === 'number' && Number.isFinite(stage.vus) ? stage.vus : null,
        duration: typeof stage.duration === 'string' ? stage.duration : '',
      }))
    : base.ramp_stages
  const param = src.parameterization
  return {
    concurrency_mode: mode,
    target_vus:
      typeof src.target_vus === 'number' && Number.isFinite(src.target_vus) ? src.target_vus : null,
    ramp_stages: stages,
    duration: typeof src.duration === 'string' && src.duration ? src.duration : base.duration,
    warmup: typeof src.warmup === 'string' ? src.warmup : '',
    timeout: typeof src.timeout === 'string' ? src.timeout : '',
    think_time: typeof src.think_time === 'string' ? src.think_time : '',
    parameterization:
      param && typeof param.path === 'string'
        ? { source: param.source === 'json' ? 'json' : 'csv', path: param.path }
        : null,
    preparation: normalizePreparation(src.preparation),
    collection: normalizeCollection(src.collection),
  }
}

/** 表单模型 → 请求体（剔除空档位/空参数化，ramp 模式不提交 target_vus）。 */
export function toFlowPayload(flow: FlowConfig): FlowConfigPayload {
  const payload: FlowConfigPayload = {
    concurrency_mode: flow.concurrency_mode,
    duration: flow.duration.trim(),
    warmup: flow.warmup.trim(),
    timeout: flow.timeout.trim(),
    think_time: flow.think_time.trim(),
  }
  if (flow.concurrency_mode === 'ramp') {
    payload.ramp_stages = flow.ramp_stages.map((stage) => ({
      vus: Number(stage.vus ?? 0),
      duration: stage.duration.trim(),
    }))
  } else if (flow.target_vus !== null) {
    payload.target_vus = flow.target_vus
  }
  if (flow.parameterization && flow.parameterization.path.trim()) {
    payload.parameterization = {
      source: flow.parameterization.source,
      path: flow.parameterization.path.trim(),
    }
  }
  if (flow.preparation) payload.preparation = { ...flow.preparation }
  if (flow.collection) {
    payload.collection = { ...flow.collection, metrics: [...flow.collection.metrics] }
  }
  return payload
}

/** 流程参数校验（返回空串表示通过）。 */
export function validateFlow(flow: FlowConfig): string {
  if (flow.concurrency_mode === 'ramp') {
    if (flow.ramp_stages.length === 0) return '阶梯加压至少需要一个档位'
    if (flow.ramp_stages.length > 20) return '阶梯加压档位不能超过 20 个'
    for (const [index, stage] of flow.ramp_stages.entries()) {
      if (!isPositiveInt(stage.vus)) return `第 ${index + 1} 档并发数需为正整数`
      if (!isValidDurationText(stage.duration)) {
        return `第 ${index + 1} 档时长格式非法（示例：60s / 2m）`
      }
    }
    const total = flow.ramp_stages.reduce(
      (sum, stage) => sum + (durationSeconds(stage.duration) ?? 0),
      0,
    )
    if (total <= 0) return '阶梯加压总时长需大于 0'
    if (total > 24 * 3600) return '阶梯加压总时长不能超过 24 小时'
  } else {
    if (!isPositiveInt(flow.target_vus)) return '目标并发数需为正整数（上限由平台 max_vus 控制）'
    if (!isValidDurationText(flow.duration)) return '持续时间格式非法（示例：300s / 5m）'
  }
  if (flow.warmup.trim() && !isValidDurationText(flow.warmup)) {
    return '预热时长格式非法（示例：10s）'
  }
  if (flow.timeout.trim() && !isValidDurationText(flow.timeout)) {
    return '请求超时格式非法（示例：5s）'
  }
  if (flow.think_time.trim() && !isValidDurationText(flow.think_time)) {
    return '思考时间格式非法（示例：0.5s）'
  }
  const param = flow.parameterization
  if (param) {
    const path = param.path.trim()
    if (!path) return '启用参数化后需填写数据文件相对路径'
    if (!SAFE_PATH_RE.test(path) || path.includes('..') || path.startsWith('/')) {
      return '参数化路径非法（仅允许字母/数字/._-/，禁止 .. 与绝对路径）'
    }
  }
  return ''
}

/** 报告配置校验。 */
export function validateReportConfig(config: ReportConfig): string {
  if ((config.formats ?? []).length === 0) return '至少选择一种报告格式'
  return ''
}

/** 清理配置校验（返回空串表示通过）。 */
export function validateCleanConfig(config: CleanConfig): string {
  // 允许不选任何动作：表示不执行环境清理（接口压测默认场景）。
  if ((config.scripts ?? []).length > 10) return '清理脚本不能超过 10 条'
  for (const [index, script] of config.scripts.entries()) {
    if (!script.host_id) return `第 ${index + 1} 条清理脚本需选择可信主机`
    const path = script.path.trim()
    if (!path) return `第 ${index + 1} 条清理脚本需填写脚本路径`
    if (!SAFE_PATH_RE.test(path) || path.includes('..')) {
      return `第 ${index + 1} 条清理脚本路径非法（禁止 .. 与非法字符）`
    }
    if (script.sha256 && !SHA256_RE.test(script.sha256.trim())) {
      return `第 ${index + 1} 条清理脚本 SHA256 需为 64 位十六进制`
    }
  }
  return ''
}

/** 提交前归一化清理配置：补齐/移除 script 动作（后端要求脚本动作必须伴随脚本项）。 */
export function toCleanPayload(config: CleanConfig): CleanConfig {
  const actions: CleanAction[] = config.actions.filter((action) => action !== 'script')
  if ((config.scripts ?? []).length > 0) actions.push('script')
  const scripts: CleanScript[] = config.scripts.map((script) => {
    const item: CleanScript = { host_id: script.host_id, path: script.path.trim() }
    const sha = script.sha256?.trim().toLowerCase()
    if (sha) item.sha256 = sha
    if (script.content && script.content.trim()) item.content = script.content
    return item
  })
  return { actions, scripts, timing: config.timing }
}

/** 服务端清理配置 → 表单模型。 */
export function normalizeCleanConfig(raw: unknown): CleanConfig {
  if (!raw || typeof raw !== 'object') return createDefaultCleanConfig()
  const src = raw as Partial<CleanConfig>
  const actions = Array.isArray(src.actions)
    ? src.actions.filter((action): action is CleanAction =>
        CLEAN_ACTION_VALUES.includes(action as CleanAction),
      )
    : []
  const scripts: CleanScript[] = Array.isArray(src.scripts)
    ? src.scripts.map((script) => {
        const item: CleanScript = {
          host_id: String(script.host_id ?? ''),
          path: String(script.path ?? ''),
        }
        if (script.sha256) item.sha256 = String(script.sha256)
        if (script.content) item.content = String(script.content)
        return item
      })
    : []
  const timing: CleanTiming = src.timing === 'manual' ? 'manual' : 'auto'
  return { actions, scripts, timing }
}

/** 服务端报告配置 → 表单模型（缺省时按全量三种格式）。 */
export function normalizeReportConfig(raw: unknown): ReportConfig {
  if (!raw || typeof raw !== 'object') return createDefaultReportConfig()
  const src = raw as Partial<ReportConfig>
  const formats = Array.isArray(src.formats)
    ? src.formats.filter(
        (format): format is ReportFormat =>
          format === 'html' || format === 'pdf' || format === 'word',
      )
    : []
  return { formats: formats.length > 0 ? formats : createDefaultReportConfig().formats }
}

/**
 * 测试管理业务错误码 → 可读提示（api-contract §4.2）。
 * 未命中业务码时回退服务端 message，避免吞掉真实原因。
 */
export function describeTaskError(error: unknown, fallback: string): string {
  if (!(error instanceof ApiError)) return fallback
  switch (error.code) {
    case 23001:
      return '任务状态不允许该操作（可能已被其他操作改变），请刷新后重试'
    case 23002:
      return '环境被占用/正在测试，请等待该环境上的任务结束后重试'
    case 23003:
      return '接口范围为空，请至少选择一个接口'
    case 23004:
      return '并发数非法或超出上限（max_vus），请调整目标并发/档位'
    case 23005:
      return '模板不存在或已被删除'
    case 10402:
      return '数据已被他人修改（并发冲突），请刷新后重试'
    case 10301:
      return '资源不存在或已被删除'
    case 10103:
      return '当前账号需先修改密码后才能继续操作'
    default:
      return error.message || fallback
  }
}

/** 采集指标选项（与阈值指标一致，PRD §9.1）。 */
export const COLLECTION_METRIC_VALUES: readonly ThresholdMetric[] = [
  'avg_rt',
  'p95',
  'p99',
  'max_rt',
  'throughput',
  'error_rate',
  'success_rate',
  'concurrency',
]
