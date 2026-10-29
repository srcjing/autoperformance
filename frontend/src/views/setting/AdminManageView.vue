<template>
  <div class="ap-page">
    <div class="ap-page-header">
      <h2 class="ap-page-title">管理员管理</h2>
      <div v-if="canWrite" class="ap-actions">
        <button
          v-if="activeTab === 'admins'"
          type="button"
          class="ap-btn ap-btn-primary"
          @click="openCreateAdmin"
        >
          新建管理员
        </button>
        <button v-else type="button" class="ap-btn ap-btn-primary" @click="openCreateRole">
          新建角色
        </button>
      </div>
    </div>

    <div class="ap-card tab-bar">
      <button
        type="button"
        class="tab"
        :class="{ active: activeTab === 'admins' }"
        @click="switchTab('admins')"
      >
        管理员
      </button>
      <button
        type="button"
        class="tab"
        :class="{ active: activeTab === 'roles' }"
        @click="switchTab('roles')"
      >
        角色管理
      </button>
      <span class="ap-muted tab-hint">
        权限点按「子模块 · 只读/可写」授予；管理员权限 = 所授角色权限并集，变更即时生效。
      </span>
    </div>

    <p v-if="notice" class="ap-alert ap-alert-hint" role="status">{{ notice }}</p>

    <!-- ===================== 管理员 ===================== -->
    <template v-if="activeTab === 'admins'">
      <form class="ap-card ap-toolbar" @submit.prevent="searchAdmins">
        <label class="ap-field">
          <span class="ap-field-label">账号/姓名</span>
          <input v-model="keyword" placeholder="关键字检索用户名或姓名" maxlength="64" />
        </label>
        <div class="ap-actions">
          <button type="submit" class="ap-btn ap-btn-primary" :disabled="adminLoading">查询</button>
          <button type="button" class="ap-btn ap-btn-ghost" @click="resetSearch">重置</button>
        </div>
      </form>

      <p class="ap-muted hints">
        新建账号默认启用、默认无权限，需通过「角色授权」授予权限；内置 admin 隐式拥有全部权限，
        不可删除/停用。停用后该账号无法登录，已签发会话即时失效。
      </p>

      <p v-if="adminError" class="ap-alert ap-alert-error" role="alert">{{ adminError }}</p>

      <AdminTable
        :admins="store.admins"
        :loading="adminLoading"
        :can-write="canWrite"
        @edit="openEditAdmin"
        @bind-roles="openBindRoles"
        @reset-password="openResetPassword"
        @toggle-status="toggleAdminStatus"
        @remove="removeAdmin"
      />

      <div v-if="store.adminTotal > store.adminPageSize" class="ap-pagination">
        <button
          type="button"
          class="ap-btn ap-btn-ghost ap-btn-sm"
          :disabled="adminPage <= 1 || adminLoading"
          @click="loadAdmins(adminPage - 1)"
        >
          上一页
        </button>
        <span class="ap-muted">
          第 {{ adminPage }} 页 / 共 {{ adminTotalPages }} 页（{{ store.adminTotal }} 条）
        </span>
        <button
          type="button"
          class="ap-btn ap-btn-ghost ap-btn-sm"
          :disabled="adminPage >= adminTotalPages || adminLoading"
          @click="loadAdmins(adminPage + 1)"
        >
          下一页
        </button>
      </div>
    </template>

    <!-- ===================== 角色 ===================== -->
    <template v-else>
      <p class="ap-muted hints">
        角色 = 权限范围集合（子模块 × 只读/可写）。日志子模块默认只读；系统设置（管理员/证书/SSH）
        为敏感子模块，仅可授予可写。被管理员引用的角色禁止删除。
      </p>

      <p v-if="roleError" class="ap-alert ap-alert-error" role="alert">{{ roleError }}</p>

      <RoleTable
        :roles="store.roles"
        :loading="roleLoading"
        :can-write="canWrite"
        :modules="store.permissionModules"
        :admin-counts="store.roleAdminCounts"
        :count-truncated="store.roleAdminCountTruncated"
        @edit="openEditRole"
        @remove="removeRole"
      />
    </template>

    <!-- ===================== 弹窗 ===================== -->
    <AdminFormDialog
      :open="adminFormOpen"
      :item="editingAdmin"
      :saving="adminSaving"
      :error-message="adminFormError"
      @close="closeAdminForm"
      @create="createAdmin"
      @update="updateAdmin"
    />

    <AdminRoleBindDialog
      :open="bindOpen"
      :admin="bindTarget"
      :roles="store.roles"
      :checked-ids="bindCheckedIds"
      :modules="store.permissionModules"
      :saving="bindSaving"
      :error-message="bindError"
      @close="closeBind"
      @submit="submitBindRoles"
    />

    <AdminPasswordDialog
      :open="passwordOpen"
      :admin="passwordTarget"
      :saving="passwordSaving"
      :error-message="passwordError"
      @close="closeResetPassword"
      @submit="submitResetPassword"
    />

    <RoleFormDialog
      :open="roleFormOpen"
      :item="editingRole"
      :modules="store.permissionModules"
      :saving="roleSaving"
      :error-message="roleFormError"
      @close="closeRoleForm"
      @create="createRole"
      @update="updateRole"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import AdminFormDialog from '@/components/setting/AdminFormDialog.vue'
