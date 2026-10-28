<template>
  <div class="app-shell">
    <!-- 侧边栏：按权限动态过滤的 6 大模块导航 -->
    <aside class="sidebar">
      <div class="logo">auto_performance</div>
      <nav class="nav">
        <RouterLink to="/overview" class="nav-item nav-single" active-class="active">概览</RouterLink>
        <div v-for="group in visibleNavGroups" :key="group.title" class="nav-group">
          <button type="button" class="nav-group-title" @click="toggleGroup(group.title)">
            <span>{{ group.title }}</span>
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
  padding: 18px 16px;
  font-size: 16px;
  font-weight: 700;
  color: #fff;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
}

.nav {
  flex: 1;
  overflow-y: auto;
  padding: 12px 0;
}

/* 一级模块：独立分组标题（加粗、亮色、分组上下间距） */
.nav-group-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  width: calc(100% - 16px);
  margin: 8px 8px 2px;
  padding: 9px 12px;
  font-size: 14px;
  font-weight: 700;
  letter-spacing: 0.02em;
  color: #f9fafb;
  background: none;
  border: none;
  border-radius: 6px;
  cursor: pointer;
  font-family: inherit;
  text-align: left;
  transition: background 0.15s;
}
.nav-group-title:hover {
  background: rgba(255, 255, 255, 0.08);
}
.nav-group:first-of-type .nav-group-title {
  margin-top: 4px;
}
.caret {
  font-size: 11px;
  transition: transform 0.2s;
  color: #9ca3af;
}
.caret.collapsed {
  transform: rotate(-90deg);
}
.nav-single {
  margin: 4px 8px 8px;
  padding: 9px 12px;
  border-radius: 6px;
  font-weight: 700;
  color: #f9fafb;
}

/* 二级子项：左侧树形竖线 + 缩进 + 更小更暗，与一级明显区分 */
.nav-items {
  display: flex;
  flex-direction: column;
  gap: 1px;
  margin: 2px 8px 6px 22px;
  padding-left: 8px;
  border-left: 1px solid rgba(255, 255, 255, 0.12);
}
.nav-item {
  display: block;
  padding: 7px 10px;
  border-radius: 5px;
  font-size: 13px;
  color: #9ca3af;
  transition: background 0.15s, color 0.15s;
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
