import { defineStore } from 'pinia'
import { computed, ref } from 'vue'
import { fetchMe as fetchMeApi, login as loginApi, logout as logoutApi } from '@/api/auth'
import { clearToken, getToken, setToken } from '@/api/http'
import type { AdminInfo, LoginResult } from '@/types'
import { checkPermission, type PermissionLevel } from '@/utils/permission'

// 认证 Store（对齐 frontend-structure.md §3 / §6）：
// 维护登录态、令牌、权限集合、首登改密标记；hasPerm 支撑路由守卫与按钮级权限。
// 权限不落 localStorage（避免篡改），刷新后由 GET /auth/me 重新加载。
export const useAuthStore = defineStore('auth', () => {
  const token = ref<string>(getToken())
  const user = ref<AdminInfo | null>(null)
  const permissions = ref<Set<string>>(new Set())
  const mustChangePassword = ref(false)
  const profileLoaded = ref(false)

  const isAuthenticated = computed(() => !!token.value)

  /** 是否具备子模块权限（write 蕴含 read，敏感子模块除外）。 */
  function hasPerm(module: string, permission: PermissionLevel = 'read'): boolean {
    return checkPermission(permissions.value, module, permission)
  }

  // 登录后默认落地页：按权限顺序取第一个可访问的功能，避免无权账号直接撞 403。
  const HOME_CANDIDATES: Array<{ path: string; module: string; permission?: PermissionLevel }> = [
    { path: '/environments/list', module: 'env.list' },
    { path: '/products/list', module: 'product.list' },
    { path: '/testing/tasks', module: 'testing.task' },
    { path: '/reports/list', module: 'report.list' },
    { path: '/logs/user', module: 'log.user' },
    { path: '/logs/system', module: 'log.system' },
    { path: '/logs/test', module: 'log.test' },
    { path: '/logs/request', module: 'log.request' },
    { path: '/logs/settings', module: 'setting.log' },
    { path: '/settings/admins', module: 'setting.admin', permission: 'write' },
    { path: '/settings/certificates', module: 'setting.cert', permission: 'write' },
    { path: '/settings/ssh-hosts', module: 'setting.ssh', permission: 'write' },
  ]

  /** 当前账号的默认落地路径（无任何权限时为 /403）。 */
  const homePath = computed<string>(() => {
    const hit = HOME_CANDIDATES.find((item) => hasPerm(item.module, item.permission ?? 'read'))
    return hit?.path ?? '/403'
  })

  /** 写入登录会话（登录成功后调用）。 */
  function setSession(
    newToken: string,
    admin: AdminInfo,
    perms: string[],
    mustChange: boolean,
  ): void {
    token.value = newToken
    user.value = admin
    permissions.value = new Set(perms)
    mustChangePassword.value = mustChange
    profileLoaded.value = true
    setToken(newToken)
  }

  /** 登录：认证成功即写入会话（含权限并集与首登改密标记）。 */
  async function signIn(username: string, password: string): Promise<LoginResult> {
    const res = await loginApi(username, password)
    setSession(res.token, res.admin, res.permissions, res.must_change_password)
    return res
  }

  /** 加载当前用户资料与权限集合（页面刷新/路由守卫调用）。 */
  async function loadProfile(force = false): Promise<void> {
    if (profileLoaded.value && !force) return
    const res = await fetchMeApi()
    user.value = res.admin
    permissions.value = new Set(res.permissions)
    mustChangePassword.value = Boolean(res.admin.must_change_password)
    profileLoaded.value = true
  }

  /** 清理本地会话（不发起请求）。 */
  function clearSession(): void {
    token.value = ''
    user.value = null
    permissions.value = new Set()
    mustChangePassword.value = false
    profileLoaded.value = false
    clearToken()
  }

  /** 登出：服务端失效 token（失败也清理本地会话）。 */
  async function signOut(): Promise<void> {
    try {
      if (token.value) await logoutApi()
    } catch {
      // 网络/会话异常不阻塞登出，本地清理即可。
    } finally {
      clearSession()
    }
  }

  return {
    token,
    user,
    permissions,
    mustChangePassword,
    profileLoaded,
    isAuthenticated,
    homePath,
    hasPerm,
    setSession,
    signIn,
    loadProfile,
    clearSession,
    signOut,
  }
})
