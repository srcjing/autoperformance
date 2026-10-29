<template>
  <div class="ap-card">
    <table class="ap-table">
      <thead>
        <tr>
          <th>账号</th>
          <th>姓名</th>
          <th>手机号</th>
          <th>邮箱</th>
          <th>归属角色</th>
          <th>状态</th>
          <th>最近登录</th>
          <th>操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="item in admins" :key="item.id">
          <td>
            <div class="account-cell">
              <span class="account-name">{{ item.username }}</span>
              <span v-if="item.is_builtin" class="ap-badge ap-badge-info">内置</span>
            </div>
            <span v-if="item.must_change_password" class="ap-muted flag">待首次改密</span>
          </td>
          <td>{{ item.name || '-' }}</td>
          <td>{{ item.phone || '-' }}</td>
          <td>{{ item.email || '-' }}</td>
          <td>
            <span v-if="rolesOf(item).length === 0" class="ap-muted">
              {{ item.is_builtin ? '隐式全权限' : '未授权' }}
            </span>
            <span v-else class="ap-chips">
              <span
                v-for="role in rolesOf(item)"
                :key="role.id"
                class="ap-chip ap-chip-static"
                :title="role.description || role.name"
              >
                {{ role.name }}
              </span>
            </span>
          </td>
          <td>
            <span class="ap-badge" :class="adminStatusMeta(item.status).badge">
              {{ adminStatusMeta(item.status).label }}
            </span>
          </td>
          <td>{{ item.last_login_at ? formatDateTime(item.last_login_at) : '-' }}</td>
          <td>
            <div v-if="canWrite" class="ap-actions">
              <button type="button" class="ap-btn ap-btn-ghost ap-btn-sm" @click="emit('edit', item)">
                编辑
              </button>
              <button
                type="button"
                class="ap-btn ap-btn-ghost ap-btn-sm"
                @click="emit('bind-roles', item)"
              >
                角色授权
              </button>
              <button
                type="button"
                class="ap-btn ap-btn-ghost ap-btn-sm"
                @click="emit('reset-password', item)"
              >
                重置密码
              </button>
              <button
                type="button"
                class="ap-btn ap-btn-ghost ap-btn-sm"
                :disabled="item.is_builtin"
                :title="item.is_builtin ? BUILTIN_PROTECTED_HINT : ''"
                @click="emit('toggle-status', item)"
              >
                {{ item.status === 'disabled' ? '启用' : '停用' }}
              </button>
              <button
                type="button"
                class="ap-btn ap-btn-danger ap-btn-sm"
                :disabled="item.is_builtin"
                :title="item.is_builtin ? BUILTIN_PROTECTED_HINT : ''"
                @click="emit('remove', item)"
              >
                删除
              </button>
            </div>
            <span v-else class="ap-muted">只读</span>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-if="!loading && admins.length === 0" class="ap-table-empty">
      暂无管理员账号，点击「新建管理员」开始维护
    </p>
    <p v-if="loading" class="ap-table-empty">加载中…</p>
  </div>
</template>

<script setup lang="ts">
import type { AdminInfo, RoleInfo } from '@/types'
import { formatDateTime } from '@/utils/format'
import { adminStatusMeta } from '@/utils/setting'

// 管理员表格（PRD §3.6 / api-contract §2.7）。
// 内置 admin 为系统保护账号：不可删除/停用（服务端返回 26002），此处按钮禁用并提示。
defineProps<{
  admins: AdminInfo[]
  loading: boolean
  /** setting.admin:write 才展示写操作入口。 */
  canWrite: boolean
}>()

const emit = defineEmits<{
  (e: 'edit', admin: AdminInfo): void
  (e: 'bind-roles', admin: AdminInfo): void
  (e: 'reset-password', admin: AdminInfo): void
  (e: 'toggle-status', admin: AdminInfo): void
  (e: 'remove', admin: AdminInfo): void
}>()

const BUILTIN_PROTECTED_HINT = '内置 admin 不可删除/停用'

/** 归属角色（后端可能返回空数组，兜底避免模板空值判断）。 */
function rolesOf(admin: AdminInfo): RoleInfo[] {
  return admin.roles ?? []
}
</script>

<style scoped>
.account-cell {
  display: flex;
  align-items: center;
  gap: 6px;
}

.account-name {
  font-weight: 600;
  color: #1f2933;
}

.flag {
  font-size: 12px;
}
</style>
