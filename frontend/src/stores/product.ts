import { defineStore } from 'pinia'
import { ref } from 'vue'
import {
  createInterface,
  createProduct,
  deleteInterface,
	deleteProduct,
	getProduct,
	listInterfaces,
  listProducts,
  listThresholds,
  replaceThresholds,
  updateInterface,
  updateInterfaceStatus,
  updateProduct,
  updateProductStatus,
} from '@/api/product'
import type {
  InterfaceInfo,
  InterfacePayload,
  InterfaceQuery,
  InterfaceStatus,
  ProductInfo,
  ProductPayload,
  ProductQuery,
  ProductStatus,
  ThresholdInfo,
  ThresholdPayload,
} from '@/types'

// 产品 Store（frontend-structure.md §3）：产品、接口、阈值的跨页面共享数据。
// 页面局部状态（弹窗开关/表单草稿）留在组件，领域数据在此集中，避免重复请求。
export const useProductStore = defineStore('product', () => {
  const products = ref<ProductInfo[]>([])
  const total = ref(0)
  const page = ref(1)
  const pageSize = ref(20)
  const loading = ref(false)

  const activeProduct = ref<ProductInfo | null>(null)
  const interfaces = ref<InterfaceInfo[]>([])
  const interfaceTotal = ref(0)
  const interfacePage = ref(1)
  const interfacePageSize = ref(20)
  const interfaceLoading = ref(false)

  /** 按接口 ID 缓存的阈值列表（阈值编辑器打开时按需加载）。 */
  const thresholds = ref<Record<string, ThresholdInfo[]>>({})

  /** 产品列表（筛选 + 分页）。 */
  async function fetchList(query: ProductQuery = {}): Promise<void> {
    loading.value = true
    try {
      const res = await listProducts(query)
      products.value = res.list
      total.value = res.total
      page.value = res.page
      pageSize.value = res.page_size
    } finally {
      loading.value = false
    }
  }

  /** 新建/修改产品（id 为空表示新建）。 */
  async function saveProduct(id: string | null, payload: ProductPayload): Promise<ProductInfo> {
    return id ? await updateProduct(id, payload) : await createProduct(payload)
  }

  /** 删除产品（逻辑删除）。 */
  async function removeProduct(id: string): Promise<void> {
    await deleteProduct(id)
  }

  /** 停用/启用产品。 */
  async function setStatus(id: string, status: ProductStatus): Promise<void> {
    await updateProductStatus(id, status)
  }

  /** 产品下接口列表（同时刷新当前产品详情）。 */
  async function fetchInterfaces(
    productId: string,
    query: InterfaceQuery = {},
  ): Promise<void> {
    interfaceLoading.value = true
    try {
      const [res, product] = await Promise.all([
        listInterfaces(productId, query),
        productInfo(productId),
      ])
      interfaces.value = res.list
      interfaceTotal.value = res.total
      interfacePage.value = res.page
      interfacePageSize.value = res.page_size
      activeProduct.value = product
    } finally {
      interfaceLoading.value = false
    }
  }

	/** 读取产品详情（详情失败时返回 null，由调用页给出提示，如 10301 产品不存在）。 */
	async function productInfo(id: string): Promise<ProductInfo | null> {
		try {
			return await getProduct(id)
		} catch {
			return null
		}
	}

  /** 清空当前产品的接口列表（未选产品/加载失败时避免展示上一个产品的脏数据）。 */
  function clearInterfaces(): void {
    interfaces.value = []
    interfaceTotal.value = 0
    interfacePage.value = 1
    activeProduct.value = null
  }

  /** 新建/修改接口（id 为空表示新建，需提供 productId）。 */
  async function saveInterface(
    productId: string,
    id: string | null,
    payload: InterfacePayload,
  ): Promise<InterfaceInfo> {
    return id ? await updateInterface(id, payload) : await createInterface(productId, payload)
  }

  /** 删除接口（逻辑删除）。 */
  async function removeInterface(id: string): Promise<void> {
    await deleteInterface(id)
  }

  /** 停用/启用接口。 */
  async function setInterfaceStatus(id: string, status: InterfaceStatus): Promise<void> {
    await updateInterfaceStatus(id, status)
  }

  /** 加载接口阈值（按接口缓存）。 */
  async function fetchThresholds(interfaceId: string): Promise<ThresholdInfo[]> {
    const res = await listThresholds(interfaceId)
    thresholds.value = { ...thresholds.value, [interfaceId]: res.list }
    return res.list
  }

  /** 全量替换接口阈值（指标方向不匹配 22003）。 */
  async function saveThresholds(
    interfaceId: string,
    items: ThresholdPayload[],
  ): Promise<ThresholdInfo[]> {
    const res = await replaceThresholds(interfaceId, items)
    thresholds.value = { ...thresholds.value, [interfaceId]: res.list }
    return res.list
  }

  return {
    products,
    total,
    page,
    pageSize,
    loading,
    activeProduct,
    interfaces,
    interfaceTotal,
    interfacePage,
    interfacePageSize,
    interfaceLoading,
    thresholds,
    fetchList,
    saveProduct,
    removeProduct,
    setStatus,
    fetchInterfaces,
    clearInterfaces,
    saveInterface,
    removeInterface,
    setInterfaceStatus,
    fetchThresholds,
    saveThresholds,
  }
})
