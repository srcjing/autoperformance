import { defineStore } from 'pinia'
import { ref } from 'vue'
import {
  bindAdminRoles,
  createAdmin,
  createRole,
  createSshHost,
  deleteAdmin,
  deleteCertificate,
  deleteRole,
  deleteSshHost,
  getAdminRoles,
  importCertificate,
  importSshHosts,
  listAdmins,
  listCertificates,
  listPermissionModules,
  listRoles,
  listSshHosts,
  reinitCertificate,
  resetAdminPassword,
  updateAdmin,
  updateAdminStatus,
  updateRole,
  updateSshHost,
} from '@/api/setting'
import type {
  AdminCreatePayload,
  AdminInfo,
  AdminQuery,
  AdminUpdatePayload,
  CertificateImportPayload,
  CertificateInfo,
  PermissionModule,
  RoleInfo,
  RolePayload,
  SshHostImportResult,
  SshHostInfo,
  SshHostPayload,
  SshHostQuery,
} from '@/types'

// 系统设置 Store（frontend-structure.md §3）：管理员 / 角色 / 证书 / SSH-Host 的领域数据。
// 页面局部状态（弹窗开关/表单草稿/筛选条件）留在组件，领域数据在此集中。
// 安全：私钥只在上行请求中出现，Store 不缓存任何密钥材料（DES-I-002）。

/** 角色引用数聚合时的取数页大小（api-contract §1.3 分页上限 200）。 */
const ROLE_REF_PAGE_SIZE = 200

