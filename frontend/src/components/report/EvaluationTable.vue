<template>
  <div class="ap-card">
    <div class="table-head">
      <h3 class="cfg-title">阈值快照判定（逐条）</h3>
      <span class="ap-muted">
        共 {{ items.length }} 条 · 达标 {{ passedCount }} · 不达标 {{ failedCount }}
      </span>
    </div>

    <table v-if="items.length > 0" class="ap-table">
      <thead>
        <tr>
          <th>接口</th>
          <th>指标</th>
          <th>条件</th>
          <th>阈值</th>
          <th>实测</th>
          <th>结论</th>
          <th>说明</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(item, index) in items" :key="`${item.interface_id}-${item.metric}-${index}`" :class="{ 'row-fail': !item.pass }">
          <td>{{ item.interface_name || `接口 #${item.interface_id}` }}</td>
          <td>{{ reportMetricLabel(item.metric) }}</td>
          <td>{{ item.operator }}</td>
          <td>{{ item.threshold }}</td>
          <td>{{ item.has_actual ? item.actual : '-' }}</td>
          <td>
            <span class="ap-badge" :class="item.pass ? 'ap-badge-success' : 'ap-badge-danger'">
              {{ item.pass ? '达标' : '不达标' }}
            </span>
          </td>
          <td>
            <span v-if="item.reason" class="reason">{{ item.reason }}</span>
            <span v-else class="ap-muted">-</span>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-else class="ap-table-empty">无阈值判定记录（任务未配置启用阈值或报告为外部导入）</p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { reportMetricLabel } from '@/utils/report'
import type { ReportSummaryItem } from '@/types'

// 报告达标明细表（不达标行高亮，与 HTML/Word 报告口径一致）。
const props = defineProps<{
  items: ReportSummaryItem[]
}>()

const passedCount = computed(() => props.items.filter((item) => item.pass).length)
const failedCount = computed(() => props.items.length - passedCount.value)
</script>

<style scoped>
.table-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 10px;
}

.cfg-title {
  margin: 0;
  font-size: 14px;
  color: #1f2933;
}

.row-fail {
  background: #fef2f2;
}

.reason {
  color: #b91c1c;
  font-size: 12px;
}
</style>
