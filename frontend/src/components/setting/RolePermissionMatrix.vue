<template>
  <div class="matrix">
    <p class="ap-muted matrix-hint">
      权限点格式 <code>module_code:read|write</code>；普通子模块「可写」已包含只读能力，
      日志子模块仅可只读，敏感子模块（管理员/证书/SSH）仅可授予可写。
    </p>

    <div v-for="group in groups" :key="group.name" class="matrix-group">
      <div class="matrix-group-title">{{ group.name }}</div>
      <table class="ap-table matrix-table">
        <thead>
          <tr>
            <th>子模块</th>
            <th class="perm-col">只读</th>
            <th class="perm-col">可写</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="module in group.modules" :key="module.module_code">
            <td>
              <div class="module-cell">
                <span>{{ shortName(module.name) }}</span>
                <span v-if="module.sensitive" class="ap-badge ap-badge-danger">敏感</span>
                <code class="module-code">{{ module.module_code }}</code>
              </div>
            </td>
            <td class="perm-col">
              <input
                type="checkbox"
                :checked="has(module, 'read')"
                :disabled="disabled || !allows(module, 'read')"
                :aria-label="`${shortName(module.name)} 只读`"
                @change="toggle(module, 'read')"
              />
            </td>
            <td class="perm-col">
              <input
                type="checkbox"
                :checked="has(module, 'write')"
                :disabled="disabled || !allows(module, 'write')"
                :aria-label="`${shortName(module.name)} 可写`"
                @change="toggle(module, 'write')"
              />
            </td>
          </tr>
        </tbody>
      </table>
      <p v-if="group.name === '日志管理'" class="ap-muted group-note">
        日志子模块默认只读，不提供写操作接口。
      </p>
      <p v-if="group.name === '系统设置'" class="ap-muted group-note">
        系统设置子模块为敏感权限：只读项不可选，仅能授予「可写」。
      </p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import type { PermissionModule } from '@/types'

// 角色权限矩阵（GET /permissions 元数据驱动；13 个子模块 × 只读/可写）。
// 可授权限与敏感标记完全以服务端元数据为准，不在前端硬编码：
//   - 日志子模块 permissions=['read'] → 可写项禁用；
//   - 系统设置敏感子模块 permissions=['write'] → 只读项禁用。
const props = defineProps<{
  modules: PermissionModule[]
  /** 已选授权项，键为 `module:permission`。 */
  modelValue: string[]
  disabled?: boolean
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: string[]): void
}>()

interface ModuleGroup {
  name: string
  modules: PermissionModule[]
}

/** 按模块名「大写分组 · 子模块」前缀分组，保持服务端返回顺序。 */
const groups = computed<ModuleGroup[]>(() => {
  const out: ModuleGroup[] = []
  for (const module of props.modules) {
    const groupName = module.name.includes('·')
      ? (module.name.split('·')[0] ?? '').trim()
      : '其他'
    let group = out.find((item) => item.name === groupName)
    if (!group) {
      group = { name: groupName, modules: [] }
      out.push(group)
    }
    group.modules.push(module)
  }
  return out
})

/** 子模块短名（去掉「分组 · 」前缀）。 */
function shortName(name: string): string {
  return name.includes('·') ? (name.split('·').pop() ?? name).trim() : name
}

function allows(module: PermissionModule, permission: 'read' | 'write'): boolean {
  return module.permissions.includes(permission)
}

function has(module: PermissionModule, permission: 'read' | 'write'): boolean {
  return props.modelValue.includes(`${module.module_code}:${permission}`)
}

function toggle(module: PermissionModule, permission: 'read' | 'write'): void {
  if (props.disabled || !allows(module, permission)) return
  const key = `${module.module_code}:${permission}`
  emit(
    'update:modelValue',
    props.modelValue.includes(key)
      ? props.modelValue.filter((item) => item !== key)
      : [...props.modelValue, key],
  )
}
</script>

<style scoped>
.matrix {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.matrix-hint {
  margin: 0;
  font-size: 12px;
  line-height: 1.6;
}

.matrix-group {
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  overflow: hidden;
}

.matrix-group-title {
  padding: 8px 12px;
  background: #f9fafb;
  border-bottom: 1px solid #e5e7eb;
  font-size: 13px;
  font-weight: 600;
  color: #374151;
}

.matrix-table th,
.matrix-table td {
  padding: 7px 12px;
}

.matrix-table tbody tr:last-child td {
  border-bottom: none;
}

.perm-col {
  width: 76px;
  text-align: center;
}

.perm-col input {
  width: 15px;
  height: 15px;
  cursor: pointer;
}

.perm-col input:disabled {
  cursor: not-allowed;
}

.module-cell {
  display: flex;
  align-items: center;
  gap: 8px;
}

.module-code {
  font-size: 11px;
  color: #9ca3af;
}

.group-note {
  margin: 0;
  padding: 6px 12px 10px;
  font-size: 12px;
}
</style>
