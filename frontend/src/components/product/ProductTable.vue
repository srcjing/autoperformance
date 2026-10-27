<template>
  <div class="ap-card product-table">
    <table class="ap-table">
      <thead>
        <tr>
          <th>产品名称</th>
          <th>产品编码</th>
          <th>版本</th>
          <th>平台</th>
          <th>接口数</th>
          <th>状态</th>
          <th v-if="canWrite">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="product in products" :key="product.id">
          <td>{{ product.name }}</td>
          <td>{{ product.code }}</td>
          <td>{{ product.product_version }}</td>
          <td>{{ product.platform || '-' }}</td>
          <td>{{ product.interface_count ?? 0 }}</td>
          <td>
            <span class="ap-badge" :class="statusClass(product.status)">{{ statusLabel(product.status) }}</span>
          </td>
          <td v-if="canWrite">
            <div class="ap-actions">
              <button type="button" class="ap-btn ap-btn-ghost ap-btn-sm" @click="emit('manage', product)">
                接口管理
              </button>
              <button type="button" class="ap-btn ap-btn-ghost ap-btn-sm" @click="emit('edit', product)">
                编辑
              </button>
              <button type="button" class="ap-btn ap-btn-ghost ap-btn-sm" @click="emit('toggle', product)">
                {{ product.status === 'disabled' ? '启用' : '停用' }}
              </button>
              <button type="button" class="ap-btn ap-btn-danger ap-btn-sm" @click="emit('remove', product)">
                删除
              </button>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-if="!loading && products.length === 0" class="ap-table-empty">暂无产品，点击「新建产品」开始录入</p>
    <p v-if="loading" class="ap-table-empty">加载中…</p>
  </div>
</template>

<script setup lang="ts">
import type { ProductInfo, ProductStatus } from '@/types'

// 产品表格（展示用，操作通过事件上抛，便于列表页统一处理提示与刷新）。
defineProps<{
  products: ProductInfo[]
  loading: boolean
  canWrite: boolean
}>()

const emit = defineEmits<{
  (e: 'edit', product: ProductInfo): void
  (e: 'toggle', product: ProductInfo): void
  (e: 'remove', product: ProductInfo): void
  (e: 'manage', product: ProductInfo): void
}>()

function statusLabel(status: ProductStatus): string {
  return status === 'disabled' ? '已停用' : '启用'
}

function statusClass(status: ProductStatus): string {
  return status === 'disabled' ? 'ap-badge-disabled' : 'ap-badge-pending'
}
</script>
