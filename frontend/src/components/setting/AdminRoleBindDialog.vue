<template>
  <div v-if="open" class="ap-dialog-mask" @click.self="onMaskClick">
    <div class="ap-dialog bind-dialog" role="dialog" aria-modal="true">
      <div class="ap-dialog-header">
        <span>角色授权 · {{ admin?.username ?? '' }}</span>
        <button type="button" class="ap-link" :disabled="saving" @click="emit('close')">关闭</button>
      </div>

      <div class="ap-dialog-body">
        <p class="ap-alert ap-alert-hint">
          全量替换：勾选后的结果即该管理员的角色集合，取消勾选会移除对应权限；保存后权限即时生效
          （无需重新登录）。
        </p>
        <p v-if="admin?.is_builtin" class="ap-alert ap-alert-hint">
          内置 admin 隐式拥有全部权限，角色绑定仅作记录，不影响其实际权限范围。
        </p>

        <p v-if="roles.length === 0" class="ap-muted">
          暂无可分配角色，请先在「角色管理」中新建角色。
        </p>
        <ul v-else class="role-list">
          <li v-for="role in roles" :key="role.id" class="role-item">
            <label class="role-label">
              <input
                type="checkbox"
                :checked="selected.includes(role.id)"
                :disabled="saving"
                @change="toggle(role.id)"
              />
              <span class="role-info">
                <span class="role-name">{{ role.name }}</span>
                <span class="ap-muted role-desc">{{ role.description || '无描述' }}</span>
              </span>
            </label>
            <span class="ap-muted role-scope" :title="scopeOf(role).detail">
              {{ scopeText(role) }}
            </span>
          </li>
        </ul>

        <p v-if="errorMessage" class="ap-alert ap-alert-error" role="alert">{{ errorMessage }}</p>
        <p v-else class="ap-muted">已选 {{ selected.length }} 个角色</p>

        <div class="ap-dialog-footer">
          <button type="button" class="ap-btn ap-btn-ghost" :disabled="saving" @click="emit('close')">
            取消
          </button>
          <button type="button" class="ap-btn ap-btn-primary" :disabled="saving" @click="onSubmit">
            {{ saving ? '保存中…' : '保存授权' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import type { AdminInfo, PermissionModule, RoleInfo } from '@/types'
import { summarizeRolePermissions } from '@/utils/setting'

// 管理员 ↔ 角色 多选授权（POST /admins/{id}/roles，全量替换）。
const props = defineProps<{
  open: boolean
  admin: AdminInfo | null
  roles: RoleInfo[]
  /** 该管理员当前已绑定的角色 ID（打开时由页面查询 GET /admins/{id}/roles 回填）。 */
  checkedIds: string[]
  modules: PermissionModule[]
  saving: boolean
  errorMessage?: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'submit', roleIds: string[]): void
}>()

const selected = ref<string[]>([])

watch(
  () => [props.open, props.checkedIds] as const,
  () => {
    selected.value = [...props.checkedIds]
  },
  { immediate: true, deep: true },
)

function toggle(roleId: string): void {
  selected.value = selected.value.includes(roleId)
    ? selected.value.filter((id) => id !== roleId)
    : [...selected.value, roleId]
}

function scopeOf(role: RoleInfo) {
  return summarizeRolePermissions(role.permissions, props.modules)
}

function scopeText(role: RoleInfo): string {
  const scope = scopeOf(role)
  if (scope.write === 0 && scope.read === 0) return '无权限项'
  return `可写 ${scope.write} · 只读 ${scope.read}`
}

function onMaskClick(): void {
  if (!props.saving) emit('close')
}

function onSubmit(): void {
  // 按角色列表顺序回传，保证请求可读且与服务端顺序无关。
  emit(
    'submit',
    props.roles.filter((role) => selected.value.includes(role.id)).map((role) => role.id),
  )
}
</script>

<style scoped>
.bind-dialog {
  width: 640px;
}

.role-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin: 0;
  padding: 0;
  list-style: none;
  max-height: 320px;
  overflow-y: auto;
}

.role-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 8px 10px;
  border: 1px solid #e5e7eb;
  border-radius: 6px;
}

.role-label {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  cursor: pointer;
  font-size: 13px;
}

.role-info {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.role-name {
  font-weight: 600;
  color: #1f2933;
}

.role-desc {
  font-size: 12px;
}

.role-scope {
  font-size: 12px;
  white-space: nowrap;
}
</style>
