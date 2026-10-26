// 共享 TS 类型（对齐 api-contract.md 关键数据结构）。

export interface AdminInfo {
  id: string
  username: string
  name: string
}

export interface LoginResult {
  token: string
  expires_in: number
  admin: AdminInfo
  permissions: string[]
  must_change_password: boolean
}

export interface PageResult<T> {
  list: T[]
  total: number
  page: number
  page_size: number
}

export interface ApiResponse<T> {
  code: number
  message: string
  data: T
}
