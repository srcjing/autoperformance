// 权限判定工具（与后端 RBAC 语义保持一致，见 internal/auth/permission.go）。

/** 权限级别。 */
export type PermissionLevel = 'read' | 'write'

/**
 * 敏感子模块（仅 write 授权，write 不蕴含 read）。
 * 与后端 auth.SensitiveModules 对齐；前端隐藏不替代服务端鉴权。
 */
export const SENSITIVE_MODULES: ReadonlySet<string> = new Set([
  'setting.admin',
  'setting.cert',
  'setting.ssh',
])

/**
 * 判断权限集合是否包含 module:permission。
 * write 蕴含 read（敏感子模块除外，与后端 HasPerm 一致）。
 */
export function checkPermission(
  permissions: ReadonlySet<string>,
  module: string,
  permission: PermissionLevel = 'read',
): boolean {
  if (!module) return false
  if (permissions.has(`${module}:${permission}`)) return true
  if (permission === 'read' && !SENSITIVE_MODULES.has(module) && permissions.has(`${module}:write`)) {
    return true
  }
  return false
}
