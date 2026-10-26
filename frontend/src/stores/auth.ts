import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import type { AdminInfo } from '@/types'

const TOKEN_KEY = 'ap_token'

// 认证 Store（对齐 frontend-structure.md §3）：
// 维护登录态、令牌、权限集合；hasPerm(module, perm) 支撑路由守卫与按钮级权限。
export const useAuthStore = defineStore('auth', () => {
  const token = ref<string>(localStorage.getItem(TOKEN_KEY) ?? '')
  const user = ref<AdminInfo | null>(null)
  const permissions = ref<Set<string>>(new Set())

  const isAuthenticated = computed(() => !!token.value)

  // write 蕴含 read（与后端 RBAC 判定一致）
  function hasPerm(module: string, perm: 'read' | 'write'): boolean {
    if (permissions.value.has(`${module}:${perm}`)) return true
    if (perm === 'read' && permissions.value.has(`${module}:write`)) return true
    return false
  }

  function setSession(newToken: string, admin: AdminInfo, perms: string[]): void {
    token.value = newToken
    user.value = admin
    permissions.value = new Set(perms)
    localStorage.setItem(TOKEN_KEY, newToken)
  }

  function logout(): void {
    token.value = ''
    user.value = null
    permissions.value = new Set()
    localStorage.removeItem(TOKEN_KEY)
  }

  return { token, user, permissions, isAuthenticated, hasPerm, setSession, logout }
})
