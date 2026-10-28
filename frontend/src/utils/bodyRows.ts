// 请求体「路径键值行」与 JSON 的互转（支持嵌套对象与数组）。
//
// 设计：一行 = 一个叶子值，路径用 `a.b` / `list[0].id` 表达层级；
// 空对象/空数组用类型 `json` 整值表达（如 `{}` / `[]`）。

/** 值类型（决定生成 JSON 时的字面量形态）。 */
export type BodyValueType = 'string' | 'number' | 'boolean' | 'null' | 'json'

/** 请求体表格中的一行。 */
export interface BodyRow {
  path: string
  value: string
  type: BodyValueType
  enabled: boolean
}

/** 路径 → 访问令牌序列（对象键为 string，数组下标为 number）。 */
export function parsePath(path: string): (string | number)[] {
  const tokens: (string | number)[] = []
  let i = 0
  const text = path.trim()
  while (i < text.length) {
    if (text[i] === '[') {
      const end = text.indexOf(']', i)
      if (end < 0) throw new Error('缺少 ]')
      const index = text.slice(i + 1, end)
      if (!/^\d+$/.test(index)) throw new Error(`数组下标非法：${index}`)
      tokens.push(Number(index))
      i = end + 1
      if (text[i] === '.') i += 1
      continue
    }
    let end = i
    while (end < text.length && text[end] !== '.' && text[end] !== '[') end += 1
    const key = text.slice(i, end)
    if (!key) throw new Error('路径段为空')
    tokens.push(key)
    i = end
    if (text[i] === '.') i += 1
  }
  if (tokens.length === 0) throw new Error('路径为空')
  return tokens
}

/** 叶子值 → 表格行（对象/数组递归展开）。 */
export function flattenJson(value: unknown, prefix = '', rows: BodyRow[] = []): BodyRow[] {
  if (Array.isArray(value)) {
    if (value.length === 0) {
      rows.push({ path: prefix || '[]', value: '[]', type: 'json', enabled: true })
      return rows
    }
    value.forEach((item, index) => flattenJson(item, `${prefix}[${index}]`, rows))
    return rows
  }
  if (value !== null && typeof value === 'object') {
    const entries = Object.entries(value as Record<string, unknown>)
    if (entries.length === 0) {
      rows.push({ path: prefix || '{}', value: '{}', type: 'json', enabled: true })
      return rows
    }
    for (const [key, item] of entries) {
      flattenJson(item, prefix ? `${prefix}.${key}` : key, rows)
    }
    return rows
  }
  if (value === null) {
    rows.push({ path: prefix, value: '', type: 'null', enabled: true })
    return rows
  }
  const type: BodyValueType =
    typeof value === 'number' ? 'number' : typeof value === 'boolean' ? 'boolean' : 'string'
  rows.push({ path: prefix, value: String(value), type, enabled: true })
  return rows
}

/** 表格行 → 字面量（变量占位符按字符串原样保留）。 */
function convertValue(row: BodyRow): unknown {
  switch (row.type) {
    case 'null':
      return null
    case 'number': {
      const num = Number(row.value)
      return Number.isFinite(num) ? num : row.value
    }
    case 'boolean':
      return row.value.trim() === 'true'
    case 'json': {
      const text = row.value.trim()
      if (!text) return null
      return JSON.parse(text)
    }
    default:
      return row.value
  }
}

function assign(root: unknown, tokens: (string | number)[], value: unknown): unknown {
  if (tokens.length === 0) return value
  const [head, ...rest] = tokens
  if (typeof head === 'number') {
    const arr = Array.isArray(root) ? [...root] : []
    arr[head] = assign(arr[head], rest, value)
    return arr
  }
  const obj: Record<string, unknown> =
    root !== null && typeof root === 'object' && !Array.isArray(root)
      ? { ...(root as Record<string, unknown>) }
      : {}
  obj[head] = assign(obj[head], rest, value)
  return obj
}

/** 表格行 → JSON 值。 */
export function buildJson(rows: BodyRow[]): { value: unknown; error: string } {
  let root: unknown = undefined
  let hasRow = false
  for (const row of rows) {
    if (!row.enabled) continue
    const path = row.path.trim()
    if (!path) continue
    let tokens: (string | number)[]
    try {
      tokens = parsePath(path)
    } catch (err) {
      return { value: null, error: `路径非法「${path}」：${(err as Error).message}` }
    }
    let converted: unknown
    try {
      converted = convertValue(row)
    } catch {
      return { value: null, error: `「${path}」的值不是合法 JSON` }
    }
    root = assign(root, tokens, converted)
    hasRow = true
  }
  return { value: hasRow ? root : null, error: '' }
}

/** JSON 文本 → 表格行（空/非法返回错误）。 */
export function jsonTextToRows(text: string): { rows: BodyRow[]; error: string } {
  const trimmed = text.trim()
  if (!trimmed) return { rows: [], error: '' }
  try {
    const parsed = JSON.parse(trimmed)
    return { rows: flattenJson(parsed), error: '' }
  } catch (err) {
    return { rows: [], error: `JSON 解析失败：${(err as Error).message}` }
  }
}
