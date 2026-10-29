<template>
  <div class="ap-page">
    <div class="ap-page-header">
      <div class="title-block">
        <h2 class="ap-page-title">
          接口管理
          <span v-if="headerProduct" class="ap-muted product-hint">
            · {{ headerProduct.name }}（{{ headerProduct.code }} / {{ headerProduct.product_version }}）
          </span>
        </h2>
        <RouterLink to="/products/list" class="ap-link">返回产品列表</RouterLink>
      </div>
      <button
        v-if="canWrite && productId && !productMissing"
        type="button"
        class="ap-btn ap-btn-primary"
        :disabled="!store.activeProduct"
        @click="openCreate"
      >
        新建接口
      </button>
    </div>

    <form class="ap-card ap-toolbar" @submit.prevent="search">
      <label class="ap-field product-field">
        <span class="ap-field-label">产品 *</span>
        <select
          :value="selectedProductId"
          :disabled="store.loading || store.interfaceLoading"
          @change="onProductChange"
        >
          <option value="">请选择产品</option>
          <option v-for="item in store.products" :key="item.id" :value="item.id">
            {{ item.name }}（{{ item.code }}）
          </option>
        </select>
      </label>
      <label class="ap-field">
        <span class="ap-field-label">接口名称/路径</span>
        <input v-model="filters.keyword" placeholder="关键字检索" maxlength="128" />
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
        <button
          type="submit"
          class="ap-btn ap-btn-primary"
          :disabled="!productId || store.interfaceLoading"
        >
          查询
        </button>
        <button type="button" class="ap-btn ap-btn-ghost" @click="resetFilters">重置</button>
      </div>
    </form>

    <p v-if="errorMessage" class="ap-alert ap-alert-error" role="alert">{{ errorMessage }}</p>

    <div v-if="!productId" class="ap-card">
      <p class="ap-alert ap-alert-hint">
        请在页面顶部「产品」下拉中选择要维护的产品，选中后加载该产品下的接口。
      </p>
    </div>

    <!-- 产品不存在（10301）时给出提示，避免导航里写死产品 ID 造成死路 -->
    <div v-else-if="productMissing" class="ap-card">
      <p class="ap-alert ap-alert-error">
        未找到该产品（可能已被删除或无权访问），请通过顶部「产品」下拉切换其他产品。
      </p>
    </div>

    <InterfaceTable
      v-else
      :interfaces="store.interfaces"
      :loading="store.interfaceLoading"
      :can-write="canWrite"
      @edit="openEdit"
      @toggle="onToggleStatus"
      @remove="onRemove"
      @thresholds="openThresholds"
    />

    <div v-if="productId && !productMissing" class="ap-pagination">
      <span>共 {{ store.interfaceTotal }} 条，第 {{ store.interfacePage }} / {{ totalPages }} 页</span>
      <button
        type="button"
        class="ap-btn ap-btn-ghost ap-btn-sm"
        :disabled="store.interfacePage <= 1 || store.interfaceLoading"
        @click="goPage(store.interfacePage - 1)"
      >
        上一页
      </button>
      <button
        type="button"
        class="ap-btn ap-btn-ghost ap-btn-sm"
        :disabled="store.interfacePage >= totalPages || store.interfaceLoading"
        @click="goPage(store.interfacePage + 1)"
      >
        下一页
      </button>
    </div>

    <InterFormDialog
      :open="dialogOpen"
      :item="editing"
      :saving="saving"
      :error-message="dialogError"
      @close="closeDialog"
      @submit="onSubmit"
    />

    <ThresholdEditor
      :open="thresholdOpen"
      :item="thresholdItem"
      :thresholds="thresholdList"
      :loading="thresholdLoading"
      :saving="thresholdSaving"
      :error-message="thresholdError"
      @close="closeThresholds"
      @submit="onSaveThresholds"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { ApiError } from '@/api/http'
import InterFormDialog from '@/components/product/InterFormDialog.vue'
import InterfaceTable from '@/components/product/InterfaceTable.vue'
import ThresholdEditor from '@/components/product/ThresholdEditor.vue'
import { useAuthStore } from '@/stores/auth'
import { useProductStore } from '@/stores/product'
import type {
  InterfaceInfo,
  InterfacePayload,
  InterfaceStatus,
  ThresholdInfo,
  ThresholdPayload,
} from '@/types'

// 接口管理页（PRD §3.2 接口管理）：产品内接口 CRUD + 阈值配置（InterTable + InterFormDialog + ThresholdEditor）。
// 认证头一律脱敏展示（仅名称），保存时由后端加密存储（DES-I-001）。
const store = useProductStore()
const auth = useAuthStore()
const route = useRoute()
const router = useRouter()

const canWrite = computed(() => auth.hasPerm('product.interface', 'write'))
const productId = computed(() => String(route.params.productId ?? ''))
const productMissing = ref(false)
/** 顶部产品下拉的当前值（路由参数的产品；空串表示尚未选择产品）。 */
const selectedProductId = ref(productId.value)

const filters = reactive({ keyword: '', status: '' })
const errorMessage = ref('')

const dialogOpen = ref(false)
const dialogError = ref('')
const editing = ref<InterfaceInfo | null>(null)
const saving = ref(false)

const thresholdOpen = ref(false)
const thresholdItem = ref<InterfaceInfo | null>(null)
const thresholdList = ref<ThresholdInfo[]>([])
const thresholdLoading = ref(false)
const thresholdSaving = ref(false)
const thresholdError = ref('')

/** 页面标题中的当前产品提示（未选产品/产品不存在时不展示）。 */
const headerProduct = computed(() =>
  productId.value && !productMissing.value ? store.activeProduct : null,
)

