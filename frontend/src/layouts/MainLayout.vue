<template>
  <div class="app-shell">
    <!-- 侧边栏：6 大模块导航 -->
    <aside class="sidebar">
      <div class="logo">auto_performance</div>
      <nav class="nav">
        <div v-for="group in navGroups" :key="group.title" class="nav-group">
          <div class="nav-group-title">{{ group.title }}</div>
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
      </nav>
    </aside>

    <div class="main">
      <!-- 顶栏 -->
      <header class="header">
        <div class="breadcrumb">{{ currentTitle }}</div>
        <div class="user">admin</div>
      </header>

      <!-- 内容区 -->
      <main class="content">
        <RouterView />
      </main>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'

interface NavItem {
  path: string
  label: string
}

interface NavGroup {
  title: string
  children: NavItem[]
}

// 侧边栏导航（对齐 frontend-structure.md §5：6 大模块）。
// 骨架阶段未做权限过滤；正式实现按「子模块 read 权限」动态过滤，敏感子模块需 write。
const navGroups: NavGroup[] = [
  {
    title: '环境管理',
    children: [
      { path: '/environments/list', label: '环境列表' },
      { path: '/environments/tags', label: '标签维护' },
    ],
  },
  {
    title: '产品管理',
    children: [
      { path: '/products/list', label: '产品列表' },
      { path: '/products/1/interfaces', label: '接口管理' },
    ],
  },
  {
    title: '测试管理',
    children: [
      { path: '/testing/templates', label: '预设模板' },
      { path: '/testing/tasks', label: '测试任务' },
    ],
  },
  {
    title: '报告管理',
    children: [{ path: '/reports/list', label: '报告列表' }],
  },
  {
    title: '日志管理',
    children: [
      { path: '/logs/user', label: '用户日志' },
      { path: '/logs/system', label: '系统日志' },
      { path: '/logs/test', label: '测试日志' },
    ],
  },
  {
    title: '系统设置',
    children: [
      { path: '/settings/admins', label: '管理员' },
      { path: '/settings/certificates', label: '证书' },
      { path: '/settings/ssh-hosts', label: 'SSH/Host' },
    ],
  },
]

const route = useRoute()
const currentTitle = computed(() => (route.meta.title as string) || '')
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

.nav-group-title {
  padding: 12px 16px 6px;
  font-size: 12px;
  text-transform: uppercase;
  letter-spacing: 0.5px;
  color: #6b7280;
}

.nav-item {
  display: block;
  padding: 9px 16px 9px 28px;
  font-size: 14px;
  color: #d1d5db;
  transition: background 0.15s;
}

.nav-item:hover {
  background: rgba(255, 255, 255, 0.06);
}

.nav-item.active {
  background: var(--ap-primary);
  color: #fff;
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
  font-size: 14px;
  color: #6b7280;
}

.content {
  flex: 1;
  overflow-y: auto;
  padding: 20px;
}
</style>
