// 共享 TS 类型（对齐 api-contract.md 关键数据结构）。

/** 统一响应信封（api-contract §1.2）。 */
export interface ApiResponse<T> {
  code: number
  message: string
  data: T
}

/** 管理员信息（后端 DTO；不含 password_hash）。 */
export interface AdminInfo {
  id: string
  username: string
  name: string
  phone?: string
  email?: string
  description?: string
  status?: 'enabled' | 'disabled'
  is_builtin?: boolean
  must_change_password?: boolean
  last_login_at?: string | null
  version?: number
  roles?: RoleInfo[]
}

/** 管理员列表查询（关键字匹配用户名/姓名 + 分页，api-contract §2.7）。 */
export interface AdminQuery {
  keyword?: string
  page?: number
  page_size?: number
}

/**
 * 新建管理员请求（白名单字段）。
 *
 * 对齐 DES-E-002：故意不含 role / role_ids / status / is_builtin 等提权字段；
 * 新建账号默认启用、默认无权限（权限只能经「角色授权」接口授予）。
 */
export interface AdminCreatePayload {
  username: string
  name: string
  phone?: string
  email?: string
  description?: string
  password: string
}

/** 修改管理员请求（用户名/状态/内置标记不可改；version 触发乐观锁 10402）。 */
export interface AdminUpdatePayload {
  name: string
  phone?: string
  email?: string
  description?: string
  version?: number
}

/** 停用/启用管理员请求。 */
export interface AdminStatusPayload {
  status: 'enabled' | 'disabled'
  version?: number
}

/** 重置管理员口令请求（成功后该账号下次登录强制改密）。 */
export interface AdminResetPasswordPayload {
  new_password: string
}

/** 角色信息（角色列表/管理员详情内嵌；列表项含权限矩阵）。 */
export interface RoleInfo {
  id: string
  name: string
  description?: string
  permissions?: PermissionItem[]
  version?: number
  created_at?: string
  updated_at?: string
}

/** 角色新建/修改请求（权限矩阵全量替换）。 */
export interface RolePayload {
  name: string
  description?: string
  permissions: PermissionItem[]
  version?: number
}

/** 角色列表响应（GET /roles、GET /admins/{id}/roles 均返回 { list }）。 */
export interface RoleListResult {
  list: RoleInfo[]
}

/** 子模块授权项。 */
export interface PermissionItem {
  module: string
  permission: 'read' | 'write'
}

/** 子模块授权项元数据（GET /permissions，权限矩阵勾选来源）。 */
export interface PermissionModule {
  module_code: string
  name: string
  permissions: Array<'read' | 'write'>
  sensitive: boolean
}

/** 权限点清单响应（GET /permissions，供角色权限矩阵渲染）。 */
export interface PermissionModuleListResult {
  list: PermissionModule[]
}

/** 登录结果（POST /auth/login）。 */
export interface LoginResult {
  token: string
  expires_in: number
  admin: AdminInfo
  permissions: string[]
  must_change_password: boolean
}

/** 当前用户与权限集合（GET /auth/me）。 */
export interface MeResult {
  admin: AdminInfo
  permissions: string[]
}

/** 改密请求。 */
export interface ChangePasswordPayload {
  old_password: string
  new_password: string
}

/** 统一分页结构（api-contract §1.3）。 */
export interface PageResult<T> {
  list: T[]
  total: number
  page: number
  page_size: number
}

// ===================== 环境管理（PRD §4.1~4.3 / api-contract §2.2） =====================

/** 环境状态（api-contract §2.2 / PRD §7.1）。 */
export type EnvironmentStatus = 'pending' | 'testing' | 'disabled'

/** 环境类型（PRD §4.3）。 */
export type EnvironmentType = 'single' | 'cluster'

/** 环境分类（PRD §4.1）。 */
export interface EnvironmentCategory {
  id: string
  name: string
  description?: string
  version?: number
  created_at?: string
  updated_at?: string
}

/** 分类创建/修改请求。 */
export interface CategoryPayload {
  name: string
  description?: string
}

/** 标签摘要（环境内嵌）。 */
export interface TagBrief {
  id: string
  name: string
  color?: string
}

/** 标签（含关联环境数，PRD §4.2）。 */
export interface TagInfo extends TagBrief {
  description?: string
  environment_count?: number
  version?: number
  created_at?: string
  updated_at?: string
}

/** 标签创建/修改请求。 */
export interface TagPayload {
  name: string
  color?: string
  description?: string
}

