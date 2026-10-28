<template>
  <div class="app-shell">
    <!-- 侧边栏：按权限动态过滤的 6 大模块导航 -->
    <aside class="sidebar">
      <div class="logo">
        <div class="logo-brand">auto_performance</div>
        <div class="logo-sub">性能测试平台</div>
      </div>
      <nav class="nav">
        <RouterLink to="/overview" class="nav-item nav-single" active-class="active">
          <span class="nav-icon" v-html="ICONS.overview"></span><span>概览</span>
        </RouterLink>
        <div v-for="group in visibleNavGroups" :key="group.title" class="nav-group">
          <button type="button" class="nav-group-title" @click="toggleGroup(group.title)">
            <span class="nav-group-left">
              <span class="nav-icon" v-html="iconOf(group.title)"></span>
              <span class="nav-group-label">{{ group.title }}</span>
            </span>
            <span class="caret" :class="{ collapsed: collapsedGroups.has(group.title) }">▾</span>
          </button>
          <div v-show="!collapsedGroups.has(group.title)" class="nav-items">
            <RouterLink
              v-for="item in group.children"
              :key="item.path"
              :to="item.path"
              class="nav-item"
              active-class="active"
            >
              {{ item.label }}
            </RouterLink>
          </div>
        </div>
        <p v-if="visibleNavGroups.length === 0" class="nav-empty">暂无可访问的功能</p>
      </nav>
    </aside>

    <div class="main">
      <!-- 顶栏 -->
      <header class="header">
        <div class="breadcrumb">{{ currentTitle }}</div>
        <div class="user">
          <span class="user-name">{{ displayName }}</span>
          <RouterLink class="header-action" to="/change-password">修改密码</RouterLink>
          <button type="button" class="header-action logout" @click="onLogout">退出登录</button>
        </div>
      </header>

      <!-- 内容区 -->
      <main class="content">
        <RouterView />
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive } from 'vue'
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import type { PermissionLevel } from '@/utils/permission'

interface NavItem {
  path: string
  label: string
  module: string
  permission?: PermissionLevel
}

interface NavGroup {
  title: string
  children: NavItem[]
}

// 侧边栏导航（对齐 frontend-structure.md §5：6 大模块）。
// 按「子模块 read 权限」动态过滤；敏感子模块（管理员/证书/SSH）需 write 授权才可见。
const navGroups: NavGroup[] = [
  {
    title: '环境管理',
    children: [
      { path: '/environments/list', label: '环境列表', module: 'env.list' },
      { path: '/environments/tags', label: '标签维护', module: 'env.tag' },
    ],
  },
  {
    title: '产品管理',
    children: [
      { path: '/products/list', label: '产品列表', module: 'product.list' },
      { path: '/products/interfaces', label: '接口管理', module: 'product.interface' },
    ],
  },
  {
    title: '测试管理',
    children: [
      { path: '/testing/templates', label: '预设模板', module: 'testing.template' },
      { path: '/testing/tasks', label: '测试任务', module: 'testing.task' },
    ],
  },
  {
    title: '报告管理',
    children: [{ path: '/reports/list', label: '报告列表', module: 'report.list' }],
  },
  {
    title: '日志管理',
    children: [
      { path: '/logs/user', label: '用户日志', module: 'log.user' },
      { path: '/logs/system', label: '系统日志', module: 'log.system' },
      { path: '/logs/test', label: '测试日志', module: 'log.test' },
    ],
  },
  {
    title: '系统设置',
    children: [
      { path: '/settings/admins', label: '管理员', module: 'setting.admin', permission: 'write' },
      { path: '/settings/certificates', label: '证书', module: 'setting.cert', permission: 'write' },
      { path: '/settings/ssh-hosts', label: 'SSH/Host', module: 'setting.ssh', permission: 'write' },
    ],
  },
]

const route = useRoute()
const router = useRouter()
const auth = useAuthStore()

const visibleNavGroups = computed<NavGroup[]>(() =>
  navGroups
    .map((group) => ({
      ...group,
      children: group.children.filter((item) => auth.hasPerm(item.module, item.permission ?? 'read')),
    }))
    .filter((group) => group.children.length > 0),
)

// 一级模块默认全部收起（只显示标题，点击展开子模块）。
const collapsedGroups = reactive(new Set<string>(navGroups.map((group) => group.title)))