import AdminPasswordDialog from '@/components/setting/AdminPasswordDialog.vue'
import AdminRoleBindDialog from '@/components/setting/AdminRoleBindDialog.vue'
import AdminTable from '@/components/setting/AdminTable.vue'
import RoleFormDialog from '@/components/setting/RoleFormDialog.vue'
import RoleTable from '@/components/setting/RoleTable.vue'
import { usePermission } from '@/composables/usePermission'
import { useAuthStore } from '@/stores/auth'
import { useSettingStore } from '@/stores/setting'
import type {
  AdminCreatePayload,
  AdminInfo,
  AdminUpdatePayload,
  RoleInfo,
  RolePayload,
} from '@/types'
import { describeSettingError } from '@/utils/setting'

// 管理员管理页（PRD §3.6 / api-contract §2.7）：管理员 CRUD + 角色授权 + 角色管理（含权限矩阵）。
// 写操作统一受 setting.admin:write 控制（前端仅控制显隐，服务端 AuthZ 为最终判定）。
const store = useSettingStore()
const auth = useAuthStore()
const { hasPerm } = usePermission()

const canWrite = computed(() => hasPerm('setting.admin', 'write'))
const adminLoading = computed(() => store.adminLoading)
const roleLoading = computed(() => store.roleLoading)
const adminPage = computed(() => store.adminPage)
const adminTotalPages = computed(() =>
  Math.max(1, Math.ceil(store.adminTotal / (store.adminPageSize || 20))),
)

const activeTab = ref<'admins' | 'roles'>('admins')
const rolesLoaded = ref(false)
const keyword = ref('')
const adminError = ref('')
const roleError = ref('')
const notice = ref('')

// ---- 管理员表单 ----
const adminFormOpen = ref(false)
const editingAdmin = ref<AdminInfo | null>(null)
const adminSaving = ref(false)
const adminFormError = ref('')

// ---- 角色授权 ----
const bindOpen = ref(false)
const bindTarget = ref<AdminInfo | null>(null)
const bindCheckedIds = ref<string[]>([])
const bindSaving = ref(false)
const bindError = ref('')

// ---- 重置密码 ----
const passwordOpen = ref(false)
const passwordTarget = ref<AdminInfo | null>(null)
const passwordSaving = ref(false)
const passwordError = ref('')

// ---- 角色表单 ----
const roleFormOpen = ref(false)
const editingRole = ref<RoleInfo | null>(null)
const roleSaving = ref(false)
const roleFormError = ref('')

// ===================== 数据加载 =====================

async function loadAdmins(targetPage = 1): Promise<void> {
  adminError.value = ''
  try {
    await store.fetchAdmins({
      keyword: keyword.value.trim() || undefined,
      page: targetPage,
      page_size: store.adminPageSize || 20,
    })
  } catch (err) {
    adminError.value = describeSettingError(err, '加载管理员列表失败')
  }
}

/** 角色列表 + 关联管理员数 + 权限点元数据（后两者失败不阻断列表展示）。 */
async function loadRoles(): Promise<void> {
  roleError.value = ''
  try {
    await store.fetchRoles()
    rolesLoaded.value = true
  } catch (err) {
    roleError.value = describeSettingError(err, '加载角色列表失败')
    return
  }
  await refreshRoleCounts()
  try {
    await store.fetchPermissionModules()
  } catch {
    // 权限点元数据缺失时摘要退化为 module_code，不阻塞角色列表。
  }
}

async function refreshRoleCounts(): Promise<void> {
  try {
    await store.fetchRoleAdminCounts()
  } catch {
    // 引用计数仅用于展示与删除前置提示，失败时保持 0（服务端 26003 仍是最终判定）。
  }
}

function switchTab(tab: 'admins' | 'roles'): void {
  activeTab.value = tab
  notice.value = ''
  if (tab === 'roles' && !rolesLoaded.value) void loadRoles()
}

function searchAdmins(): void {
  void loadAdmins(1)
}

function resetSearch(): void {
  keyword.value = ''
  void loadAdmins(1)
}

// ===================== 管理员操作 =====================

function openCreateAdmin(): void {
  if (!canWrite.value) return
  editingAdmin.value = null
  adminFormError.value = ''
  adminFormOpen.value = true
}