export const useSettingStore = defineStore('setting', () => {
  // ===================== 管理员 =====================
  const admins = ref<AdminInfo[]>([])
  const adminTotal = ref(0)
  const adminPage = ref(1)
  const adminPageSize = ref(20)
  const adminLoading = ref(false)

  // ===================== 角色 / 权限矩阵 =====================
  const roles = ref<RoleInfo[]>([])
  const roleLoading = ref(false)
  const permissionModules = ref<PermissionModule[]>([])
  const permissionLoading = ref(false)

  // 角色 → 关联管理员数：角色 DTO 未暴露引用计数，由管理员列表（含内嵌角色）聚合得出。
  const roleAdminCounts = ref<Record<string, number>>({})
  const roleAdminCountTruncated = ref(false)

  // ===================== 证书 =====================
  const certificates = ref<CertificateInfo[]>([])
  const certificateTotal = ref(0)
  const certificateLoading = ref(false)

  // ===================== SSH/Host =====================
  const sshHosts = ref<SshHostInfo[]>([])
  const sshHostTotal = ref(0)
  const sshHostPage = ref(1)
  const sshHostPageSize = ref(20)
  const sshHostLoading = ref(false)

  // ===================== 管理员操作 =====================

  /** 管理员列表（关键字 + 分页）；返回角色绑定信息用于列表展示。 */
  async function fetchAdmins(query: AdminQuery = {}): Promise<void> {
    adminLoading.value = true
    try {
      const res = await listAdmins(query)
      admins.value = res.list
      adminTotal.value = res.total
      adminPage.value = res.page
      adminPageSize.value = res.page_size
    } finally {
      adminLoading.value = false
    }
  }

  /** 新建管理员（仅上行白名单字段；默认启用、默认无权限、首登强制改密）。 */
  async function addAdmin(payload: AdminCreatePayload): Promise<AdminInfo> {
    return await createAdmin(payload)
  }

  /** 修改管理员基础信息（用户名/状态不可改，带 version 乐观锁）。 */
  async function editAdmin(id: string, payload: AdminUpdatePayload): Promise<AdminInfo> {
    return await updateAdmin(id, payload)
  }

  /** 删除管理员（逻辑删除；内置 admin 由服务端回 26002）。 */
  async function removeAdmin(id: string): Promise<void> {
    await deleteAdmin(id)
  }

  /** 停用/启用管理员（权限缓存由服务端即时失效）。 */
  async function setAdminStatus(
    id: string,
    status: 'enabled' | 'disabled',
    version?: number,
  ): Promise<void> {
    await updateAdminStatus(id, { status, version })
  }

  /** 重置管理员口令（成功后该账号下次登录强制改密）。 */
  async function resetAdminPwd(id: string, newPassword: string): Promise<void> {
    await resetAdminPassword(id, { new_password: newPassword })
  }

  /** 查询管理员已绑定角色（角色授权弹窗回填）。 */
  async function fetchAdminRoles(id: string): Promise<RoleInfo[]> {
    const res = await getAdminRoles(id)
    return res.list
  }

  /** 角色授权：全量替换绑定，保存后权限即时生效。 */
  async function bindRoles(id: string, roleIds: string[]): Promise<void> {
    await bindAdminRoles(id, roleIds)
  }

  // ===================== 角色 / 权限矩阵 =====================

  /** 角色列表（含权限矩阵）。 */
  async function fetchRoles(): Promise<void> {
    roleLoading.value = true
    try {
      const res = await listRoles()
      roles.value = res.list
    } finally {
      roleLoading.value = false
    }
  }

  /** 新建/修改角色（id 为空表示新建；权限矩阵全量替换）。 */
  async function saveRole(id: string | null, payload: RolePayload): Promise<RoleInfo> {
    return id ? await updateRole(id, payload) : await createRole(payload)
  }

  /** 删除角色（被管理员引用时服务端回 26003）。 */
  async function removeRole(id: string): Promise<void> {
    await deleteRole(id)
  }

  /** 权限点清单（子模块元数据；首次获取后缓存，避免重复请求）。 */
  async function fetchPermissionModules(force = false): Promise<void> {
    if (!force && permissionModules.value.length > 0) return
    permissionLoading.value = true
    try {
      const res = await listPermissionModules()
      permissionModules.value = res.list
    } finally {
      permissionLoading.value = false
    }
  }

  /**
   * 聚合角色关联管理员数（供角色列表展示与删除前置提示）。
   *
   * 后端未在角色 DTO 暴露引用计数，此处按管理员列表（含内嵌角色）汇总；
   * 管理员总数超过单页上限时标记 truncated，由 UI 提示计数为下限。
   */
  async function fetchRoleAdminCounts(): Promise<void> {
    const res = await listAdmins({ page: 1, page_size: ROLE_REF_PAGE_SIZE })
    const counts: Record<string, number> = {}
    for (const admin of res.list) {
      for (const role of admin.roles ?? []) {
        counts[role.id] = (counts[role.id] ?? 0) + 1
      }
    }
    roleAdminCounts.value = counts
    roleAdminCountTruncated.value = res.total > res.list.length
  }

  /** 证书列表（内置自签证书由后端保证存在）。 */
  async function fetchCertificates(): Promise<void> {
    certificateLoading.value = true
    try {
      const res = await listCertificates()
      certificates.value = res.list
      certificateTotal.value = res.total
    } finally {
      certificateLoading.value = false
    }
  }

  /** 导入证书（证书内容/私钥仅上行，响应不含密钥材料）。 */
  async function uploadCertificate(payload: CertificateImportPayload): Promise<CertificateInfo> {
    return await importCertificate(payload)
  }

  /** 删除证书（内置自签证书由服务端拒绝，返回 26004）。 */
  async function removeCertificate(id: string): Promise<void> {
    await deleteCertificate(id)
  }

  /** 重新初始化内置自签证书（仅更新有效期）。 */
  async function reinitBuiltinCertificate(id: string): Promise<CertificateInfo> {
    return await reinitCertificate(id)
  }

  /** SSH/Host 列表（筛选 + 分页）。 */
  async function fetchSshHosts(query: SshHostQuery = {}): Promise<void> {
    sshHostLoading.value = true
    try {
      const res = await listSshHosts(query)
      sshHosts.value = res.list
      sshHostTotal.value = res.total
      sshHostPage.value = res.page
      sshHostPageSize.value = res.page_size
    } finally {
      sshHostLoading.value = false
    }
  }

  /** 新增/修改 SSH/Host（id 为空表示新增）。 */
  async function saveSshHost(
    id: string | null,
    payload: SshHostPayload,
  ): Promise<SshHostInfo> {
    return id ? await updateSshHost(id, payload) : await createSshHost(payload)
  }

  /** 删除 SSH/Host（被环境引用时服务端返回 26005）。 */
  async function removeSshHost(id: string): Promise<void> {
    await deleteSshHost(id)
  }

  /** 批量导入 SSH/Host（重复项跳过，返回导入统计）。 */
  async function uploadSshHosts(file: File): Promise<SshHostImportResult> {
    return await importSshHosts(file)
  }

  return {
    admins,
    adminTotal,
    adminPage,
    adminPageSize,
    adminLoading,
    roles,
    roleLoading,
    permissionModules,
    permissionLoading,
    roleAdminCounts,
    roleAdminCountTruncated,
    fetchAdmins,
    addAdmin,
    editAdmin,
    removeAdmin,
    setAdminStatus,
    resetAdminPwd,
    fetchAdminRoles,
    bindRoles,
    fetchRoles,
    saveRole,
    removeRole,
    fetchPermissionModules,
    fetchRoleAdminCounts,
    certificates,
    certificateTotal,
    certificateLoading,
    sshHosts,
    sshHostTotal,
    sshHostPage,
    sshHostPageSize,
    sshHostLoading,
    fetchCertificates,
    uploadCertificate,
    removeCertificate,
    reinitBuiltinCertificate,
    fetchSshHosts,
    saveSshHost,
    removeSshHost,
    uploadSshHosts,
  }
})
