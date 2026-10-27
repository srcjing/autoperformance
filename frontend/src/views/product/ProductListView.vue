<template>
  <div class="ap-page">
    <div class="ap-page-header">
      <h2 class="ap-page-title">产品列表</h2>
      <button v-if="canWrite" type="button" class="ap-btn ap-btn-primary" @click="openCreate">
        新建产品
      </button>
    </div>

    <form class="ap-card ap-toolbar" @submit.prevent="search">
      <label class="ap-field">
        <span class="ap-field-label">名称/编码</span>
        <input v-model="filters.keyword" placeholder="关键字检索" maxlength="64" />
      </label>
      <label class="ap-field">
        <span class="ap-field-label">版本</span>
        <input v-model="filters.productVersion" placeholder="如：v1.2.3" maxlength="32" />
      </label>
      <label class="ap-field">
        <span class="ap-field-label">平台</span>
        <input v-model="filters.platform" placeholder="如：Linux-x86" maxlength="64" />
      </label>
      <label class="ap-field">
        <span class="ap-field-label">状态</span>
        <select v-model="filters.status">
          <option value="">全部状态</option>
          <option value="enabled">启用</option>
          <option value="disabled">已停用</option>
        </select>
      </label>
      <div class="ap-actions">
        <button type="submit" class="ap-btn ap-btn-primary" :disabled="store.loading">查询</button>
        <button type="button" class="ap-btn ap-btn-ghost" @click="resetFilters">重置</button>
      </div>
    </form>

    <p v-if="errorMessage" class="ap-alert ap-alert-error" role="alert">{{ errorMessage }}</p>

    <ProductTable
      :products="store.products"
      :loading="store.loading"
      :can-write="canWrite"
      @edit="openEdit"
      @toggle="onToggleStatus"
      @remove="onRemove"
      @manage="openInterfaces"
    />

    <div class="ap-pagination">
      <span>共 {{ store.total }} 条，第 {{ store.page }} / {{ totalPages }} 页</span>
      <button
        type="button"
        class="ap-btn ap-btn-ghost ap-btn-sm"
        :disabled="store.page <= 1 || store.loading"
        @click="goPage(store.page - 1)"
      >
        上一页
      </button>
      <button
        type="button"
        class="ap-btn ap-btn-ghost ap-btn-sm"
        :disabled="store.page >= totalPages || store.loading"
        @click="goPage(store.page + 1)"
      >
        下一页
      </button>
    </div>

    <ProductFormDialog
      :open="dialogOpen"
      :product="editing"
      :saving="saving"
      :error-message="dialogError"
      @close="closeDialog"
      @submit="onSubmit"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ApiError } from '@/api/http'
import ProductFormDialog from '@/components/product/ProductFormDialog.vue'
import ProductTable from '@/components/product/ProductTable.vue'
import { useAuthStore } from '@/stores/auth'
import { useProductStore } from '@/stores/product'
import type { ProductInfo, ProductPayload } from '@/types'

// 产品列表页（PRD §3.2）：筛选（名称/编码、版本、平台、状态）+ CRUD + 停用/启用 + 跳转接口管理。
// 删除受引用保护约束：产品下存在接口时后端返回 22002，前端提示先处理接口（INV-6）。
const store = useProductStore()
const auth = useAuthStore()
const router = useRouter()

const canWrite = computed(() => auth.hasPerm('product.list', 'write'))
const filters = reactive({
  keyword: '',
  productVersion: '',
  platform: '',
  status: '',
})

const errorMessage = ref('')
const dialogOpen = ref(false)
const dialogError = ref('')
const editing = ref<ProductInfo | null>(null)
const saving = ref(false)

const totalPages = computed(() =>
  Math.max(1, Math.ceil(store.total / Math.max(store.pageSize, 1))),
)

/** 按当前筛选条件加载指定页（默认第 1 页）。 */
async function load(page = 1): Promise<void> {
  errorMessage.value = ''
  try {
    await store.fetchList({
      keyword: filters.keyword.trim(),
      product_version: filters.productVersion.trim() || undefined,
      platform: filters.platform.trim() || undefined,
      status: filters.status || undefined,
      page,
      page_size: 20,
    })
  } catch (err) {
    errorMessage.value = err instanceof ApiError ? err.message : '加载产品列表失败'
  }
}

function search(): void {
  void load(1)
}

function resetFilters(): void {
  filters.keyword = ''
  filters.productVersion = ''
  filters.platform = ''
  filters.status = ''
  void load(1)
}

function goPage(page: number): void {
  void load(page)
}

function openCreate(): void {
  editing.value = null
  dialogError.value = ''
  dialogOpen.value = true
}

function openEdit(product: ProductInfo): void {
  editing.value = product
  dialogError.value = ''
  dialogOpen.value = true
}

function closeDialog(): void {
  if (saving.value) return
  dialogOpen.value = false
  editing.value = null
  dialogError.value = ''
}

async function onSubmit(payload: ProductPayload): Promise<void> {
  if (saving.value) return
  saving.value = true
  dialogError.value = ''
  const wasEdit = editing.value !== null
  try {
    await store.saveProduct(editing.value?.id ?? null, payload)
    dialogOpen.value = false
    editing.value = null
    // 编辑保持当前页，新建回到第 1 页（列表按 ID 升序）。
    await load(wasEdit ? store.page : 1)
  } catch (err) {
    // 22001 编码重复 / 10402 乐观锁冲突。
    dialogError.value = err instanceof ApiError ? err.message : '保存产品失败'
  } finally {
    saving.value = false
  }
}

async function onToggleStatus(product: ProductInfo): Promise<void> {
  const target = product.status === 'disabled' ? 'enabled' : 'disabled'
  const action = target === 'enabled' ? '启用' : '停用'
  if (!window.confirm(`确认${action}产品「${product.name}」？`)) return

  errorMessage.value = ''
  try {
    await store.setStatus(product.id, target)
    await load(store.page)
  } catch (err) {
    // 10401 当前状态不允许该操作。
    errorMessage.value = err instanceof ApiError ? err.message : `${action}产品失败`
  }
}

async function onRemove(product: ProductInfo): Promise<void> {
  if (
    !window.confirm(
      `确认删除产品「${product.name}」？删除为逻辑删除；产品下存在接口时需先删除接口。`,
    )
  ) {
    return
  }

  errorMessage.value = ''
  try {
    await store.removeProduct(product.id)
    await load(store.page)
  } catch (err) {
    // 22002 产品下存在接口。
    errorMessage.value = err instanceof ApiError ? err.message : '删除产品失败'
  }
}

function openInterfaces(product: ProductInfo): void {
  void router.push({ name: 'interface-list', params: { productId: product.id } })
}

onMounted(() => {
  void load(1)
})
</script>
