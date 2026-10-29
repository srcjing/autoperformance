import { request } from './http'
import type { ChangePasswordPayload, LoginResult, MeResult } from '@/types'

// 认证接口（api-contract §2.1）。

/** 登录（失败统一提示「用户名或密码错误」20001，锁定 10502）。 */
export function login(username: string, password: string): Promise<LoginResult> {
  return request<LoginResult>('/auth/login', { method: 'POST', body: { username, password } })
}

/** 登出（当前 token 立即失效）。 */
export function logout(): Promise<null> {
  return request<null>('/auth/logout', { method: 'POST' })
}

/** 修改密码（首登强制改密同此接口：20003/20004/20005）。 */
export function changePassword(payload: ChangePasswordPayload): Promise<null> {
  return request<null>('/auth/change-password', { method: 'POST', body: payload })
}

/** 查询当前用户与权限并集（路由守卫据此加载权限集合）。 */
export function fetchMe(): Promise<MeResult> {
  return request<MeResult>('/auth/me')
}
