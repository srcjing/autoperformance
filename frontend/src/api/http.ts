import type { ApiResponse } from '@/types'

// 统一 API 客户端（对齐 api-contract.md §1.2/§1.5）：
//   - 所有接口返回统一结构 { code, message, data }；
//   - HTTP 状态码做全局拦截（401 跳登录 / 403 跳 403 页 / 10103 跳改密页）；
//   - 业务码非 0 时抛 ApiError，由页面按 code 分支提示。
/** API 基础路径（二进制预览/下载复用鉴权头时需要）。 */
export const API_BASE_URL = '/api/v1'

const BASE_URL = API_BASE_URL
const TOKEN_KEY = 'ap_token'

/** 业务错误（携带后端 code，便于页面分支）。 */
export class ApiError extends Error {
  readonly code: number

  constructor(code: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.code = code
  }
}

/** 全局拦截回调（由 main.ts 注入路由/Store 依赖，避免 http ↔ router 循环依赖）。 */
export interface HttpHooks {
  /** 未认证/会话失效（10101/10102/10202）→ 清理会话并跳登录。 */
  onUnauthorized?: (code: number) => void
  /** 权限不足（10201 等）→ 跳 403 页。 */
  onForbidden?: (code: number) => void
  /** 首登强制改密（10103）→ 跳改密页。 */
  onMustChangePassword?: () => void
}

let hooks: HttpHooks = {}

/** 注册全局拦截回调。 */
export function setHttpHooks(next: HttpHooks): void {
  hooks = next
}

/** 读取持久化 token。 */
export function getToken(): string {
  return localStorage.getItem(TOKEN_KEY) ?? ''
}

/** 持久化 token。 */
export function setToken(token: string): void {
  localStorage.setItem(TOKEN_KEY, token)
}

/** 清除持久化 token。 */
export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY)
}

/** 请求选项：body 允许传普通对象（自动 JSON 序列化）。 */
export interface RequestOptions extends Omit<RequestInit, 'body'> {
  body?: unknown
}

/**
 * 发起请求并解包统一响应。
 *
 * @throws ApiError 业务失败（code≠0）或 HTTP 异常
 */
export async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const { body, headers: rawHeaders, ...rest } = options
  const headers = new Headers(rawHeaders as HeadersInit | undefined)

  const isForm = typeof FormData !== 'undefined' && body instanceof FormData
  if (body !== undefined && !isForm && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  const token = getToken()
  if (token && !headers.has('Authorization')) {
    headers.set('Authorization', `Bearer ${token}`)
  }

  const res = await fetch(`${BASE_URL}${path}`, {
    ...rest,
    headers,
    body: body === undefined ? undefined : isForm ? (body as FormData) : JSON.stringify(body),
  })

  let payload: ApiResponse<T> | null = null
  try {
    payload = (await res.json()) as ApiResponse<T>
  } catch {
    payload = null
  }

  const code = payload?.code ?? res.status
  const message = payload?.message || `请求失败(${res.status})`

  if (res.status === 401) {
    if (code === 10101 || code === 10102) {
      hooks.onUnauthorized?.(code)
    }
    throw new ApiError(code, message)
  }

  if (res.status === 403) {
    if (code === 10103) {
      hooks.onMustChangePassword?.()
    } else if (code === 10202) {
      // 账号已停用：会话不可继续使用，按未认证处理。
      hooks.onUnauthorized?.(code)
    } else {
      hooks.onForbidden?.(code)
    }
    throw new ApiError(code, message)
  }

  if (!payload) {
    throw new ApiError(10601, '响应格式非法')
  }
  if (payload.code !== 0) {
    throw new ApiError(payload.code, message)
  }
  return payload.data
}
