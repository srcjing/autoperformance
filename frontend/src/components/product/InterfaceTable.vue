<template>
  <div class="ap-card interface-table">
    <table class="ap-table">
      <thead>
        <tr>
          <th>接口名称</th>
          <th>方法</th>
          <th>接口地址</th>
          <th>认证头</th>
          <th>状态</th>
          <th v-if="canWrite">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="item in interfaces" :key="item.id">
          <td>{{ item.name }}</td>
          <td><span class="ap-badge">{{ item.method }}</span></td>
          <td class="url">{{ formatInterfaceAddress(item) }}</td>
          <td>
            <div v-if="item.auth_headers.length > 0" class="ap-chips">
              <span v-for="header in item.auth_headers" :key="header" class="ap-chip ap-chip-static">
                {{ header }}（已加密）
              </span>
            </div>
            <span v-else class="ap-muted">无</span>
          </td>
          <td>
            <span class="ap-badge" :class="statusClass(item.status)">{{ statusLabel(item.status) }}</span>
          </td>
          <td v-if="canWrite">
            <div class="ap-actions">
              <button type="button" class="ap-btn ap-btn-ghost ap-btn-sm" @click="emit('thresholds', item)">
                阈值配置
              </button>
              <button type="button" class="ap-btn ap-btn-ghost ap-btn-sm" @click="emit('edit', item)">
                编辑
              </button>
              <button type="button" class="ap-btn ap-btn-ghost ap-btn-sm" @click="emit('toggle', item)">
                {{ item.status === 'disabled' ? '启用' : '停用' }}
              </button>
              <button type="button" class="ap-btn ap-btn-danger ap-btn-sm" @click="emit('remove', item)">
                删除
              </button>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-if="!loading && interfaces.length === 0" class="ap-table-empty">暂无接口，点击「新建接口」开始录入</p>
    <p v-if="loading" class="ap-table-empty">加载中…</p>
  </div>
</template>

<script setup lang="ts">
import type { InterfaceInfo, InterfaceStatus } from '@/types'
import { formatInterfaceAddress } from '@/utils/format'

// 接口表格。认证头只展示名称并标注「已加密」，不展示任何明文值（DES-I-001）。
defineProps<{
  interfaces: InterfaceInfo[]
  loading: boolean
  canWrite: boolean
}>()

const emit = defineEmits<{
  (e: 'edit', item: InterfaceInfo): void
  (e: 'toggle', item: InterfaceInfo): void
  (e: 'remove', item: InterfaceInfo): void
  (e: 'thresholds', item: InterfaceInfo): void
}>()

function statusLabel(status: InterfaceStatus): string {
  return status === 'disabled' ? '已停用' : '启用'
}

function statusClass(status: InterfaceStatus): string {
  return status === 'disabled' ? 'ap-badge-disabled' : 'ap-badge-pending'
}
</script>

<style scoped>
.url {
  max-width: 320px;
  font-size: 12px;
  color: #4b5563;
  word-break: break-all;
}
</style>
