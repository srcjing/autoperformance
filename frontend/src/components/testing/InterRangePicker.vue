<template>
  <div class="range-picker">
    <div class="range-head">
      <span class="range-summary">已选 {{ modelValue.length }} 个接口</span>
      <input v-model="keyword" class="range-search" placeholder="搜索产品/接口" maxlength="64" />
      <button
        type="button"
        class="ap-btn ap-btn-ghost ap-btn-sm"
        :disabled="disabled || modelValue.length === 0"
        @click="clearAll"
      >
        清空选择
      </button>
    </div>

    <p v-if="errorMessage" class="ap-alert ap-alert-error" role="alert">{{ errorMessage }}</p>
    <p v-else-if="!canReadProducts" class="ap-alert ap-alert-hint">
      缺少「产品列表」读取权限，无法选择接口范围
    </p>

    <div v-else class="product-list">
      <p v-if="loadingProducts" class="ap-muted">加载产品…</p>
      <p v-else-if="filteredProducts.length === 0" class="ap-muted">暂无启用的产品</p>

      <div v-for="product in filteredProducts" :key="product.id" class="product-row">
        <div class="product-head">
          <button type="button" class="ap-link product-toggle" @click="toggleExpand(product.id)">
            {{ expanded[product.id] ? '▾' : '▸' }} {{ product.name }}（{{ product.code }}）
          </button>
          <span class="ap-muted">{{ selectedCount(product.id) }} / {{ interfaceCount(product.id) }}</span>
          <button
            type="button"
            class="ap-btn ap-btn-ghost ap-btn-sm"
            :disabled="disabled || interfacesOf(product.id).length === 0"
            @click="selectProduct(product.id, true)"
          >
            全选
          </button>
          <button
            type="button"
            class="ap-btn ap-btn-ghost ap-btn-sm"
            :disabled="disabled || interfacesOf(product.id).length === 0"
            @click="selectProduct(product.id, false)"
          >
            清空
          </button>
        </div>

        <div v-if="expanded[product.id]" class="product-body">
          <p v-if="loadingProductIds[product.id]" class="ap-muted">加载接口…</p>
          <p v-else-if="interfacesOf(product.id).length === 0" class="ap-muted">该产品暂无接口</p>
          <label
            v-for="item in filteredInterfaces(product.id)"
            :key="item.id"
            class="interface-row"
          >
            <input
              type="checkbox"
              :checked="selectedSet.has(item.id)"
              :disabled="disabled"
              @change="onToggleInterface(item.id, $event)"
            />
            <span class="ap-badge">{{ item.method }}</span>
            <span class="interface-name">{{ item.name }}</span>
            <span class="interface-url">{{ formatInterfaceAddress(item) }}</span>
          </label>
          <p
            v-if="filteredInterfaces(product.id).length === 0 && interfacesOf(product.id).length > 0"
            class="ap-muted"
          >
            无匹配接口
          </p>
        </div>
      </div>
    </div>

    <div v-if="selectedChips.length > 0" class="ap-chips selected-chips">
      <button
        v-for="chip in selectedChips"
        :key="chip.id"
        type="button"
        class="ap-chip"
        :disabled="disabled"
        @click="removeInterface(chip.id)"
      >
        {{ chip.label }} ×
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { listInterfaces, listProducts } from '@/api/product'
import { useAuthStore } from '@/stores/auth'
import type { InterfaceInfo, ProductInfo } from '@/types'
import { formatInterfaceAddress } from '@/utils/format'

// 接口范围选择器（PRD §3.2「按产品/按接口勾选」，api-contract §2.4 interface_ids）。
//   - 接口范围可跨产品；按产品展开后逐接口勾选，或按产品全选/清空；
//   - 关联产品（product_ids）由已选接口推导并回传，供任务创建时一并提交；
//   - 编辑回显：通过 productIds 展开对应产品，必要时顺序加载前 10 个产品补齐接口名称。
const props = defineProps<{
  modelValue: string[]
  /** 关联产品（编辑回显/名称解析用；由本组件通过 update:productIds 回传）。 */
  productIds?: string[]
  disabled?: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: string[]): void
  (e: 'update:productIds', value: string[]): void
}>()

const auth = useAuthStore()
const canReadProducts = computed(() => auth.hasPerm('product.list', 'read'))
const canReadInterfaces = computed(() => auth.hasPerm('product.interface', 'read'))

const products = ref<ProductInfo[]>([])
const interfacesByProduct = ref<Record<string, InterfaceInfo[]>>({})
const loadingProducts = ref(false)
const loadingProductIds = ref<Record<string, boolean>>({})
const expanded = ref<Record<string, boolean>>({})
const errorMessage = ref('')
const keyword = ref('')
let resolving = false

/** 单产品接口解析上限（page_size 上限 200，与后端 MaxPageSize 一致）。 */
const PAGE_LIMIT = 200

const selectedSet = computed(() => new Set(props.modelValue))

/** 已加载接口索引：interface_id → { 接口, 产品名 }。 */
const interfaceIndex = computed(() => {
  const index: Record<string, { item: InterfaceInfo; productName: string }> = {}
  for (const product of products.value) {
    for (const item of interfacesByProduct.value[product.id] ?? []) {
      index[item.id] = { item, productName: product.name }
    }
  }
  return index
})

const selectedChips = computed(() =>
  props.modelValue.map((id) => {
    const hit = interfaceIndex.value[id]
    return { id, label: hit ? `${hit.productName} · ${hit.item.name}` : `接口 #${id}` }
  }),
)

