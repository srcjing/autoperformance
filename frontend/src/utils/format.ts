// 通用工具函数（时间展示 / 接口地址展示 / 筛选参数转换）。

/** 接口地址（接口不保存 host，展示时用占位符表示由环境主机决定）。 */
export interface InterfaceAddressParts {
  scheme: string
  port: number
  path: string
}

/**
 * 接口地址展示文本：`{scheme}://<host>:{port}{path}`。
 *
 * 接口不再配置 host（需求 1），完整地址在创建任务时用所选环境的 target_host 拼出；
 * 列表页用 <host> 占位，避免把某个环境的 host 误当接口固有属性。
 */
export function formatInterfaceAddress(item: InterfaceAddressParts): string {
  const scheme = item.scheme || 'http'
  const port = item.port || 80
  const path = item.path || '/'
  return `${scheme}://<host>:${port}${path}`
}

export function formatDateTime(iso: string): string {
  if (!iso) return '-'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString()
}

/**
 * 日期（YYYY-MM-DD，本地日期控件值）→ RFC3339（UTC）。
 *
 * 列表筛选统一按 UTC 下发（api-contract §1.3 时间参数格式）：
 * 起始取当日 00:00:00Z，结束取当日 23:59:59Z；空值返回 undefined（表示不限）。
 */
export function dateToRfc3339(date: string, endOfDay = false): string | undefined {
  if (!date) return undefined
  return `${date}T${endOfDay ? '23:59:59' : '00:00:00'}Z`
}