/** 环境实体（PRD §4.3；hosts 无 env.list:read 时不下发）。 */
export interface EnvironmentInfo {
  id: string
  name: string
  code: string
  category_id: string
  category_name?: string
  tags: TagBrief[]
  type: EnvironmentType
  hosts?: string[]
  os?: string
  ssh_host_id?: string
  cert_id?: string
  description?: string
  status: EnvironmentStatus
  version: number
  created_at?: string
  updated_at?: string
}

/** 环境创建/修改请求。 */
export interface EnvironmentPayload {
  name: string
  code: string
  category_id: string
  tag_ids?: string[]
  type?: EnvironmentType
  hosts: string[]
  os?: string
  ssh_host_id?: string
  cert_id?: string
  description?: string
  version?: number
}

/** 环境列表查询。 */
export interface EnvironmentQuery {
  keyword?: string
  category_id?: string
  tag_id?: string
  tag_ids?: string[]
  status?: string
  page?: number
  page_size?: number
}

// ===================== 产品管理（PRD §4.4~4.6 / api-contract §2.3） =====================

export type ProductStatus = 'enabled' | 'disabled'
export type InterfaceStatus = 'enabled' | 'disabled'
export type HttpMethod = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE' | 'HEAD' | 'OPTIONS'

/** 产品实体。 */
export interface ProductInfo {
  id: string
  name: string
  code: string
  product_version?: string
  platform?: string
  description?: string
  status: ProductStatus
  interface_count?: number
  version: number
  created_at?: string
  updated_at?: string
}

/** 产品创建/修改请求。 */
export interface ProductPayload {
  name: string
  code: string
  product_version?: string
  platform?: string
  description?: string
  version?: number
}

/** 产品列表查询。 */
export interface ProductQuery {
  keyword?: string
  product_version?: string
  platform?: string
  status?: string
  page?: number
  page_size?: number
}

/** 接口超时配置（毫秒）。 */
export interface InterfaceTimeouts {
  connect_ms: number
  read_ms: number
  total_ms: number
}

/** 接口协议（接口不再配置 host，执行期由任务的 target_host 补齐）。 */
export type InterfaceScheme = 'http' | 'https'

/** 接口实体（认证头只回传名称 auth_headers，值不出现在响应中）。 */
export interface InterfaceInfo {
  id: string
  product_id: string
  name: string
  method: HttpMethod
  scheme: InterfaceScheme
  port: number
  path: string
  headers?: Record<string, string>
  auth_headers: string[]
  body?: unknown
  timeouts?: InterfaceTimeouts
  description?: string
  status: InterfaceStatus
  version: number
  created_at?: string
  updated_at?: string
}

/** 接口创建/修改请求（headers 中的认证头由服务端加密存储）。 */
export interface InterfacePayload {
  name: string
  method: HttpMethod
  scheme: InterfaceScheme
  port: number
  path: string
  headers?: Record<string, string>
  body?: unknown
  timeouts?: InterfaceTimeouts
  description?: string
  version?: number
}

/** 接口列表查询。 */
export interface InterfaceQuery {
  keyword?: string
  status?: string
  page?: number
  page_size?: number
}

/** 阈值指标（方向由指标唯一决定，PRD §9.1）。 */
export type ThresholdMetric =
  | 'avg_rt'
  | 'p95'
  | 'p99'
  | 'max_rt'
  | 'error_rate'
  | 'throughput'
  | 'success_rate'
  | 'concurrency'

/** 阈值方向：le=≤ 越小越好，ge=≥ 越大越好。 */
export type ThresholdOperator = 'le' | 'ge'

/** 指标阈值（接口级，enabled=true 才参与判定）。 */
export interface ThresholdInfo {
  metric: ThresholdMetric
  operator: ThresholdOperator
  value: number
  unit: string
  enabled: boolean
}

/** 阈值全量替换请求项。 */
export type ThresholdPayload = ThresholdInfo

// ===================== 测试管理（PRD §4.7~4.9 / api-contract §2.4） =====================

export type TaskStatus =
  | 'pending'
  | 'running'
  | 'collecting'
  | 'evaluating'
  | 'generating'
  | 'finished'
  | 'failed'
  | 'cancelled'
  | 'cleanup_pending'
  | 'cleaning'
  | 'ended'

/** 并发模式：固定 / 阶梯加压 / 脉冲。 */
export type ConcurrencyMode = 'fixed' | 'ramp' | 'pulse'

/** 阶梯加压档位。 */
export interface RampStage {
  vus: number | null
  duration: string
}

