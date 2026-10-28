import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

// 路由表（对齐 frontend-structure.md §1.1）。
// 所有业务页面懒加载（() => import(...)），内嵌部署下减小首屏体积。
const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    component: () => import('@/layouts/AuthLayout.vue'),
    children: [
      {
        path: '',
        name: 'login',
        component: () => import('@/views/auth/LoginView.vue'),
        meta: { title: '登录', public: true },
      },
    ],
  },
  {
    path: '/change-password',
    component: () => import('@/layouts/AuthLayout.vue'),
    children: [
      {
        path: '',
        name: 'change-password',
        component: () => import('@/views/auth/ChangePasswordView.vue'),
        meta: { title: '修改密码', requiresAuth: true },
      },
    ],
  },
  {
    path: '/',
    component: () => import('@/layouts/MainLayout.vue'),
    redirect: '/overview',
    meta: { requiresAuth: true },
    children: [
      // 环境管理
      { path: 'overview', name: 'overview', component: () => import('@/views/OverviewView.vue'), meta: { title: '概览' } },
      { path: 'environments/list', name: 'environment-list', component: () => import('@/views/environment/EnvironmentListView.vue'), meta: { title: '环境列表', module: 'env.list' } },
      { path: 'environments/tags', name: 'tag-list', component: () => import('@/views/environment/TagListView.vue'), meta: { title: '标签维护', module: 'env.tag' } },
      // 产品管理
      { path: 'products/list', name: 'product-list', component: () => import('@/views/product/ProductListView.vue'), meta: { title: '产品列表', module: 'product.list' } },
      // 接口管理：产品由页面顶部下拉选择（可选 productId 用于从产品列表直达）。
      { path: 'products/interfaces/:productId?', name: 'interface-list', component: () => import('@/views/product/InterfaceListView.vue'), meta: { title: '接口管理', module: 'product.interface' } },
      // 兼容旧链接（/products/1/interfaces）与历史收藏。
      {
        path: 'products/:productId(\\d+)/interfaces',
        redirect: (to) => ({ name: 'interface-list', params: { productId: String(to.params.productId) } }),
      },
      // 测试管理
      { path: 'testing/templates', name: 'template-list', component: () => import('@/views/testing/TemplateListView.vue'), meta: { title: '预设模板', module: 'testing.template' } },
      { path: 'testing/tasks', name: 'task-list', component: () => import('@/views/testing/TaskListView.vue'), meta: { title: '测试任务', module: 'testing.task' } },
      { path: 'testing/tasks/:taskId', name: 'task-detail', component: () => import('@/views/testing/TaskDetailView.vue'), meta: { title: '任务详情', module: 'testing.task' } },
      // 报告管理
      { path: 'reports/list', name: 'report-list', component: () => import('@/views/report/ReportListView.vue'), meta: { title: '报告列表', module: 'report.list' } },
      { path: 'reports/:reportId', name: 'report-detail', component: () => import('@/views/report/ReportDetailView.vue'), meta: { title: '报告详情', module: 'report.list' } },
      // 日志管理
      { path: 'logs/user', name: 'user-log', component: () => import('@/views/log/UserLogView.vue'), meta: { title: '用户日志', module: 'log.user' } },
      { path: 'logs/system', name: 'system-log', component: () => import('@/views/log/SystemLogView.vue'), meta: { title: '系统日志', module: 'log.system' } },
      { path: 'logs/test', name: 'test-log', component: () => import('@/views/log/TestLogView.vue'), meta: { title: '测试日志', module: 'log.test' } },
      // 系统设置（敏感子模块：仅 write 授权可见）
      { path: 'settings/admins', name: 'admin-manage', component: () => import('@/views/setting/AdminManageView.vue'), meta: { title: '管理员管理', module: 'setting.admin', permission: 'write' } },
      { path: 'settings/certificates', name: 'certificate-manage', component: () => import('@/views/setting/CertificateManageView.vue'), meta: { title: '证书管理', module: 'setting.cert', permission: 'write' } },
      { path: 'settings/ssh-hosts', name: 'ssh-host-manage', component: () => import('@/views/setting/SshHostManageView.vue'), meta: { title: 'SSH 与 Host 管理', module: 'setting.ssh', permission: 'write' } },
      // 无权限
      { path: '403', name: 'forbidden', component: () => import('@/views/error/ForbiddenView.vue'), meta: { title: '无权限' } },
    ],
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'not-found',
    component: () => import('@/views/error/NotFoundView.vue'),
    meta: { title: '404', public: true },
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

/**
 * 导航守卫（对齐 frontend-structure.md §6 与 api-contract §1.4）：
 *   1. 未登录 → /login（携带 redirect）；
 *   2. 已登录但权限未加载 → GET /auth/me 加载（失败则清理会话回登录）；
 *   3. must_change_password=true → 强制停留改密页（除改密外全部重定向）；
 *   4. meta.module/meta.permission（默认 read）→ hasPerm 校验，不满足跳 403。
 *
 * 权限仅驱动前端显隐/路由跳转，最终鉴权在服务端（INV-1）。
 */
router.beforeEach(async (to) => {
  const auth = useAuthStore()

  if (to.meta.public) {
    if (to.name === 'login' && auth.isAuthenticated) {
      // 已有 token 时先加载权限集合，再决定默认落地页（刷新场景）。
      if (!auth.profileLoaded) {
        try {
          await auth.loadProfile()
        } catch {
          auth.clearSession()
          return true
        }
      }
      const redirect = typeof to.query.redirect === 'string' ? to.query.redirect : auth.homePath
      return redirect
    }
    return true
  }

  if (!auth.isAuthenticated) {
    return { name: 'login', query: { redirect: to.fullPath } }
  }

  if (!auth.profileLoaded) {
    try {
      await auth.loadProfile()
    } catch {
      auth.clearSession()
      return { name: 'login', query: { redirect: to.fullPath } }
    }
  }

  if (auth.mustChangePassword && to.name !== 'change-password') {
    return { name: 'change-password' }
  }

  const module = to.meta.module
  if (module) {
    const permission = to.meta.permission ?? 'read'
    if (!auth.hasPerm(module, permission)) {
      return { name: 'forbidden' }
    }
  }

  return true
})

export default router