const filteredProducts = computed(() => {
  const text = keyword.value.trim().toLowerCase()
  if (!text) return products.value
  return products.value.filter((product) => {
    if (product.name.toLowerCase().includes(text) || product.code.toLowerCase().includes(text)) {
      return true
    }
    return (interfacesByProduct.value[product.id] ?? []).some(
      (item) =>
        item.name.toLowerCase().includes(text) || item.path.toLowerCase().includes(text),
    )
  })
})

function interfacesOf(productId: string): InterfaceInfo[] {
  return interfacesByProduct.value[productId] ?? []
}

function interfaceCount(productId: string): number | string {
  const list = interfacesByProduct.value[productId]
  return list ? list.length : '未加载'
}

function selectedCount(productId: string): number {
  return interfacesOf(productId).filter((item) => selectedSet.value.has(item.id)).length
}

function filteredInterfaces(productId: string): InterfaceInfo[] {
  const text = keyword.value.trim().toLowerCase()
  const list = interfacesOf(productId)
  if (!text) return list
  return list.filter(
    (item) =>
      item.name.toLowerCase().includes(text) ||
      item.path.toLowerCase().includes(text) ||
      item.method.toLowerCase().includes(text),
  )
}

/** 提交选择（同步回传关联产品，未解析的接口保留外部 product_ids 兜底）。 */
function commit(next: string[]): void {
  emit('update:modelValue', next)
  const resolved = new Set<string>()
  let hasUnresolved = false
  for (const id of next) {
    const hit = interfaceIndex.value[id]
    if (hit) resolved.add(hit.item.product_id)
    else hasUnresolved = true
  }
  if (hasUnresolved) {
    for (const productId of props.productIds ?? []) resolved.add(productId)
  }
  emit('update:productIds', Array.from(resolved))
}

function onToggleInterface(id: string, event: Event): void {
  const checked = (event.target as HTMLInputElement).checked
  const next = new Set(props.modelValue)
  if (checked) next.add(id)
  else next.delete(id)
  commit(Array.from(next))
}

function removeInterface(id: string): void {
  commit(props.modelValue.filter((item) => item !== id))
}

function clearAll(): void {
  commit([])
}

function selectProduct(productId: string, checked: boolean): void {
  void ensureInterfaces(productId).then(() => {
    const next = new Set(props.modelValue)
    for (const item of interfacesOf(productId)) {
      if (checked) next.add(item.id)
      else next.delete(item.id)
    }
    commit(Array.from(next))
  })
}

function toggleExpand(productId: string): void {
  const open = !expanded.value[productId]
  expanded.value = { ...expanded.value, [productId]: open }
  if (open) void ensureInterfaces(productId)
}

async function loadProducts(): Promise<void> {
  if (!canReadProducts.value) return
  loadingProducts.value = true
  errorMessage.value = ''
  try {
    const res = await listProducts({ page: 1, page_size: PAGE_LIMIT, status: 'enabled' })
    products.value = res.list
  } catch {
    errorMessage.value = '加载产品列表失败'
  } finally {
    loadingProducts.value = false
  }
}

async function ensureInterfaces(productId: string): Promise<void> {
  if (interfacesByProduct.value[productId] || loadingProductIds.value[productId]) return
  if (!canReadInterfaces.value) {
    errorMessage.value = '缺少「接口管理」读取权限，无法加载接口'
    return
  }
  loadingProductIds.value = { ...loadingProductIds.value, [productId]: true }
  try {
    const res = await listInterfaces(productId, { page: 1, page_size: PAGE_LIMIT })
    interfacesByProduct.value = { ...interfacesByProduct.value, [productId]: res.list }
  } catch {
    errorMessage.value = '加载接口列表失败'
  } finally {
    loadingProductIds.value = { ...loadingProductIds.value, [productId]: false }
  }
}

/** 解析已选接口名称（编辑回显时自动展开产品并加载接口）。 */
async function resolveSelected(): Promise<void> {
  if (resolving || products.value.length === 0) return
  const unresolved = (): boolean =>
    props.modelValue.some((id) => !interfaceIndex.value[id])
  if (!unresolved()) return

  resolving = true
  try {
    for (const productId of props.productIds ?? []) {
      if (!products.value.some((product) => product.id === productId)) continue
      expanded.value = { ...expanded.value, [productId]: true }
      await ensureInterfaces(productId)
    }
    // 仅有接口 ID（无 product_ids）时按顺序补加载，限制 10 个产品避免请求放大。
    for (const product of products.value.slice(0, 10)) {
      if (!unresolved()) break
      await ensureInterfaces(product.id)
    }
  } finally {
    resolving = false
  }
}

watch(
  [products, () => props.modelValue, () => props.productIds],
  () => {
    void resolveSelected()
  },
)

onMounted(() => {
  void loadProducts()
})
</script>

<style scoped>
.range-picker {
  display: flex;
  flex-direction: column;
  gap: 8px;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  padding: 10px 12px;
  background: #fcfcfd;
}

.range-head {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.range-summary {
  font-size: 13px;
  color: #374151;
}

.range-search {
  flex: 1;
  min-width: 180px;
  padding: 6px 9px;
  border: 1px solid #d1d5db;
  border-radius: 6px;
  font-size: 13px;
}

.product-list {
  max-height: 320px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.product-row {
  border-bottom: 1px dashed #e5e7eb;
  padding-bottom: 6px;
}

.product-head {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}

.product-toggle {
  font-size: 13px;
  font-weight: 600;
}

.product-body {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 6px 0 0 18px;
}

.interface-row {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: #374151;
  cursor: pointer;
}

.interface-name {
  min-width: 120px;
}

.interface-url {
  font-size: 12px;
  color: #6b7280;
  word-break: break-all;
}

.selected-chips {
  max-height: 96px;
  overflow-y: auto;
}
</style>