/** 参数化数据源（相对路径白名单校验）。 */
export interface FlowParameterization {
  source: 'csv' | 'json'
  path: string
}

/** 准备阶段配置。 */
export interface FlowPreparationConfig {
  connectivity_check: boolean
  interface_ready_check: boolean
  on_failure: 'abort' | 'continue'
}

/** 采集评估阶段配置。 */
export interface FlowCollectionConfig {
  interval: string
  metrics: ThresholdMetric[]
}

/** 流程参数表单模型（PRD §3.3.6）。 */
export interface FlowConfig {
  concurrency_mode: ConcurrencyMode
  target_vus: number | null
  ramp_stages: RampStage[]
  duration: string
  warmup: string
  timeout: string
  think_time: string
  parameterization: FlowParameterization | null
  preparation?: FlowPreparationConfig
  collection?: FlowCollectionConfig
}

/** 流程参数提交体（后端 domain.FlowConfig 白名单）。 */
export interface FlowConfigPayload {
  concurrency_mode: ConcurrencyMode
  target_vus?: number
  ramp_stages?: RampStage[]
  duration?: string
  warmup?: string
  timeout?: string
  think_time?: string
  parameterization?: FlowParameterization
  preparation?: FlowPreparationConfig
  collection?: FlowCollectionConfig
}

/** 报告输出配置（决策 #4：默认全量三种格式）。 */
export interface ReportConfig {
  formats: ReportFormat[]
}

/** 清理动作类型（内置动作 + 用户脚本）。 */
export type CleanAction =
  | 'restore_config'
  | 'clean_data'
  | 'restart_service'
  | 'release_resource'
  | 'script'

/** 清理执行时机：成功即执行 / 人工确认。 */
export type CleanTiming = 'auto' | 'manual'

/** 用户清理脚本（仅在可信 SSH 主机执行，可声明 SHA256 白名单）。 */
export interface CleanScript {
  host_id: string
  path: string
  sha256?: string
  content?: string
}

/** 环境清理配置（PRD §3.3.7 / 决策 #15）。 */
export interface CleanConfig {
  actions: CleanAction[]
  scripts: CleanScript[]
  timing: CleanTiming
}

/** 测试模板实体。 */
export interface TestTemplateInfo {
  id: string
  name: string
  description?: string
  env_ids: string[]
  interface_ids: string[]
  flow: FlowConfigPayload
  report_config: ReportConfig
  clean_config: CleanConfig
  version: number
  created_at?: string
  updated_at?: string
}

/** 模板创建/修改请求。 */
export interface TestTemplatePayload {
  name: string
  description?: string
  env_ids: string[]
  interface_ids: string[]
  flow: FlowConfigPayload
  report_config: ReportConfig
  clean_config: CleanConfig
  version?: number
}

/** 模板列表查询。 */
export interface TemplateQuery {
  keyword?: string
  page?: number
  page_size?: number
}

/** 阈值判定单条结果（实时评估与报告共用口径）。 */
export interface EvaluationItem {
  interface_id: string
  interface_name?: string
  metric: string
  operator: string
  threshold: number
  has_actual: boolean
  actual?: number
  pass: boolean
  reason?: string
}

/** 阈值评估结论（pass / fail / unconfigured / missing）。 */
export interface TaskEvaluation {
  state: 'pass' | 'fail' | 'unconfigured' | 'missing' | string
  items: EvaluationItem[]
}

/** 任务执行结果（采集指标 + 评估结论 + 用时）。 */
export interface TaskResult {
  metrics?: Record<string, number>
  evaluation?: TaskEvaluation
  duration_seconds?: number
}

/** 接口范围快照项（创建任务时固化，INV-5；认证头仅保留名称）。 */
export interface InterfaceSnapshotItem {
  interface_id: string
  product_id?: string
  product_name?: string
  name: string
  method: string
  scheme?: InterfaceScheme
  port?: number
  path?: string
  /** 展示用完整地址（由任务 target_host 拼接；历史快照保留原值）。 */
  url?: string
  auth_headers: string[]
  status: string
  captured_at: string
}

/** 阈值快照项（评估口径，INV-2）。 */
export interface ThresholdSnapshotItem {
  interface_id: string
  interface_name?: string
  metric: ThresholdMetric
  operator: ThresholdOperator
  value: number
  unit: string
  enabled: boolean
}

