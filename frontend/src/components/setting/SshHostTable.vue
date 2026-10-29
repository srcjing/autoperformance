<template>
  <div class="ap-card">
    <table class="ap-table">
      <thead>
        <tr>
          <th>类型</th>
          <th>主机</th>
          <th>端口</th>
          <th>SSH 用户</th>
          <th>认证方式</th>
          <th>Host 解析内容</th>
          <th>说明</th>
          <th>操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="item in hosts" :key="item.id">
          <td>
            <span class="ap-badge" :class="item.type === 'ssh' ? 'ap-badge-info' : 'ap-badge-muted'">
              {{ sshTypeLabel(item.type) }}
            </span>
          </td>
          <td class="host-cell">{{ item.host }}</td>
          <td>{{ item.type === 'ssh' ? item.port || 22 : '-' }}</td>
          <td>{{ item.type === 'ssh' ? item.user || '-' : '-' }}</td>
          <td>{{ item.type === 'ssh' ? sshAuthLabel(item.auth_type, item.has_key) : '-' }}</td>
          <td>
            <span v-if="item.host_entries.length === 0" class="ap-muted">-</span>
            <span v-else class="ap-chips">
              <span v-for="entry in item.host_entries" :key="entry" class="ap-chip ap-chip-static">
                {{ entry }}
              </span>
            </span>
          </td>
          <td>{{ item.description || '-' }}</td>
          <td>
            <div class="ap-actions">
              <template v-if="canWrite">
                <button type="button" class="ap-btn ap-btn-ghost ap-btn-sm" @click="emit('edit', item)">
                  编辑
                </button>
                <button type="button" class="ap-btn ap-btn-danger ap-btn-sm" @click="emit('remove', item)">
                  删除
                </button>
              </template>
              <span v-else class="ap-muted">只读</span>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-if="!loading && hosts.length === 0" class="ap-table-empty">
      暂无 SSH/Host 记录，点击「添加」或「批量导入」开始维护
    </p>
    <p v-if="loading" class="ap-table-empty">加载中…</p>
  </div>
</template>

<script setup lang="ts">
import type { SshHostInfo } from '@/types'
import { sshAuthLabel, sshTypeLabel } from '@/utils/setting'

// SSH/Host 表格（私钥只展示「已加密保存」状态，不回显任何密钥材料）。
defineProps<{
  hosts: SshHostInfo[]
  loading: boolean
  canWrite: boolean
}>()

const emit = defineEmits<{
  (e: 'edit', host: SshHostInfo): void
  (e: 'remove', host: SshHostInfo): void
}>()
</script>

<style scoped>
.host-cell {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
}
</style>
