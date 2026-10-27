import type { Directive, DirectiveBinding } from 'vue'
import { useAuthStore } from '@/stores/auth'
import type { PermissionLevel } from '@/utils/permission'

/** 指令取值：'module.write' / 'module:read' 或 { module, permission }。 */
export type PermissionValue = string | { module: string; permission?: PermissionLevel }

interface ResolvedPermission {
  module: string
  permission: PermissionLevel
}

/** 解析指令取值（默认 read）。 */
function resolvePermission(value: PermissionValue | undefined): ResolvedPermission | null {
  if (typeof value === 'string') {
    const [module, rawPermission] = value.split(/[.:]/)
    if (!module) return null
    return { module, permission: rawPermission === 'write' ? 'write' : 'read' }
  }
  if (value && typeof value === 'object' && value.module) {
    return { module: value.module, permission: value.permission === 'write' ? 'write' : 'read' }
  }
  return null
}

/** 应用权限：无权时隐藏元素（服务端仍会强制鉴权）。 */
function apply(el: HTMLElement, binding: DirectiveBinding<PermissionValue>): void {
  const target = resolvePermission(binding.value)
  if (!target) {
    el.style.display = ''
    return
  }
  const allowed = useAuthStore().hasPerm(target.module, target.permission)
  el.style.display = allowed ? '' : 'none'
  if (allowed) {
    el.removeAttribute('aria-hidden')
  } else {
    el.setAttribute('aria-hidden', 'true')
  }
}

/**
 * v-permission 指令：按「子模块 + read/write」控制按钮级显隐。
 *
 * 用法：
 *   <button v-permission="'env.list.write'">新建环境</button>
 *   <button v-permission="{ module: 'setting.admin', permission: 'write' }">新建管理员</button>
 */
export const permissionDirective: Directive<HTMLElement, PermissionValue> = {
  mounted: apply,
  updated: apply,
}
