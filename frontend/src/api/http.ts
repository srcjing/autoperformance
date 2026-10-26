import type { ApiResponse } from '@/types'

// 统一 API 客户端（骨架：基于 fetch，后续可切换为 axios 封装）。
// 对齐 api-contract.md §1.2：所有接口返回统一结构 { code, message, data }。
const BASE_URL = '/api/v1'
const TOKEN_KEY = 'ap_token'

export async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const token = localStorage.getItem(TOKEN_KEY)

  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options.headers as Record<string, string> | undefined),
  }
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }

  const res = await fetch(`${BASE_URL}${path}`, { ...options, headers })
  const body = (await res.json()) as ApiResponse<T>

  if (body.code !== 0) {
    throw new Error(body.message || `请求失败(${body.code})`)
  }
  return body.data
}