// 一级菜单图标（线性图标，与参考后台一致：左侧图标 + 文字 + 右侧箭头）。
const ICONS: Record<string, string> = {
  overview:
    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 9.5 12 3l9 6.5V21a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V9.5z"/><path d="M9 22V12h6v10"/></svg>',
  '环境管理':
    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="3" width="20" height="7" rx="2"/><rect x="2" y="14" width="20" height="7" rx="2"/><path d="M6 6.5h.01M6 17.5h.01"/></svg>',
  '产品管理':
    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 8 12 3 3 8v8l9 5 9-5V8z"/><path d="M3 8l9 5 9-5"/><path d="M12 13v8"/></svg>',
  '测试管理':
    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 3h6"/><path d="M10 3v6L5 19a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2L14 9V3"/></svg>',
  '报告管理':
    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6"/><path d="M8 13h8M8 17h8"/></svg>',
  '日志管理':
    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 6h13M8 12h13M8 18h13"/><path d="M3 6h.01M3 12h.01M3 18h.01"/></svg>',
  '系统设置':
    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z"/></svg>',
}
function iconOf(title: string): string {
  return ICONS[title] ?? ''
}
function toggleGroup(title: string): void {
  if (collapsedGroups.has(title)) collapsedGroups.delete(title)
  else collapsedGroups.add(title)
}

const currentTitle = computed(() => (route.meta.title as string) || '')
const displayName = computed(() => auth.user?.name || auth.user?.username || '未登录')

async function onLogout(): Promise<void> {
  await auth.signOut()
  await router.replace({ name: 'login' })
}
</script>

<style scoped>
.app-shell {
  display: flex;
  height: 100vh;
  overflow: hidden;
}

.sidebar {
  width: 220px;
  flex-shrink: 0;
  background: var(--ap-sidebar-bg);
  color: var(--ap-sidebar-text);
  display: flex;
  flex-direction: column;
}

.logo {
  padding: 16px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
}
.logo-brand {
  font-size: 16px;
  font-weight: 700;
  color: #fff;
  letter-spacing: 0.01em;
}
.logo-sub {
  font-size: 11.5px;
  color: #7b8794;
  margin-top: 2px;
}

.nav {
  flex: 1;
  overflow-y: auto;
  padding: 12px 0;
}

/* 一级模块：图标 + 文字 + 折叠箭头（对齐参考后台） */
.nav-group-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: calc(100% - 16px);
  margin: 2px 8px;
  padding: 10px 12px;
  font-size: 14px;
  font-weight: 500;
  color: #c9d1d9;
  background: none;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  font-family: inherit;
  text-align: left;
  transition: background 0.15s, color 0.15s;
}
.nav-group-title:hover {
  background: rgba(255, 255, 255, 0.06);
  color: #fff;
}
.nav-group-left {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}
.nav-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  color: #9ca3af;
  flex: none;
}
.nav-icon svg {
  width: 16px;
  height: 16px;
}
.caret {
  font-size: 11px;
  transition: transform 0.2s;
  color: #6b7280;
  flex: none;
}
.caret.collapsed {
  transform: rotate(-90deg);
}

/* 二级子项：纯缩进（对齐一级图标+文字），选中蓝色背景 */
.nav-items {
  display: flex;
  flex-direction: column;
  gap: 1px;
  margin: 2px 8px 4px;
}
.nav-item {
  display: block;
  /* 左侧 38px 缩进与一级图标+文字对齐；背景通栏（小 margin），对齐参考图选中项 */
  padding: 8px 10px 8px 38px;
  border-radius: 5px;
  font-size: 13px;
  color: #9ca3af;
  transition: background 0.15s, color 0.15s;
}
.nav-item.nav-single {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 4px 8px 6px;
  padding: 10px 12px;
  border-radius: 6px;
  font-weight: 500;
  color: #c9d1d9;
}
.nav-item.nav-single .nav-icon {
  color: #9ca3af;
}

.nav-item:hover {
  background: rgba(255, 255, 255, 0.06);
  color: #e5e7eb;
}

.nav-item.active {
  background: var(--ap-primary);
  color: #fff;
}

.nav-empty {
  padding: 16px;
  font-size: 13px;
  color: #6b7280;
}

.main {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 0;
}

.header {
  height: 52px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 20px;
  background: #fff;
  border-bottom: 1px solid #e5e7eb;
}

.breadcrumb {
  font-size: 14px;
  color: #374151;
}

.user {
  display: flex;
  align-items: center;
  gap: 14px;
  font-size: 14px;
  color: #6b7280;
}

.user-name {
  color: #374151;
}

.header-action {
  border: none;
  background: none;
  padding: 0;
  font-size: 14px;
  color: var(--ap-primary);
  cursor: pointer;
}

.logout {
  color: #b91c1c;
}

.content {
  flex: 1;
  overflow-y: auto;
  padding: 20px;
}
</style>
