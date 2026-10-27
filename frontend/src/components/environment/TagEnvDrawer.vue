<template>
  <div v-if="open" class="ap-drawer-mask" @click.self="emit('close')">
    <aside class="ap-drawer" role="dialog" aria-modal="true">
      <div class="ap-dialog-header">
        <span>标签「{{ tag?.name }}」关联环境（{{ total }}）</span>
        <button type="button" class="ap-link" @click="emit('close')">关闭</button>
      </div>

      <div class="ap-drawer-body">
        <table class="ap-table">
          <thead>
            <tr>
              <th>环境名称</th>
              <th>编码</th>
              <th>分类</th>
              <th>状态</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="env in environments" :key="env.id">
              <td>{{ env.name }}</td>
              <td>{{ env.code }}</td>
              <td>{{ env.category_name || '-' }}</td>
              <td>
                <span class="ap-badge" :class="`ap-badge-${env.status}`">{{ statusLabel(env.status) }}</span>
              </td>
            </tr>
          </tbody>
        </table>
        <p v-if="!loading && environments.length === 0" class="ap-table-empty">该标签暂未关联环境</p>
        <p v-if="loading" class="ap-table-empty">加载中…</p>

        <div class="ap-pagination drawer-pagination">
          <span>第 {{ page }} / {{ totalPages }} 页</span>
          <button
            type="button"
            class="ap-btn ap-btn-ghost ap-btn-sm"
            :disabled="page <= 1 || loading"
            @click="emit('page-change', page - 1)"
          >
            上一页
          </button>
          <button
            type="button"
            class="ap-btn ap-btn-ghost ap-btn-sm"
            :disabled="page >= totalPages || loading"
            @click="emit('page-change', page + 1)"
          >
            下一页
          </button>
        </div>
      </div>
    </aside>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { EnvironmentInfo, EnvironmentStatus, TagInfo } from '@/types'

// 标签 → 关联环境反查抽屉（GET /tags/{id}/environments）。
const props = defineProps<{
  open: boolean
  tag: TagInfo | null
  environments: EnvironmentInfo[]
  total: number
  page: number
  pageSize: number
  loading: boolean
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'page-change', page: number): void
}>()

const totalPages = computed(() => Math.max(1, Math.ceil(props.total / Math.max(props.pageSize, 1))))

function statusLabel(status: EnvironmentStatus): string {
  if (status === 'testing') return '测试中'
  if (status === 'disabled') return '已下线'
  return '待测试'
}
</script>

<style scoped>
.drawer-pagination {
  margin-top: 16px;
}
</style>
