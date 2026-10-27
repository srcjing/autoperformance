// 通用工具函数（时间展示 / 筛选参数转换）。

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
