<template>
  <div class="ap-card env-table">
    <table class="ap-table">
      <thead>
        <tr>
          <th>环境名称</th>
          <th>编码</th>
          <th>分类</th>
          <th>标签</th>
          <th>类型</th>
          <th>主机地址</th>
          <th>操作系统</th>
          <th>状态</th>
          <th v-if="canWrite">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="env in environments" :key="env.id">
          <td>{{ env.name }}</td>
          <td>{{ env.code }}</td>
          <td>{{ env.category_name || '-' }}</td>
          <td>
            <div v-if="env.tags.length > 0" class="ap-chips">
              <span
                v-for="tag in env.tags"
                :key="tag.id"
                class="ap-chip ap-chip-static"
                :style="tag.color ? { borderColor: tag.color, color: tag.color } : undefined"
              >
                {{ tag.name }}
              </span>
            </div>
            <span v-else class="ap-muted">-</span>
          </td>
          <td>{{ env.type === 'cluster' ? '集群' : '单机' }}</td>
          <td class="hosts">
            <template v-if="env.hosts && env.hosts.length > 0">
              <div v-for="host in env.hosts" :key="host">{{ host }}</div>
            </template>
            <span v-else class="ap-muted">无权限查看</span>
          </td>
          <td>{{ env.os || '-' }}</td>
          <td>
            <span class="ap-badge" :class="statusClass(env.status)">{{ statusLabel(env.status) }}</span>
          </td>
          <td v-if="canWrite">
            <div class="ap-actions">
              <button type="button" class="ap-btn ap-btn-ghost ap-btn-sm" @click="emit('edit', env)">
                编辑
              </button>
              <button
                v-if="env.status !== 'testing'"
                type="button"
                class="ap-btn ap-btn-ghost ap-btn-sm"
                @click="emit('toggle', env)"
              >
                {{ env.status === 'disabled' ? '启用' : '停用' }}
              </button>
              <button
                v-if="env.status !== 'testing'"
                type="button"
                class="ap-btn ap-btn-danger ap-btn-sm"
                @click="emit('remove', env)"
              >
                删除
              </button>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-if="!loading && environments.length === 0" class="ap-table-empty">暂无环境，点击「新建环境」开始录入</p>
    <p v-if="loading" class="ap-table-empty">加载中…</p>
  </div>
</template>

<script setup lang="ts">
import type { EnvironmentInfo, EnvironmentStatus } from '@/types'

// 环境表格（展示用，操作通过事件上抛，便于列表页统一处理提示与刷新）。
defineProps<{
  environments: EnvironmentInfo[]
  loading: boolean
  canWrite: boolean
}>()

const emit = defineEmits<{
  (e: 'edit', env: EnvironmentInfo): void
  (e: 'toggle', env: EnvironmentInfo): void
  (e: 'remove', env: EnvironmentInfo): void
}>()

function statusLabel(status: EnvironmentStatus): string {
  if (status === 'testing') return '测试中'
  if (status === 'disabled') return '已下线'
  return '待测试'
}

function statusClass(status: EnvironmentStatus): string {
  return `ap-badge-${status}`
}
</script>

<style scoped>
.hosts {
  max-width: 200px;
  font-size: 12px;
  color: #4b5563;
  word-break: break-all;
}
</style>
