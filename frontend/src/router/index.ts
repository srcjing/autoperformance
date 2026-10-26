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
    path: '/',
    component: () => import('@/layouts/MainLayout.vue'),
    redirect: '/environments/list',
    meta: { requiresAuth: true },
    children: [
      // 环境管理
      { path: 'environments/list', name: 'environment-list', component: () => import('@/views/environment/EnvironmentListView.vue'), meta: { title: '环境列表', module: 'env.list' } },
      { path: 'environments/tags', name: 'tag-list', component: () => import('@/views/environment/TagListView.vue'), meta: { title: '标签维护', module: 'env.tag' } },
      // 产品管理
      { path: 'products/list', name: 'product-list', component: () => import('@/views/product/ProductListView.vue'), meta: { title: '产品列表', module: 'product.list' } },
      { path: 'products/:productId/interfaces', name: 'interface-list', component: () => import('@/views/product/InterfaceListView.vue'), meta: { title: '接口管理', module: 'product.interface' } },
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

// 导航守卫（对齐 frontend-structure.md §6）。
// 骨架：仅校验登录态；权限守卫（meta.module/permission → hasPerm）后续按实现计划完善。
router.beforeEach((to) => {
  const auth = useAuthStore()
  if (!to.meta.public && !auth.isAuthenticated) {
    return { name: 'login', query: { redirect: to.fullPath } }
  }
  return true
})

export default router