function openEditAdmin(admin: AdminInfo): void {
  if (!canWrite.value) return
  editingAdmin.value = admin
  adminFormError.value = ''
  adminFormOpen.value = true
}

function closeAdminForm(): void {
  if (adminSaving.value) return
  adminFormOpen.value = false
  editingAdmin.value = null
  adminFormError.value = ''
}

async function createAdmin(payload: AdminCreatePayload): Promise<void> {
  if (adminSaving.value) return
  adminSaving.value = true
  adminFormError.value = ''
  notice.value = ''
  try {
    await store.addAdmin(payload)
    adminFormOpen.value = false
    editingAdmin.value = null
    await loadAdmins(1)
    notice.value = '管理员已创建（默认无权限，请通过「角色授权」授予权限）。'
  } catch (err) {
    // 26001 用户名已存在 / 20004 口令不合规 / 10003 参数非法。
    adminFormError.value = describeSettingError(err, '新建管理员失败')
  } finally {
    adminSaving.value = false
  }
}

async function updateAdmin(payload: AdminUpdatePayload): Promise<void> {
  const target = editingAdmin.value
  if (adminSaving.value || !target) return
  adminSaving.value = true
  adminFormError.value = ''
  notice.value = ''
  try {
    await store.editAdmin(target.id, payload)
    adminFormOpen.value = false
    editingAdmin.value = null
    await loadAdmins(adminPage.value)
    notice.value = '管理员信息已更新。'
  } catch (err) {
    // 10402 并发冲突 / 10301 不存在。
    adminFormError.value = describeSettingError(err, '保存管理员失败')
  } finally {
    adminSaving.value = false
  }
}

async function toggleAdminStatus(admin: AdminInfo): Promise<void> {
  if (!canWrite.value) return
  const next: 'enabled' | 'disabled' = admin.status === 'disabled' ? 'enabled' : 'disabled'
  const selfHint =
    next === 'disabled' && auth.user?.id === admin.id
      ? '这是当前登录账号，停用后会立即失去访问权限。'
      : ''
  const action = next === 'disabled' ? '停用' : '启用'
  if (!window.confirm(`确认${action}管理员「${admin.username}」？${selfHint}`)) return

  adminError.value = ''
  notice.value = ''
  try {
    await store.setAdminStatus(admin.id, next, admin.version)
    await loadAdmins(adminPage.value)
    void refreshRoleCounts()
    notice.value = `管理员「${admin.username}」已${action}。`
  } catch (err) {
    // 26002 内置 admin 保护 / 10402 并发冲突。
    adminError.value = describeSettingError(err, `${action}管理员失败`)
  }
}

async function removeAdmin(admin: AdminInfo): Promise<void> {
  if (!canWrite.value) return
  if (admin.is_builtin) return
  const selfHint = auth.user?.id === admin.id ? '（当前登录账号，删除后会话立即失效）' : ''
  if (
    !window.confirm(
      `确认删除管理员「${admin.username}」${selfHint}？删除为逻辑删除，同时解绑其全部角色。`,
    )
  ) {
    return
  }

  adminError.value = ''
  notice.value = ''
  try {
    await store.removeAdmin(admin.id)
    // 当前页删空时回到上一页，避免停留在空列表。
    const lastOnPage = store.admins.length === 1 && adminPage.value > 1
    await loadAdmins(lastOnPage ? adminPage.value - 1 : adminPage.value)
    void refreshRoleCounts()
    notice.value = `管理员「${admin.username}」已删除。`
  } catch (err) {
    adminError.value = describeSettingError(err, '删除管理员失败')
  }
}

// ===================== 角色授权 =====================

async function openBindRoles(admin: AdminInfo): Promise<void> {
  if (!canWrite.value) return
  adminError.value = ''
  notice.value = ''
  try {
    // 先取齐角色列表、权限点元数据与当前绑定，再开弹窗：
    // 避免「绑定状态未知时保存」导致误清空角色（全量替换语义）。
    if (store.roles.length === 0) await store.fetchRoles()
    await store.fetchPermissionModules()
    const bound = await store.fetchAdminRoles(admin.id)
    bindTarget.value = admin
    bindCheckedIds.value = bound.map((role) => role.id)
    bindError.value = ''
    bindOpen.value = true
  } catch (err) {
    adminError.value = describeSettingError(err, '加载管理员角色失败')
  }
}

function closeBind(): void {
  if (bindSaving.value) return
  bindOpen.value = false
  bindTarget.value = null
  bindCheckedIds.value = []
  bindError.value = ''
}

async function submitBindRoles(roleIds: string[]): Promise<void> {
  const target = bindTarget.value
  if (bindSaving.value || !target) return
  bindSaving.value = true
  bindError.value = ''
  try {
    await store.bindRoles(target.id, roleIds)
    bindOpen.value = false
    bindTarget.value = null
    await loadAdmins(adminPage.value)
    void refreshRoleCounts()
    notice.value = `管理员「${target.username}」的角色授权已保存，权限即时生效。`
  } catch (err) {
    bindError.value = describeSettingError(err, '保存角色授权失败')
  } finally {
    bindSaving.value = false
  }
}

