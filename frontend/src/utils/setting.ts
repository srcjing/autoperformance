import { ApiError } from '@/api/http'
import type { CertificateStatus, PermissionItem, PermissionModule } from '@/types'

// 系统设置展示元数据与错误文案（管理员 / 角色 / 证书 / SSH-Host，api-contract §2.7）。

/** 徽标元数据（类名以 ap-badge-* 复用全局样式）。 */
export interface BadgeMeta {
  label: string
  badge: string
}

/** 证书状态（由有效期推导，PRD §3.6.4）。 */
export const CERT_STATUS_META: Record<string, BadgeMeta> = {
  valid: { label: '有效', badge: 'ap-badge-success' },
  expiring: { label: '即将过期', badge: 'ap-badge-warn' },
  expired: { label: '已过期', badge: 'ap-badge-danger' },
}

/** 管理员状态（PRD §3.6.4：启用 / 停用）。 */
export const ADMIN_STATUS_META: Record<string, BadgeMeta> = {
  enabled: { label: '启用', badge: 'ap-badge-success' },
  disabled: { label: '停用', badge: 'ap-badge-muted' },
}

/** 管理员状态元数据（未知状态兜底展示原值）。 */
export function adminStatusMeta(status: string | undefined): BadgeMeta {
  return ADMIN_STATUS_META[status ?? ''] ?? { label: status || '-', badge: 'ap-badge-muted' }
}

/**
 * 口令复杂度本地预校（与后端 auth.ValidatePassword 一致）：
 * ≥8 位、≤72 字节，且同时含大写/小写/数字/特殊字符；通过时返回空串。
 *
 * 前端校验仅用于即时反馈，最终以服务端 20004 为准。
 */
export function validatePasswordStrength(password: string): string {
  if (password.length < 8) return '口令至少 8 位'
  if (new TextEncoder().encode(password).length > 72) return '口令不得超过 72 字节'
  const missing: string[] = []
  if (!/[a-z]/.test(password)) missing.push('小写字母')
  if (!/[A-Z]/.test(password)) missing.push('大写字母')
  if (!/[0-9]/.test(password)) missing.push('数字')
  if (!/[^A-Za-z0-9\s]/.test(password)) missing.push('特殊字符')
  if (missing.length > 0) return `口令需包含${missing.join('、')}`
  return ''
}

/** 口令复杂度提示文案（登录/新建/重置三处共用）。 */
export const PASSWORD_POLICY_HINT = '至少 8 位（不超过 72 字节），需同时包含大写字母、小写字母、数字与特殊字符。'

/** 角色权限范围摘要（列表展示 + hover 明细）。 */
export interface RolePermissionSummary {
  /** 可写子模块数。 */
  write: number
  /** 只读子模块数。 */
  read: number
  /** 敏感子模块（仅可写）授权数。 */
  sensitive: number
  /** 明细文案（子模块名（可写/只读），顿号分隔）。 */
  detail: string
}

/**
 * 汇总角色权限矩阵（权限范围摘要）。
 *
 * 子模块展示名以 GET /permissions 元数据为准；元数据缺失时回退 module_code。
 */
export function summarizeRolePermissions(
  grants: PermissionItem[] | undefined,
  modules: PermissionModule[],
): RolePermissionSummary {
  const list = grants ?? []
  const moduleMap = new Map(modules.map((m) => [m.module_code, m]))
  let write = 0
  let read = 0
  let sensitive = 0
  const detail: string[] = []

  for (const grant of list) {
    const meta = moduleMap.get(grant.module)
    if (grant.permission === 'write') {
      write += 1
      if (meta?.sensitive) sensitive += 1
    } else {
      read += 1
    }
    const name = meta?.name ?? grant.module
    const shortName = name.includes('·') ? (name.split('·').pop() ?? name).trim() : name
    detail.push(`${shortName}（${grant.permission === 'write' ? '可写' : '只读'}）`)
  }

  return { write, read, sensitive, detail: detail.join('、') }
}

/** 证书状态元数据（未知状态兜底展示原值）。 */
export function certStatusMeta(status: string): BadgeMeta {
  return CERT_STATUS_META[status] ?? { label: status || '-', badge: 'ap-badge-muted' }
}

/** SSH/Host 记录类型文案。 */
export function sshTypeLabel(type: string): string {
  return type === 'host' ? 'Host 解析' : type === 'ssh' ? 'SSH 信任' : type || '-'
}

/** SSH 认证方式文案。 */
export function sshAuthLabel(authType: string | undefined, hasKey: boolean): string {
  if (authType === 'key') return hasKey ? '密钥（已加密保存）' : '密钥（未配置）'
  if (authType === 'password') return '密码'
  return '-'
}

/** 系统设置相关错误文案（管理员 / 角色 / 证书 / SSH，按业务码分支）。 */
export function describeSettingError(error: unknown, fallback: string): string {
  if (!(error instanceof ApiError)) return fallback
  switch (error.code) {
    case 26001:
      return '用户名已存在，请更换登录名'
    case 26002:
      return '内置 admin 为系统保护账号，不可删除或停用'
    case 26003:
      return '角色已被管理员引用，禁止删除；请先在管理员管理中解除该角色的授权'
    case 26004:
      return '内置自签证书不可删除，如需更新有效期请使用「重新初始化」'
    case 26005:
      return 'SSH/Host 已被环境引用，请先在环境管理中解除关联后再删除'
    case 20004:
      return `口令复杂度不合规：${PASSWORD_POLICY_HINT}`
    case 10401:
      return '仅内置自签证书支持重新初始化'
    case 10302:
      return '资源已存在（用户名/角色名/证书名重复），请调整后再试'
    case 10402:
      return '数据已被他人修改（并发冲突），请刷新后重试'
    case 24002:
      return '导入文件类型不允许：仅支持 JSON / CSV'
    case 10301:
      return '资源不存在或已被删除'
    default:
      return error.message || fallback
  }
}

/** 证书状态 → 说明文案（过期提醒）。 */
export function certExpiryHint(status: CertificateStatus, validTo?: string): string {
  if (status === 'expired') return `证书已于 ${validTo || '未知日期'} 过期，请尽快更新`
  if (status === 'expiring') return `证书将于 ${validTo || '近期'} 过期，请提前更换`
  return validTo ? `有效期至 ${validTo}` : '有效期未设置'
}
