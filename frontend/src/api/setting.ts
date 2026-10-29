import { request } from './http'
import type {
  AdminCreatePayload,
  AdminInfo,
  AdminQuery,
  AdminResetPasswordPayload,
  AdminStatusPayload,
  AdminUpdatePayload,
  CertificateImportPayload,
  CertificateInfo,
  PageResult,
  PermissionModuleListResult,
  RoleInfo,
  RoleListResult,
  RolePayload,
  SshHostImportResult,
  SshHostInfo,
  SshHostPayload,
  SshHostQuery,
} from '@/types'

// 系统设置接口（api-contract §2.7）：
//   管理员  GET/POST /admins、PUT/DELETE /admins/{id}、PATCH /admins/{id}/status、
//           POST /admins/{id}/reset-password、GET/POST /admins/{id}/roles
//   角色    GET/POST /roles、PUT/DELETE /roles/{id}、GET /permissions（权限矩阵元数据）
//   证书    GET/POST /certificates、DELETE /certificates/{id}、POST /certificates/{id}/reinit
//   SSH     GET/POST /ssh-hosts、PUT/DELETE /ssh-hosts/{id}、POST /ssh-hosts/import
// 权限点：setting.admin / setting.cert / setting.ssh（敏感子模块仅 write 授权）。
// 安全：私钥/证书内容仅上行，任何响应都不回传密钥材料（DES-I-002）；
//       管理员接口不回传 password_hash，新建/修改仅上行白名单字段（DES-E-002）。

/** 证书选项（环境表单 cert_id 选择来源）。 */
export type CertificateOption = CertificateInfo

/** SSH/Host 选项（环境表单 ssh_host_id 选择来源）。 */
export type SshHostOption = SshHostInfo

/** 构造 query 字符串（跳过空值）。 */
function buildQuery(params: Record<string, string | number | undefined>): string {
  const search = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === '') continue
    search.set(key, String(value))
  }
  const qs = search.toString()
  return qs ? `?${qs}` : ''
}

// ===================== 管理员 / 角色（setting.admin，敏感 write-only） =====================

/** 管理员列表（关键字匹配用户名/姓名 + 分页）。 */
export function listAdmins(query: AdminQuery = {}): Promise<PageResult<AdminInfo>> {
  return request<PageResult<AdminInfo>>(`/admins${buildQuery({ ...query })}`)
}

/** 管理员详情（10301 不存在）。 */
export function getAdmin(id: string): Promise<AdminInfo> {
  return request<AdminInfo>(`/admins/${id}`)
}

/** 新建管理员（用户名重复 26001；口令不合规 20004）。 */
export function createAdmin(payload: AdminCreatePayload): Promise<AdminInfo> {
  return request<AdminInfo>('/admins', { method: 'POST', body: payload })
}

/** 修改管理员（用户名不可改；并发冲突 10402）。 */
export function updateAdmin(id: string, payload: AdminUpdatePayload): Promise<AdminInfo> {
  return request<AdminInfo>(`/admins/${id}`, { method: 'PUT', body: payload })
}

/** 删除管理员（逻辑删除；内置 admin → 26002）。 */
export function deleteAdmin(id: string): Promise<null> {
  return request<null>(`/admins/${id}`, { method: 'DELETE' })
}

/** 停用/启用管理员（内置 admin → 26002；停用后无法登录 10202）。 */
export function updateAdminStatus(id: string, payload: AdminStatusPayload): Promise<null> {
  return request<null>(`/admins/${id}/status`, { method: 'PATCH', body: payload })
}

/** 重置管理员口令（成功后下次登录强制改密）。 */
export function resetAdminPassword(
  id: string,
  payload: AdminResetPasswordPayload,
): Promise<null> {
  return request<null>(`/admins/${id}/reset-password`, { method: 'POST', body: payload })
}

/** 查询管理员已绑定角色。 */
export function getAdminRoles(id: string): Promise<RoleListResult> {
  return request<RoleListResult>(`/admins/${id}/roles`)
}

/** 角色授权：全量替换管理员角色绑定（权限即时生效）。 */
export function bindAdminRoles(id: string, roleIds: string[]): Promise<null> {
  return request<null>(`/admins/${id}/roles`, {
    method: 'POST',
    body: { role_ids: roleIds },
  })
}

