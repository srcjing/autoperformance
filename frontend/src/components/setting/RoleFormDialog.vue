<template>
  <div v-if="open" class="ap-dialog-mask" @click.self="onMaskClick">
    <div class="ap-dialog role-form" role="dialog" aria-modal="true">
      <div class="ap-dialog-header">
        <span>{{ isEdit ? '编辑角色' : '新建角色' }}</span>
        <button type="button" class="ap-link" :disabled="saving" @click="emit('close')">关闭</button>
      </div>

      <form class="ap-dialog-body" @submit.prevent="onSubmit">
        <div class="ap-form-row">
          <label class="ap-field">
            <span class="ap-field-label">角色名 *</span>
            <input v-model="name" :disabled="saving" maxlength="64" placeholder="如：只读观察员" />
          </label>
          <label class="ap-field">
            <span class="ap-field-label">描述</span>
            <input v-model="description" :disabled="saving" maxlength="500" placeholder="角色用途说明" />
          </label>
        </div>

        <div class="scope-bar">
          <span class="scope-title">权限范围</span>
          <span class="ap-muted">
            可写 {{ summary.write }} 项 · 只读 {{ summary.read }} 项
            <template v-if="summary.sensitive > 0">· 敏感 {{ summary.sensitive }} 项</template>
          </span>
        </div>

        <p v-if="modules.length === 0" class="ap-alert ap-alert-hint">
          权限点元数据尚未加载，无法编辑权限矩阵，请关闭弹窗后重试。
        </p>
        <RolePermissionMatrix
          v-else
          v-model="grantKeys"
          :modules="modules"
          :disabled="saving"
        />

        <p v-if="isEdit" class="ap-alert ap-alert-hint">
          保存后权限矩阵全量替换，绑定该角色的管理员权限即时生效。
        </p>

        <p v-if="displayError" class="ap-alert ap-alert-error" role="alert">{{ displayError }}</p>

        <div class="ap-dialog-footer">
          <button type="button" class="ap-btn ap-btn-ghost" :disabled="saving" @click="emit('close')">
            取消
          </button>
          <button
            type="submit"
            class="ap-btn ap-btn-primary"
            :disabled="saving || modules.length === 0"
          >
            {{ saving ? '保存中…' : '保存' }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import RolePermissionMatrix from '@/components/setting/RolePermissionMatrix.vue'
import type { PermissionItem, PermissionModule, RoleInfo, RolePayload } from '@/types'
import { summarizeRolePermissions } from '@/utils/setting'

// 角色新建/编辑（含权限矩阵全量替换）。角色名唯一冲突由服务端返回 10302。
const props = defineProps<{
  open: boolean
  item: RoleInfo | null
  /** GET /permissions 返回的子模块元数据（权限矩阵事实源）。 */
  modules: PermissionModule[]
  saving: boolean
  errorMessage?: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'create', payload: RolePayload): void
  (e: 'update', payload: RolePayload): void
}>()

const name = ref('')
const description = ref('')
const grantKeys = ref<string[]>([])
const formError = ref('')

const isEdit = computed(() => props.item !== null)
const displayError = computed(() => props.errorMessage || formError.value)

const summary = computed(() =>
  summarizeRolePermissions(grantsFromKeys(grantKeys.value, props.modules), props.modules),
)

watch(
  () => props.open,
  (open) => {
    if (open) reset()
  },
  { immediate: true },
)

function reset(): void {
  name.value = props.item?.name ?? ''
  description.value = props.item?.description ?? ''
  grantKeys.value = (props.item?.permissions ?? []).map((p) => `${p.module}:${p.permission}`)
  formError.value = ''
}

/** `module:permission` 键集合 → 授权项（按元数据顺序归一，避免乱序/重复）。 */
function grantsFromKeys(keys: string[], modules: PermissionModule[]): PermissionItem[] {
  const out: PermissionItem[] = []
  for (const module of modules) {
    for (const permission of ['read', 'write'] as const) {
      if (keys.includes(`${module.module_code}:${permission}`)) {
        out.push({ module: module.module_code, permission })
      }
    }
  }
  return out
}

function onMaskClick(): void {
  if (!props.saving) emit('close')
}

function onSubmit(): void {
  const trimmedName = name.value.trim()
  if (!trimmedName) {
    formError.value = '请填写角色名'
    return
  }
  if (trimmedName.length > 64) {
    formError.value = '角色名不得超过 64 个字符'
    return
  }

  const payload: RolePayload = {
    name: trimmedName,
    description: description.value.trim(),
    permissions: grantsFromKeys(grantKeys.value, props.modules),
  }

  formError.value = ''
  if (isEdit.value) {
    emit('update', { ...payload, version: props.item?.version })
  } else {
    emit('create', payload)
  }
}
</script>

<style scoped>
.role-form {
  width: 820px;
}

.scope-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  font-size: 13px;
}

.scope-title {
  font-weight: 600;
  color: #374151;
}
</style>
