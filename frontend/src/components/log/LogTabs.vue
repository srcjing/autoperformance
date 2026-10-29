<template>
  <div class="ap-card tab-bar">
    <RouterLink
      v-for="tab in visibleTabs"
      :key="tab.name"
      class="tab"
      :class="{ active: route.name === tab.name }"
      :to="{ name: tab.name }"
    >
      {{ tab.label }}
    </RouterLink>
    <span class="ap-muted tab-hint">
      日志为只读审计数据：无删除/修改入口；请求详情按任务采样限量，策略见「日志设置」
    </span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

// 日志管理多 Tab（用户/系统/测试/请求详情）：按子模块只读权限动态显隐。
interface LogTab {
  name: string
  label: string
  module: string
}

const TABS: LogTab[] = [
  { name: 'user-log', label: '用户日志', module: 'log.user' },
  { name: 'system-log', label: '系统日志', module: 'log.system' },
  { name: 'test-log', label: '测试日志', module: 'log.test' },
  { name: 'request-log', label: '请求详情', module: 'log.request' },
]

const route = useRoute()
const auth = useAuthStore()

const visibleTabs = computed(() => TABS.filter((tab) => auth.hasPerm(tab.module, 'read')))
</script>

<style scoped>
.tab-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.tab {
  padding: 6px 14px;
  border: 1px solid transparent;
  border-radius: 6px;
  background: #f3f4f6;
  color: #4b5563;
  font-size: 13px;
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
</style>