const totalPages = computed(() =>
  Math.max(1, Math.ceil(store.interfaceTotal / Math.max(store.interfacePageSize, 1))),
)

/** 加载接口列表（并同步产品详情；产品不存在时给出切换入口）。 */
async function load(page = 1): Promise<void> {
  errorMessage.value = ''
  productMissing.value = false
  if (!productId.value) {
    // 未选产品：只提示选择产品（需求 2），不写死 productId。
    store.clearInterfaces()
    return
  }

  try {
    await store.fetchInterfaces(productId.value, {
      keyword: filters.keyword.trim() || undefined,
      status: filters.status || undefined,
      page,
      page_size: 20,
    })
  } catch (err) {
    // 10301 产品不存在（或已被删除）。
    if (err instanceof ApiError && err.code === 10301) {
      productMissing.value = true
      store.clearInterfaces()
      return
    }
    errorMessage.value = err instanceof ApiError ? err.message : '加载接口列表失败'
    store.clearInterfaces()
    return
  }

  productMissing.value = store.activeProduct === null
}

/** 加载产品下拉选项（接口管理页始终展示产品切换）。 */
async function loadProducts(): Promise<void> {
  try {
    // 上限 200（api-contract §1.3），覆盖产品下拉选择场景。
    await store.fetchList({ page: 1, page_size: 200 })
  } catch {
    errorMessage.value = '加载产品列表失败，请稍后重试'
  }
}

function search(): void {
  void load(1)
}

function resetFilters(): void {
  filters.keyword = ''
  filters.status = ''
  void load(1)
}

function goPage(page: number): void {
  void load(page)
}

/** 切换产品：改写路由参数后由 productId watcher 触发加载（空值回到“未选择产品”）。 */
function onProductChange(event: Event): void {
  const value = (event.target as HTMLSelectElement).value
  selectedProductId.value = value
  if (value === productId.value) return // 重复选择同一产品：不触发冗余导航
  if (!value) {
    void router.push({ name: 'interface-list' })
    return
  }
  void router.push({ name: 'interface-list', params: { productId: value } })
}

function openCreate(): void {
  editing.value = null
  dialogError.value = ''
  dialogOpen.value = true
}

function openEdit(item: InterfaceInfo): void {
  editing.value = item
  dialogError.value = ''
  dialogOpen.value = true
}

function closeDialog(): void {
  if (saving.value) return
  dialogOpen.value = false
  editing.value = null
  dialogError.value = ''
}

async function onSubmit(payload: InterfacePayload): Promise<void> {
  if (saving.value) return
  saving.value = true
  dialogError.value = ''
  const wasEdit = editing.value !== null
  try {
    await store.saveInterface(productId.value, editing.value?.id ?? null, payload)
    dialogOpen.value = false
    editing.value = null
    await load(wasEdit ? store.interfacePage : 1)
  } catch (err) {
    // 10301 产品/接口不存在 / 10402 乐观锁冲突 / 10003 参数非法。
    dialogError.value = err instanceof ApiError ? err.message : '保存接口失败'
  } finally {
    saving.value = false
  }
}

async function onToggleStatus(item: InterfaceInfo): Promise<void> {
  const target: InterfaceStatus = item.status === 'disabled' ? 'enabled' : 'disabled'
  const action = target === 'enabled' ? '启用' : '停用'
  if (!window.confirm(`确认${action}接口「${item.name}」？`)) return

  errorMessage.value = ''
  try {
    await store.setInterfaceStatus(item.id, target)
    await load(store.interfacePage)
  } catch (err) {
    errorMessage.value = err instanceof ApiError ? err.message : `${action}接口失败`
  }
}

async function onRemove(item: InterfaceInfo): Promise<void> {
  if (!window.confirm(`确认删除接口「${item.name}」？删除为逻辑删除，历史任务快照不受影响。`)) return

  errorMessage.value = ''
  try {
    await store.removeInterface(item.id)
    await load(store.interfacePage)
  } catch (err) {
    errorMessage.value = err instanceof ApiError ? err.message : '删除接口失败'
  }
}

/** 打开阈值编辑器（先加载已保存阈值）。 */
async function openThresholds(item: InterfaceInfo): Promise<void> {
  thresholdItem.value = item
  thresholdError.value = ''
  thresholdList.value = []
  thresholdOpen.value = true
  thresholdLoading.value = true
  try {
    thresholdList.value = await store.fetchThresholds(item.id)
  } catch (err) {
    thresholdError.value = err instanceof ApiError ? err.message : '加载阈值失败'
  } finally {
    thresholdLoading.value = false
  }
}

function closeThresholds(): void {
  if (thresholdSaving.value) return
  thresholdOpen.value = false
  thresholdItem.value = null
  thresholdList.value = []
  thresholdError.value = ''
}

async function onSaveThresholds(rows: ThresholdPayload[]): Promise<void> {
  if (!thresholdItem.value || thresholdSaving.value) return
  thresholdSaving.value = true
  thresholdError.value = ''
  try {
    thresholdList.value = await store.saveThresholds(thresholdItem.value.id, rows)
    thresholdOpen.value = false
    thresholdItem.value = null
  } catch (err) {
    // 22003 指标与方向不匹配 / 10003 数值非法。
    thresholdError.value = err instanceof ApiError ? err.message : '保存阈值失败'
  } finally {
    thresholdSaving.value = false
  }
}

/** 路由产品 ID 变化（从产品列表点「接口管理」或顶部下拉切换）时重新加载。 */
watch(productId, (value) => {
  selectedProductId.value = value
  void load(1)
})

onMounted(() => {
  void loadProducts()
  void load(1)
})
</script>

<style scoped>
.title-block {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.product-hint {
  font-size: 13px;
  font-weight: normal;
}

.product-field select {
  min-width: 220px;
}
</style>
