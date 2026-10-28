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
      <!-- 顶栏：面包屑 + 右侧信息区（消息图标 + 用户名下拉） -->
      <header class="header">
        <div class="breadcrumb">{{ currentTitle }}</div>
        <div ref="headerRight" class="header-right">
          <!-- 消息（占位：暂未接入站内通知） -->
          <div class="dropdown">
            <button
              type="button"
              class="icon-btn"
              :class="{ active: openMenu === 'msg' }"
              title="消息"
              @click.stop="toggleMenu('msg')"
            >
              <span class="nav-icon" v-html="ICONS.bell"></span>
              <span class="dot"></span>
            </button>
            <div v-if="openMenu === 'msg'" class="menu">
              <p class="menu-empty">暂无新消息（站内通知后续接入）</p>
            </div>
          </div>

          <!-- 用户下拉：账号信息 / 修改密码 / 退出登录 -->
          <div class="dropdown">
            <button
              type="button"
              class="user-trigger"
              :class="{ active: openMenu === 'user' }"
              @click.stop="toggleMenu('user')"
            >
              <span class="avatar">{{ avatarText }}</span>
              <span class="user-name">{{ displayName }}</span>
              <span class="trigger-caret" :class="{ open: openMenu === 'user' }">▾</span>
            </button>
            <div v-if="openMenu === 'user'" class="menu menu-user">
              <div class="menu-head">
                <div class="menu-name">{{ displayName }}</div>
                <div class="menu-sub">{{ auth.user?.username || '-' }}</div>
              </div>
              <RouterLink class="menu-item" to="/change-password" @click="closeMenu">
                修改密码
              </RouterLink>
              <button type="button" class="menu-item danger" @click="onLogout">退出登录</button>
            </div>
          </div>
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
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
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
      { path: '/logs/request', label: '请求详情', module: 'log.request' },
      { path: '/logs/settings', label: '日志设置', module: 'setting.log' },
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
  bell:
    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M18 8a6 6 0 1 0-12 0c0 7-3 8-3 8h18s-3-1-3-8"/><path d="M13.7 21a2 2 0 0 1-3.4 0"/></svg>',
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
const avatarText = computed(() => displayName.value.slice(0, 1).toUpperCase())

// 右上角下拉（消息 / 用户）：同刻仅展开一个，点击外部区域关闭。
const headerRight = ref<HTMLElement | null>(null)
const openMenu = ref<'' | 'msg' | 'user'>('')

function toggleMenu(menu: 'msg' | 'user'): void {
  openMenu.value = openMenu.value === menu ? '' : menu
}

function closeMenu(): void {
  openMenu.value = ''
}

function onDocumentClick(event: MouseEvent): void {
  if (!headerRight.value) return
  if (!headerRight.value.contains(event.target as Node)) closeMenu()
}

onMounted(() => document.addEventListener('click', onDocumentClick))
onBeforeUnmount(() => document.removeEventListener('click', onDocumentClick))

async function onLogout(): Promise<void> {
  closeMenu()
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

/* 右侧信息区：消息图标 + 用户名下拉（对齐参考后台） */
.header-right {
  display: flex;
  align-items: center;
  gap: 8px;
}

.dropdown {
  position: relative;
}

.icon-btn {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border: none;
  border-radius: 6px;
  background: none;
  color: #6b7280;
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}
.icon-btn:hover,
.icon-btn.active {
  background: #f3f4f6;
  color: var(--ap-primary);
}
.icon-btn .nav-icon,
.icon-btn .nav-icon svg {
  width: 17px;
  height: 17px;
}
.icon-btn .dot {
  position: absolute;
  top: 6px;
  right: 7px;
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #ef4444;
  border: 1px solid #fff;
}

.user-trigger {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 4px 8px 4px 4px;
  border: none;
  border-radius: 999px;
  background: none;
  cursor: pointer;
  font-size: 14px;
  color: #374151;
  font-family: inherit;
}
.user-trigger:hover,
.user-trigger.active {
  background: #f3f4f6;
}

.avatar {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 26px;
  height: 26px;
  border-radius: 50%;
  background: var(--ap-primary);
  color: #fff;
  font-size: 12px;
  font-weight: 600;
}

.user-name {
  color: #374151;
  max-width: 140px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.trigger-caret {
  font-size: 11px;
  color: #9ca3af;
  transition: transform 0.2s;
}
.trigger-caret.open {
  transform: rotate(180deg);
}

.menu {
  position: absolute;
  top: calc(100% + 6px);
  right: 0;
  z-index: 30;
  min-width: 180px;
  padding: 6px;
  background: #fff;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  box-shadow: 0 8px 24px rgba(15, 20, 36, 0.12);
}

.menu-user {
  min-width: 200px;
}

.menu-head {
  padding: 8px 10px;
  border-bottom: 1px solid #f1f2f4;
  margin-bottom: 4px;
}
.menu-name {
  font-size: 13px;
  color: #1f2933;
  font-weight: 600;
}
.menu-sub {
  font-size: 12px;
  color: #6b7280;
}

.menu-item {
  display: block;
  width: 100%;
  padding: 8px 10px;
  border: none;
  border-radius: 6px;
  background: none;
  text-align: left;
  font-size: 13px;
  color: #374151;
  font-family: inherit;
  cursor: pointer;
}
.menu-item:hover {
  background: #f3f4f6;
  color: var(--ap-primary);
}
.menu-item.danger {
  color: #b91c1c;
}
.menu-item.danger:hover {
  background: #fef2f2;
  color: #b91c1c;
}

.menu-empty {
  margin: 0;
  padding: 10px;
  font-size: 13px;
  color: #6b7280;
  white-space: nowrap;
}

.content {
  flex: 1;
  overflow-y: auto;
  padding: 20px;
}
</style>
