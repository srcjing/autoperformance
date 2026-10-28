// 请求体「缩进键值行」与 JSON 的互转。
//
// 行是有层级的数据：容器行（对象/数组）承载子级，叶子行（字符串/数字/布尔/null）承载值；
// 层级由 depth 表达（0 为根层），由「+ 嵌套」按钮在容器行下生成子级行。
// 数组元素行用 `[0]` 形式的键名表示。

/** 值类型。 */
export type BodyValueType =
  | 'string'
  | 'number'
  | 'boolean'
  | 'null'
  | 'object'
  | 'array'
  // json：JSON 片段整值（兼容手工填写 `{}` / `[]` 等）
  | 'json'

/** 请求体表格中的一行。 */
export interface BodyRow {
  /** 键名（数组元素写 `[0]`）。 */
  key: string
  value: string
  type: BodyValueType
  /** 嵌套层级：0 为根层，n 为上一层容器的子键。 */
  depth: number
  enabled: boolean
}

/** 是否容器行（对象/数组）。 */
export function isContainer(row: BodyRow): boolean {
  return row.type === 'object' || row.type === 'array'
}

/** 键名 → 访问令牌序列（对象键为 string，数组下标为 number）。 */
export function parseKey(key: string): (string | number)[] {
  const tokens: (string | number)[] = []
  let i = 0
  const text = key.trim()
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
    const part = text.slice(i, end)
    if (!part) throw new Error('键名为空')
    tokens.push(part)
    i = end
    if (text[i] === '.') i += 1
  }
  if (tokens.length === 0) throw new Error('键名为空')
  return tokens
}

/** JSON 值 → 表格行（容器行 + 叶子行，depth 递增）。 */
export function flattenJson(value: unknown): BodyRow[] {
  const rows: BodyRow[] = []
  if (value === null || value === undefined) return rows
  if (Array.isArray(value)) {
    value.forEach((item, index) => walk(item, 0, `[${index}]`, rows))
    return rows
  }
  if (typeof value === 'object') {
    for (const [key, item] of Object.entries(value as Record<string, unknown>)) {
      walk(item, 0, key, rows)
    }
  }
  return rows
}

function walk(value: unknown, depth: number, key: string, rows: BodyRow[]): void {
  if (Array.isArray(value)) {
    rows.push({ key, value: '', type: 'array', depth, enabled: true })
    value.forEach((item, index) => walk(item, depth + 1, `[${index}]`, rows))
    return
  }
  if (value !== null && typeof value === 'object') {
    rows.push({ key, value: '', type: 'object', depth, enabled: true })
    for (const [childKey, item] of Object.entries(value as Record<string, unknown>)) {
      walk(item, depth + 1, childKey, rows)
    }
    return
  }
  if (value === null) {
    rows.push({ key, value: '', type: 'null', depth, enabled: true })
    return
  }
  const type: BodyValueType =
    typeof value === 'number' ? 'number' : typeof value === 'boolean' ? 'boolean' : 'string'
  rows.push({ key, value: String(value), type, depth, enabled: true })
}

/** 叶子行 → 字面量（变量占位符按字符串原样保留）。 */
function convertValue(row: BodyRow): unknown {
  switch (row.type) {
    case 'object':
      return {}
    case 'array':
      return []
    case 'null':
      return null
    case 'number': {
      const num = Number(row.value)
      return Number.isFinite(num) ? num : row.value
    }
    case 'boolean':
      return row.value.trim() === 'true'
    case 'json': {
      // 兼容历史数据：JSON 片段整值
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

/** 表格行 → JSON 值（容器行先建空容器，子级行再填充）。 */
export function buildJson(rows: BodyRow[]): { value: unknown; error: string } {
  let root: unknown = undefined
  const stack: string[] = []
  for (const row of rows) {
    if (!row.enabled) continue
    const key = row.key.trim()
    if (!key) continue
    const depth = Math.max(0, Math.min(row.depth, stack.length))
    stack.length = depth
    stack[depth] = key

    let tokens: (string | number)[] = []
    try {
      for (const part of stack) tokens.push(...parseKey(part))
    } catch (err) {
      return { value: null, error: `键名非法「${key}」：${(err as Error).message}` }
    }

    let converted: unknown
    try {
      converted = convertValue(row)
    } catch {
      return { value: null, error: `「${key}」的值不是合法 JSON` }
    }
    root = assign(root, tokens, converted)
  }
  return { value: root === undefined ? null : root, error: '' }
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