// ===================== 重置密码 =====================

function openResetPassword(admin: AdminInfo): void {
  if (!canWrite.value) return
  passwordTarget.value = admin
  passwordError.value = ''
  passwordOpen.value = true
}

function closeResetPassword(): void {
  if (passwordSaving.value) return
  passwordOpen.value = false
  passwordTarget.value = null
  passwordError.value = ''
}

async function submitResetPassword(newPassword: string): Promise<void> {
  const target = passwordTarget.value
  if (passwordSaving.value || !target) return
  passwordSaving.value = true
  passwordError.value = ''
  try {
    await store.resetAdminPwd(target.id, newPassword)
    passwordOpen.value = false
    passwordTarget.value = null
    notice.value = `管理员「${target.username}」的密码已重置，下次登录需强制改密。`
  } catch (err) {
    // 20004 口令不合规 / 10301 不存在。
    passwordError.value = describeSettingError(err, '重置密码失败')
  } finally {
    passwordSaving.value = false
  }
}

// ===================== 角色管理 =====================

/** 确保权限点元数据已加载（权限矩阵事实源，缺失时不允许提交）。 */
async function ensurePermissionModules(): Promise<boolean> {
  try {
    await store.fetchPermissionModules()
    return true
  } catch (err) {
    roleError.value = describeSettingError(err, '加载权限点元数据失败')
    return false
  }
}

async function openCreateRole(): Promise<void> {
  if (!canWrite.value) return
  if (!(await ensurePermissionModules())) return
  editingRole.value = null
  roleFormError.value = ''
  roleFormOpen.value = true
}

async function openEditRole(role: RoleInfo): Promise<void> {
  if (!canWrite.value) return
  if (!(await ensurePermissionModules())) return
  editingRole.value = role
  roleFormError.value = ''
  roleFormOpen.value = true
}

function closeRoleForm(): void {
  if (roleSaving.value) return
  roleFormOpen.value = false
  editingRole.value = null
  roleFormError.value = ''
}

async function createRole(payload: RolePayload): Promise<void> {
  if (roleSaving.value) return
  roleSaving.value = true
  roleFormError.value = ''
  notice.value = ''
  try {
    await store.saveRole(null, payload)
    roleFormOpen.value = false
    await loadRoles()
    notice.value = `角色「${payload.name}」已创建。`
  } catch (err) {
    roleFormError.value = describeSettingError(err, '新建角色失败')
  } finally {
    roleSaving.value = false
  }
}

async function updateRole(payload: RolePayload): Promise<void> {
  const target = editingRole.value
  if (roleSaving.value || !target) return
  roleSaving.value = true
  roleFormError.value = ''
  notice.value = ''
  try {
    await store.saveRole(target.id, payload)
    roleFormOpen.value = false
    editingRole.value = null
    await loadRoles()
    notice.value = `角色「${payload.name}」已更新，绑定该角色的管理员权限即时生效。`
  } catch (err) {
    // 10302 角色名重复 / 10402 并发冲突。
    roleFormError.value = describeSettingError(err, '保存角色失败')
  } finally {
    roleSaving.value = false
  }
}

async function removeRole(role: RoleInfo): Promise<void> {
  if (!canWrite.value) return
  if (!window.confirm(`确认删除角色「${role.name}」？删除为逻辑删除，角色权限矩阵一并清理。`)) {
    return
  }
  roleError.value = ''
  notice.value = ''
  try {
    await store.removeRole(role.id)
    await loadRoles()
    await loadAdmins(adminPage.value)
    notice.value = `角色「${role.name}」已删除。`
  } catch (err) {
    // 26003 角色被管理员引用，禁止删除。
    roleError.value = describeSettingError(err, '删除角色失败')
  }
}

onMounted(() => {
  void loadAdmins(1)
  void store.fetchPermissionModules().catch(() => {
    // 权限点元数据缺失仅影响角色摘要/矩阵，管理员列表不受影响。
  })
})
</script>

<style scoped>
.tab-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
}

.tab {
  padding: 6px 14px;
  border: 1px solid transparent;
  border-radius: 6px;
  background: #f3f4f6;
  color: #4b5563;
  font-size: 13px;
  cursor: pointer;
}

.tab.active {
  background: #eff6ff;
  border-color: #bfdbfe;
  color: var(--ap-primary);
  font-weight: 600;
}

.tab-hint {
  margin-left: auto;
  font-size: 12px;
}

.hints {
  margin: 0;
  font-size: 12px;
  line-height: 1.6;
}
</style>
