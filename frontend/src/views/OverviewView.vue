<template>
  <div class="overview">
    <div class="ov-head">
      <div>
        <h2 class="ov-title">概览</h2>
        <p class="ap-muted">环境、产品、测试任务与报告的运行态势</p>
      </div>
    </div>

    <div class="ov-stats">
      <RouterLink v-for="s in stats" :key="s.label" :to="s.to" class="ov-stat">
        <div class="ov-stat-label">{{ s.label }}</div>
        <div class="ov-stat-value">{{ s.value }}</div>
        <div class="ov-stat-hint">{{ s.hint }}</div>
      </RouterLink>
    </div>

    <div class="ov-card">
      <div class="ov-card-head">
        <span class="ov-card-title">最近测试任务</span>
        <RouterLink to="/testing/tasks" class="ap-link">查看全部 →</RouterLink>
      </div>
      <table class="ap-table">
        <thead>
          <tr><th>任务名称</th><th>状态</th><th>更新时间</th></tr>
        </thead>
        <tbody>
          <tr v-for="t in recentTasks" :key="t.id">
            <td>
              <RouterLink :to="`/testing/tasks/${t.id}`" class="ap-link">{{ t.name }}</RouterLink>
            </td>
            <td><span class="ov-badge" :class="statusClass(t.status)">{{ statusLabel(t.status) }}</span></td>
            <td class="ap-muted">{{ t.created_at ?? '-' }}</td>
          </tr>
          <tr v-if="recentTasks.length === 0">
            <td colspan="3" class="ap-muted" style="text-align:center;padding:24px">暂无任务</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div class="ov-actions">
      <RouterLink to="/testing/tasks" class="ap-btn ap-btn-primary">去创建任务</RouterLink>
      <RouterLink to="/reports/list" class="ap-btn ap-btn-ghost">查看报告</RouterLink>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { listEnvironments } from '@/api/environment'
import { listProducts } from '@/api/product'
import { listTasks } from '@/api/testing'
import type { TestTaskInfo } from '@/types'

interface StatItem {
  label: string
  value: string
  hint: string
  to: string
}

const stats = ref<StatItem[]>([
  { label: '待测环境', value: '-', hint: '环境管理', to: '/environments/list' },
  { label: '在册产品', value: '-', hint: '产品管理', to: '/products/list' },
  { label: '在册接口', value: '-', hint: '接口管理', to: '/products/interfaces' },
  { label: '运行中任务', value: '-', hint: '测试任务', to: '/testing/tasks' },
])
const recentTasks = ref<TestTaskInfo[]>([])

const STATUS_MAP: Record<string, string> = {
  pending: '待执行', running: '执行中', collecting: '采集中', evaluating: '评估中',
  generating: '报告生成中', finished: '已完成', failed: '失败', cancelled: '已取消',
  cleanup_pending: '清理待确认', cleaning: '清理中', ended: '已结束',
}
function statusLabel(s: string): string {
  return STATUS_MAP[s] ?? s
}
function statusClass(s: string): string {
  if (s === 'finished') return 'ok'
  if (s === 'failed' || s === 'cancelled') return 'bad'
  if (s === 'running' || s === 'collecting' || s === 'evaluating' || s === 'generating') return 'run'
  return 'neutral'
}

// 各统计独立容错：无权限/请求失败时保持 '-'，不影响整页。
async function load(): Promise<void> {
  const tasks: Array<Promise<void>> = [
    listEnvironments({ page: 1, page_size: 1 })
      .then((r) => { stats.value[0].value = String(r.total) })
      .catch(() => {}),
    listProducts({ page: 1, page_size: 200 })
      .then((r) => {
        stats.value[1].value = String(r.total)
        const ifaces = r.list.reduce((sum, p) => sum + (p.interface_count ?? 0), 0)
        stats.value[2].value = String(ifaces)
      })
      .catch(() => {}),
    listTasks({ status: 'running', page: 1, page_size: 1 })
      .then((r) => { stats.value[3].value = String(r.total) })
      .catch(() => {}),
    listTasks({ page: 1, page_size: 5 })
      .then((r) => { recentTasks.value = r.list })
      .catch(() => {}),
  ]
  await Promise.allSettled(tasks)
}

onMounted(() => { void load() })
</script>

<style scoped>
.ov-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-end;
  margin-bottom: 18px;
}
.ov-title {
  font-size: 20px;
  font-weight: 700;
  margin: 0 0 4px;
}
.ov-stats {
  display: grid;
  grid-template-columns: repeat(4, 1fr);
  gap: 16px;
  margin-bottom: 16px;
}
@media (max-width: 1100px) {
  .ov-stats { grid-template-columns: repeat(2, 1fr); }
}
.ov-stat {
  display: block;
  background: #fff;
  border: 1px solid #e5e7eb;
  border-radius: 12px;
  padding: 16px 18px;
  text-decoration: none;
  color: inherit;
  transition: border-color 0.15s, box-shadow 0.15s;
}
.ov-stat:hover {
  border-color: var(--ap-primary);
  box-shadow: 0 4px 16px -8px rgba(0, 0, 0, 0.15);
}
.ov-stat-label {
  font-size: 12.5px;
  color: #6b7280;
}
.ov-stat-value {
  font-size: 26px;
  font-weight: 700;
  margin-top: 8px;
  font-variant-numeric: tabular-nums;
}
.ov-stat-hint {
  font-size: 12px;
  color: #9ca3af;
  margin-top: 4px;
}
.ov-card {
  background: #fff;
  border: 1px solid #e5e7eb;
  border-radius: 12px;
  overflow: hidden;
}
.ov-card-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 14px 18px;
  border-bottom: 1px solid #e5e7eb;
}
.ov-card-title {
  font-weight: 600;
}
.ov-badge {
  display: inline-block;
  padding: 2px 9px;
  border-radius: 99px;
  font-size: 12px;
  font-weight: 600;
}
.ov-badge.ok { background: #ecfdf5; color: #047857; }
.ov-badge.bad { background: #fff1f2; color: #e11d48; }
.ov-badge.run { background: #eff6ff; color: #2563eb; }
.ov-badge.neutral { background: #f1f5f9; color: #64748b; }
.ov-actions {
  display: flex;
  gap: 10px;
  margin-top: 16px;
}
</style>
