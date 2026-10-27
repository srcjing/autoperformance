import { defineStore } from 'pinia'
import { reactive } from 'vue'
import { listSystemLogs, listTestLogs, listUserLogs } from '@/api/log'
import type {
  SystemLogInfo,
  SystemLogQuery,
  TestLogInfo,
  TestLogQuery,
  UserLogInfo,
  UserLogQuery,
} from '@/types'

// 日志 Store（frontend-structure.md §3）：用户/系统/测试三类日志的分页列表状态。
// 三类日志相互独立（各自权限点 log.user / log.system / log.test）；
// 页面局部状态（筛选条件输入、当前页签名）留在组件内。

/** 单个日志列表的分页状态。 */
export interface LogListState<T> {
  list: T[]
  total: number
  page: number
  pageSize: number
  loading: boolean
}

/** 构造空列表状态（各类型独立实例，避免共享引用）。 */
function createListState<T>(): LogListState<T> {
  return { list: [], total: 0, page: 1, pageSize: 20, loading: false }
}

export const useLogStore = defineStore('log', () => {
  const userLogs = reactive<LogListState<UserLogInfo>>(createListState<UserLogInfo>())
  const systemLogs = reactive<LogListState<SystemLogInfo>>(createListState<SystemLogInfo>())
  const testLogs = reactive<LogListState<TestLogInfo>>(createListState<TestLogInfo>())

  /** 用户日志列表（筛选 + 分页）。 */
  async function fetchUserLogs(query: UserLogQuery = {}): Promise<void> {
    userLogs.loading = true
    try {
      const res = await listUserLogs(query)
      userLogs.list = res.list
      userLogs.total = res.total
      userLogs.page = res.page
      userLogs.pageSize = res.page_size
    } finally {
      userLogs.loading = false
    }
  }

  /** 系统日志列表（筛选 + 分页）。 */
  async function fetchSystemLogs(query: SystemLogQuery = {}): Promise<void> {
    systemLogs.loading = true
    try {
      const res = await listSystemLogs(query)
      systemLogs.list = res.list
      systemLogs.total = res.total
      systemLogs.page = res.page
      systemLogs.pageSize = res.page_size
    } finally {
      systemLogs.loading = false
    }
  }

  /** 测试日志列表（task_id 定位 + 筛选 + 分页）。 */
  async function fetchTestLogs(query: TestLogQuery = {}): Promise<void> {
    testLogs.loading = true
    try {
      const res = await listTestLogs(query)
      testLogs.list = res.list
      testLogs.total = res.total
      testLogs.page = res.page
      testLogs.pageSize = res.page_size
    } finally {
      testLogs.loading = false
    }
  }

  return {
    userLogs,
    systemLogs,
    testLogs,
    fetchUserLogs,
    fetchSystemLogs,
    fetchTestLogs,
  }
})