/** 角色列表（含权限矩阵）。 */
export function listRoles(): Promise<RoleListResult> {
  return request<RoleListResult>('/roles')
}

/** 角色详情（含权限矩阵）。 */
export function getRole(id: string): Promise<RoleInfo> {
  return request<RoleInfo>(`/roles/${id}`)
}

/** 新建角色（角色名重复 10302；授权项超出可授权范围 10003）。 */
export function createRole(payload: RolePayload): Promise<RoleInfo> {
  return request<RoleInfo>('/roles', { method: 'POST', body: payload })
}

/** 修改角色（权限矩阵全量替换；并发冲突 10402）。 */
export function updateRole(id: string, payload: RolePayload): Promise<RoleInfo> {
  return request<RoleInfo>(`/roles/${id}`, { method: 'PUT', body: payload })
}

/** 删除角色（被管理员引用 → 26003）。 */
export function deleteRole(id: string): Promise<null> {
  return request<null>(`/roles/${id}`, { method: 'DELETE' })
}

/** 权限点清单（子模块元数据：可授权限 + 敏感标记，供权限矩阵渲染）。 */
export function listPermissionModules(): Promise<PermissionModuleListResult> {
  return request<PermissionModuleListResult>('/permissions')
}

// ===================== 证书 =====================

/** 证书列表（内置自签证书排首位，状态由有效期推导）。 */
export function listCertificates(page = 1, pageSize = 200): Promise<PageResult<CertificateInfo>> {
  return request<PageResult<CertificateInfo>>(
    `/certificates${buildQuery({ page, page_size: pageSize })}`,
  )
}

/** 导入证书（multipart：证书文件必填，私钥文件可选；重名 10302）。 */
export function importCertificate(payload: CertificateImportPayload): Promise<CertificateInfo> {
  const form = new FormData()
  form.append('file', payload.cert_file)
  if (payload.key_file) form.append('key', payload.key_file)
  form.append('name', payload.name)
  if (payload.issuer) form.append('issuer', payload.issuer)
  if (payload.valid_from) form.append('valid_from', payload.valid_from)
  if (payload.valid_to) form.append('valid_to', payload.valid_to)
  if (payload.associated_service) {
    form.append('associated_service', payload.associated_service)
  }
  return request<CertificateInfo>('/certificates', { method: 'POST', body: form })
}

/** 删除证书（内置自签证书不可删除 → 26004）。 */
export function deleteCertificate(id: string): Promise<null> {
  return request<null>(`/certificates/${id}`, { method: 'DELETE' })
}

/** 重新初始化内置自签证书（仅更新有效期；非内置 → 10401）。 */
export function reinitCertificate(id: string): Promise<CertificateInfo> {
  return request<CertificateInfo>(`/certificates/${id}/reinit`, { method: 'POST' })
}

// ===================== SSH/Host =====================

/** SSH/Host 列表（关键字/类型 + 分页）。 */
export function listSshHosts(query: SshHostQuery = {}): Promise<PageResult<SshHostInfo>> {
  return request<PageResult<SshHostInfo>>(`/ssh-hosts${buildQuery({ ...query })}`)
}

/** 新增 SSH/Host（重复 type+host+port → 10302；私钥由服务端加密存储）。 */
export function createSshHost(payload: SshHostPayload): Promise<SshHostInfo> {
  return request<SshHostInfo>('/ssh-hosts', { method: 'POST', body: payload })
}

/** 修改 SSH/Host（必须携带 version；并发冲突 10402；私钥留空表示不改）。 */
export function updateSshHost(id: string, payload: SshHostPayload): Promise<SshHostInfo> {
  return request<SshHostInfo>(`/ssh-hosts/${id}`, { method: 'PUT', body: payload })
}

/** 删除 SSH/Host（被环境引用 → 26005）。 */
export function deleteSshHost(id: string): Promise<null> {
  return request<null>(`/ssh-hosts/${id}`, { method: 'DELETE' })
}

/** 批量导入 SSH/Host（JSON/CSV，重复项跳过；文件类型不允许 24002）。 */
export function importSshHosts(file: File): Promise<SshHostImportResult> {
  const form = new FormData()
  form.append('file', file)
  return request<SshHostImportResult>('/ssh-hosts/import', { method: 'POST', body: form })
}
