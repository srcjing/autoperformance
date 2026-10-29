import { defineStore } from 'pinia'
import { reactive, ref } from 'vue'
import {
  getLogSetting,
  listRequestLogs,
  listSystemLogs,
  listTestLogs,
  listUserLogs,
  updateLogSetting,
} from '@/api/log'
import type {
  LogSettingInfo,
  LogSettingPayload,
  RequestLogInfo,
  RequestLogQuery,
  SystemLogInfo,
  SystemLogQuery,
  TestLogInfo,
  TestLogQuery,
  UserLogInfo,
  UserLogQuery,
} from '@/types'

// 日志 Store（frontend-structure.md §3）：四类日志的分页列表状态 + 日志设置。
// 四类日志相互独立（各自权限点 log.user / log.system / log.test / log.request）；
// 日志设置为单例配置（setting.log:read 查看、setting.log:write 保存）。

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
  const requestLogs = reactive<LogListState<RequestLogInfo>>(createListState<RequestLogInfo>())

  /** 日志设置（null 表示尚未加载）。 */
  const setting = ref<LogSettingInfo | null>(null)
  const settingLoading = ref(false)
  const settingSaving = ref(false)

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

  /** 请求详情列表（任务/接口定位 + 筛选 + 分页）。 */
  async function fetchRequestLogs(query: RequestLogQuery = {}): Promise<void> {
    requestLogs.loading = true
    try {
      const res = await listRequestLogs(query)
      requestLogs.list = res.list
      requestLogs.total = res.total
      requestLogs.page = res.page
      requestLogs.pageSize = res.page_size
    } finally {
      requestLogs.loading = false
    }
  }

  /** 读取日志设置。 */
  async function fetchSetting(): Promise<void> {
    settingLoading.value = true
    try {
      setting.value = await getLogSetting()
    } finally {
      settingLoading.value = false
    }
  }

  /** 保存日志设置（成功后以服务端返回值为准）。 */
  async function saveSetting(payload: LogSettingPayload): Promise<LogSettingInfo> {
    settingSaving.value = true
    try {
      const saved = await updateLogSetting(payload)
      setting.value = saved
      return saved
    } finally {
      settingSaving.value = false
    }
  }

  return {
    userLogs,
    systemLogs,
    testLogs,
    requestLogs,
    setting,
    settingLoading,
    settingSaving,
    fetchUserLogs,
    fetchSystemLogs,
    fetchTestLogs,
    fetchRequestLogs,
    fetchSetting,
    saveSetting,
  }
})