/** 测试任务实体。 */
export interface TestTaskInfo {
  id: string
  name: string
  template_id?: string
  environment_id: string
  /** 压测目标主机（取自所选环境 hosts 中的任一项）。 */
  target_host: string
  interface_ids: string[]
  product_ids?: string[]
  flow: FlowConfigPayload
  report_config: ReportConfig
  clean_config: CleanConfig
  status: TaskStatus
  result?: TaskResult | null
  interface_range_snapshot: InterfaceSnapshotItem[]
  threshold_snapshot: ThresholdSnapshotItem[]
  created_at?: string
  started_at?: string
  finished_at?: string
  version?: number
}

/** 任务创建请求（五要素 + 可选模板）。 */
export interface CreateTaskPayload {
  name: string
  environment_id: string
  /** 压测目标主机：必须属于所选环境的 hosts。 */
  target_host: string
  interface_ids: string[]
  product_ids?: string[]
  template_id?: string
  flow: FlowConfigPayload
  report_config: ReportConfig
  clean_config: CleanConfig
  auto_start: boolean
}

/** 任务列表查询。 */
export interface TaskQuery {
  keyword?: string
  status?: string
  env_id?: string
  environment_id?: string
  template_id?: string
  product_id?: string
  created_from?: string
  created_to?: string
  start_time?: string
  end_time?: string
  page?: number
  page_size?: number
}

// ===================== 报告管理（PRD §4.9 / api-contract §2.5） =====================

export type ReportFormat = 'html' | 'pdf' | 'word'
export type ReportStatus = 'generating' | 'ready' | 'generation_failed'
export type ReportSource = 'internal' | 'external'

/** 报告产物（在线查看/导出入口）。 */
export interface ReportArtifact {
  format: ReportFormat
  inline: boolean
  size: number
  created_at?: string
  filename?: string
  sha256?: string
}

/** 阈值判定明细（报告达标明细表，不达标行高亮）。 */
export interface ReportSummaryItem {
  id?: string
  interface_id: string
  interface_name?: string
  metric: string
  operator: string
  threshold: number
  has_actual: boolean
  actual?: number
  pass: boolean
  reason?: string
}

/** 报告摘要（达标率/阈值项/执行用时；外部导入含原始文件信息）。 */
export interface ReportSummary {
  state?: string
  total_interfaces: number
  passed_interfaces: number
  failed_interfaces: number
  pass_rate: number
  pass?: boolean
  passed_thresholds?: number
  failed_thresholds?: number
  duration_seconds?: number
  items?: ReportSummaryItem[]
  original_filename?: string
  size_bytes?: number
  sha256?: string
}

/** 报告实体（GET /reports、GET /reports/{id}）。 */
export interface ReportInfo {
  id: string
  task_id?: string
  product_id?: string
  source: ReportSource
  title: string
  status: ReportStatus
  summary?: ReportSummary
  artifacts: ReportArtifact[]
  created_at?: string
  updated_at?: string
}

/** 报告列表查询参数（时间参数需 RFC3339）。 */
export interface ReportQuery {
  task_id?: string
  product_id?: string
  type?: string
  format?: string
  status?: string
  source?: string
  keyword?: string
  start_time?: string
  end_time?: string
  page?: number
  page_size?: number
}

// ===================== 日志管理（PRD §3.5 / §4.10 / api-contract §2.6） =====================

/** 日志级别（system/test 自带 level；用户日志由 result 推导：ok→info、fail→error）。 */
export type LogLevel = 'info' | 'warn' | 'error'

/** 审计结果（user_log.result）。 */
export type AuditResult = 'ok' | 'fail'

/** 系统日志事件（服务启动/重启/停止）。 */
export type SystemLogEvent = 'start' | 'restart' | 'stop'

/** 三类日志共用查询条件（时间参数需 RFC3339）。 */
export interface LogQuery {
  level?: LogLevel | ''
  keyword?: string
  start_time?: string
  end_time?: string
  page?: number
  page_size?: number
}

/** 用户日志（操作人/时间/动作/对象/结果 5W1H）。 */
export interface UserLogInfo {
  id: string
  admin_id?: string
  admin_username?: string
  action: string
  resource_type?: string
  resource_id?: string
  detail?: string
  ip?: string
  result: AuditResult
  level: LogLevel
  created_at: string
}

/** 用户日志查询参数（通用条件 + 操作人/动作/关联对象）。 */
export interface UserLogQuery extends LogQuery {
  admin_id?: string
  action?: string
  resource_type?: string
  resource_id?: string
}

/** 系统日志（服务运行状态记录）。 */
export interface SystemLogInfo {
  id: string
  level: LogLevel
  event?: SystemLogEvent | string
  message?: string
  created_at: string
}

