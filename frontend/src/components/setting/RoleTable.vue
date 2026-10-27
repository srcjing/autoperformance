<template>
  <div class="ap-card">
    <table class="ap-table">
      <thead>
        <tr>
          <th>角色名</th>
          <th>权限范围</th>
          <th>描述</th>
          <th>关联管理员</th>
          <th>操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="role in roles" :key="role.id">
          <td class="role-name">{{ role.name }}</td>
          <td>
            <div v-if="scopeOf(role).write + scopeOf(role).read === 0" class="ap-muted">
              无权限项
            </div>
            <div v-else class="ap-chips" :title="scopeOf(role).detail">
              <span v-if="scopeOf(role).write > 0" class="ap-badge ap-badge-info">
                可写 {{ scopeOf(role).write }}
              </span>
              <span v-if="scopeOf(role).read > 0" class="ap-badge ap-badge-muted">
                只读 {{ scopeOf(role).read }}
              </span>
              <span v-if="scopeOf(role).sensitive > 0" class="ap-badge ap-badge-danger">
                敏感 {{ scopeOf(role).sensitive }}
              </span>
            </div>
          </td>
          <td class="desc-cell">{{ role.description || '-' }}</td>
          <td>
            <span v-if="countOf(role) === 0" class="ap-muted">0</span>
            <span v-else class="ap-badge ap-badge-pending">
              {{ countTruncated ? `≥${countOf(role)}` : countOf(role) }}
            </span>
          </td>
          <td>
            <div v-if="canWrite" class="ap-actions">
              <button type="button" class="ap-btn ap-btn-ghost ap-btn-sm" @click="emit('edit', role)">
                编辑
              </button>
              <button
                type="button"
                class="ap-btn ap-btn-danger ap-btn-sm"
                :disabled="countOf(role) > 0"
                :title="countOf(role) > 0 ? `已被 ${countOf(role)} 个管理员引用，请先解除授权` : ''"
                @click="emit('remove', role)"
              >
                删除
              </button>
            </div>
            <span v-else class="ap-muted">只读</span>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-if="!loading && roles.length === 0" class="ap-table-empty">
      暂无角色，点击「新建角色」创建权限范围集合
    </p>
    <p v-if="loading" class="ap-table-empty">加载中…</p>
    <p v-if="countTruncated" class="ap-muted table-note">
      管理员总数超过单页上限（200），「关联管理员」列展示的是下限值；删除前以服务端校验（26003）为准。
    </p>
  </div>
</template>

<script setup lang="ts">
import type { PermissionModule, RoleInfo } from '@/types'
import { summarizeRolePermissions } from '@/utils/setting'

// 角色表格：角色名 / 权限范围摘要 / 描述 / 关联管理员数。
// 关联管理员数由管理员列表聚合（后端角色 DTO 未暴露引用计数），仅作展示与删除前置提示。
const props = defineProps<{
  roles: RoleInfo[]
  loading: boolean
  canWrite: boolean
  modules: PermissionModule[]
  adminCounts: Record<string, number>
  countTruncated: boolean
}>()

const emit = defineEmits<{
  (e: 'edit', role: RoleInfo): void
  (e: 'remove', role: RoleInfo): void
}>()

function scopeOf(role: RoleInfo) {
  return summarizeRolePermissions(role.permissions, props.modules)
}

function countOf(role: RoleInfo): number {
  return props.adminCounts[role.id] ?? 0
}
</script>

<style scoped>
.role-name {
  font-weight: 600;
  color: #1f2933;
  white-space: nowrap;
}

.desc-cell {
  max-width: 320px;
  color: #4b5563;
}

.table-note {
  margin: 10px 0 0;
  font-size: 12px;
}
</style>
