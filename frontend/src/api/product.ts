import { request } from './http'
import type {
  InterfaceInfo,
  InterfacePayload,
  InterfaceQuery,
  InterfaceStatus,
  PageResult,
  ProductInfo,
  ProductPayload,
  ProductQuery,
  ProductStatus,
  ThresholdInfo,
  ThresholdPayload,
} from '@/types'

// 产品管理接口（api-contract §2.3）。
// 权限点：产品 product.list，接口与阈值 product.interface；read 用于查询、write 用于变更。
// 安全：接口认证头只下发名称（auth_headers），值不出现在任何响应中（DES-I-001）。

/** 构造 query 字符串（跳过空值）。 */
function buildQuery(params: Record<string, string | number | undefined>): string {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === '') continue
    search.set(key, String(value))
  }
  const qs = search.toString()
  return qs ? `?${qs}` : ''
}

// ===================== 产品 =====================

/** 产品列表（关键字/版本/平台/状态 + 分页）。 */
export function listProducts(query: ProductQuery = {}): Promise<PageResult<ProductInfo>> {
  return request<PageResult<ProductInfo>>(`/products${buildQuery({ ...query })}`)
}

/** 产品详情（10301）。 */
export function getProduct(id: string): Promise<ProductInfo> {
  return request<ProductInfo>(`/products/${id}`)
}

/** 创建产品（编码重复 22001）。 */
export function createProduct(payload: ProductPayload): Promise<ProductInfo> {
  return request<ProductInfo>('/products', { method: 'POST', body: payload })
}

/** 修改产品（必须携带 version；冲突 10402）。 */
export function updateProduct(id: string, payload: ProductPayload): Promise<ProductInfo> {
  return request<ProductInfo>(`/products/${id}`, { method: 'PUT', body: payload })
}

/** 删除产品（逻辑删除；下有接口 22002）。 */
export function deleteProduct(id: string): Promise<null> {
  return request<null>(`/products/${id}`, { method: 'DELETE' })
}

/** 停用/启用产品（同状态 10401）。 */
export function updateProductStatus(id: string, status: ProductStatus): Promise<null> {
  return request<null>(`/products/${id}/status`, { method: 'PATCH', body: { status } })
}

// ===================== 接口 =====================

/** 产品下接口列表（关键字/状态 + 分页）。 */
export function listInterfaces(
  productId: string,
  query: InterfaceQuery = {},
): Promise<PageResult<InterfaceInfo>> {
  return request<PageResult<InterfaceInfo>>(
    `/products/${productId}/interfaces${buildQuery({ ...query })}`,
  )
}

/** 新建接口（headers 中的认证头由服务端加密存储）。 */
export function createInterface(
  productId: string,
  payload: InterfacePayload,
): Promise<InterfaceInfo> {
  return request<InterfaceInfo>(`/products/${productId}/interfaces`, {
    method: 'POST',
    body: payload,
  })
}

/** 修改接口（全量字段 + version；认证头留空表示沿用旧值）。 */
export function updateInterface(id: string, payload: InterfacePayload): Promise<InterfaceInfo> {
  return request<InterfaceInfo>(`/interfaces/${id}`, { method: 'PUT', body: payload })
}

/** 删除接口（逻辑删除）。 */
export function deleteInterface(id: string): Promise<null> {
  return request<null>(`/interfaces/${id}`, { method: 'DELETE' })
}

/** 停用/启用接口（同状态 10401）。 */
export function updateInterfaceStatus(id: string, status: InterfaceStatus): Promise<null> {
  return request<null>(`/interfaces/${id}/status`, { method: 'PATCH', body: { status } })
}

// ===================== 阈值 =====================

/** 接口阈值列表。 */
export function listThresholds(interfaceId: string): Promise<{ list: ThresholdInfo[] }> {
  return request<{ list: ThresholdInfo[] }>(`/interfaces/${interfaceId}/thresholds`)
}

/** 批量设置阈值（全量替换；指标方向不匹配 22003）。 */
export function replaceThresholds(
  interfaceId: string,
  thresholds: ThresholdPayload[],
): Promise<{ list: ThresholdInfo[] }> {
  return request<{ list: ThresholdInfo[] }>(`/interfaces/${interfaceId}/thresholds`, {
    method: 'PUT',
    body: { thresholds },
  })
}
