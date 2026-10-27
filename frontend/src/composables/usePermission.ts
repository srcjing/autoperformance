import { useAuthStore } from '@/stores/auth'
import type { PermissionLevel } from '@/utils/permission'

/**
 * 权限组合式函数（对齐 frontend-structure.md §4/§6）：
 * 按钮级权限通过 usePermission().hasPerm(module, perm) 控制显隐/禁用。
 *
 * 注意：前端隐藏不替代服务端鉴权（INV-1），所有写操作仍由后端 AuthZ 强制校验。
 */
export function usePermission() {
  const auth = useAuthStore()

  const hasPerm = (module: string, permission: PermissionLevel = 'read'): boolean =>
    auth.hasPerm(module, permission)

  const canRead = (module: string): boolean => hasPerm(module, 'read')
  const canWrite = (module: string): boolean => hasPerm(module, 'write')

  return { hasPerm, canRead, canWrite }
}