/** 系统日志查询参数（通用条件 + 事件）。 */
export interface SystemLogQuery extends LogQuery {
  event?: string
}

/** 测试执行日志（按任务定位 + 任务内增量序号）。 */
export interface TestLogInfo {
  id: string
  task_id?: string
  seq: number
  level: LogLevel
  message?: string
  created_at: string
}

/** 测试日志查询参数（通用条件 + 任务定位）。 */
export interface TestLogQuery extends LogQuery {
  task_id?: string
}

/** 日志类型（log_setting.log_types，PRD §3.5 日志设置）。 */
export type LogType = 'user' | 'system' | 'test' | 'request'

/** 请求详情（request_log）：压测时对被测接口的逐请求明细。 */
export interface RequestLogInfo {
  id: string
  task_id?: string
  interface_id?: string
  seq: number
  occurred_at: string
  method: string
  url: string
  request_body?: string
  response_status: number
  response_body?: string
  error_code?: string
  error_message?: string
  duration_ms: number
}

/** 请求详情查询参数（任务/接口定位 + 关键字/时间范围 + 分页）。 */
export interface RequestLogQuery {
  task_id?: string
  interface_id?: string
  keyword?: string
  start_time?: string
  end_time?: string
  page?: number
  page_size?: number
}

/** 日志设置（GET/PUT /log-settings）：服务开关 + 最大条数 + 存储天数 + 启用类型。 */
export interface LogSettingInfo {
  enabled: boolean
  max_count: number
  retention_days: number
  log_types: LogType[]
  updated_at?: string
}

/** 日志设置保存载荷（未提供的字段沿用服务端当前值）。 */
export interface LogSettingPayload {
  enabled?: boolean
  max_count?: number
  retention_days?: number
  log_types?: LogType[]
}

// ===================== 实时通道（api-contract §2.4 / frontend-structure §3.1） =====================

/** WebSocket 推送信封：`{channel,event,data,time}`。 */
export interface WsEnvelope<T = unknown> {
  channel: string
  event: string
  data: T
  time?: string
}

/** 任务状态推送（event=status）。 */
export interface TaskStatusEvent {
  status: TaskStatus
  message?: string
}

/** 任务实时日志推送（event=log，服务端已脱敏）。 */
export interface TaskLogEvent {
  task_id?: string
  seq: number
  level: LogLevel | string
  message: string
  time: string
}

/** 任务实时指标推送（event=metrics，归一化指标）。 */
export type TaskMetricsEvent = Record<string, number>

// ===================== 系统设置（PRD §3.6 / §4.12~4.13 / api-contract §2.7） =====================

/** 证书状态（由有效期推导，PRD §3.6.4）。 */
export type CertificateStatus = 'valid' | 'expiring' | 'expired'

/** 证书（脱敏：后端不回传证书内容与私钥，仅 has_key）。 */
export interface CertificateInfo {
  id: string
  name: string
  issuer?: string
  valid_from?: string
  valid_to?: string
  associated_service?: string
  is_builtin: boolean
  status: CertificateStatus
  has_key: boolean
  version: number
  created_at?: string
  updated_at?: string
}

/** 证书导入表单（multipart：证书文件必填，私钥文件可选）。 */
export interface CertificateImportPayload {
  name: string
  issuer?: string
  valid_from?: string
  valid_to?: string
  associated_service?: string
  cert_file: File
  key_file?: File | null
}

/** SSH/Host 记录类型与认证方式（PRD §4.13）。 */
export type SshHostType = 'host' | 'ssh'
export type SshAuthType = 'password' | 'key'

/** SSH/Host 记录（私钥脱敏为 has_key）。 */
export interface SshHostInfo {
  id: string
  type: SshHostType
  host: string
  port?: number
  user?: string
  auth_type?: SshAuthType | ''
  has_key: boolean
  host_entries: string[]
  description?: string
  version: number
  created_at?: string
  updated_at?: string
}

/** SSH/Host 新增/修改请求（key 为私钥明文，仅提交不落前端缓存）。 */
export interface SshHostPayload {
  type: SshHostType
  host: string
  port?: number
  user?: string
  auth_type?: SshAuthType | ''
  key?: string
  host_entries?: string[]
  description?: string
  version?: number
}

/** SSH/Host 列表查询。 */
export interface SshHostQuery {
  keyword?: string
  type?: string
  page?: number
  page_size?: number
}

/** SSH/Host 批量导入结果（重复项跳过，非法行回报样例）。 */
export interface SshHostImportResult {
  total: number
  created: number
  skipped: number
  invalid: number
  errors: string[]
}
