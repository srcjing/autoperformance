<template>
  <div class="ap-page">
    <div class="ap-page-header">
      <div class="title-block">
        <h2 class="ap-page-title">系统日志</h2>
        <span class="ap-muted">
          服务运行状态记录：启动 / 重启 / 停止；上次进程未落 stop 记录时按「重启」告警
        </span>
      </div>
      <div class="ap-actions">
        <button type="button" class="ap-btn ap-btn-ghost" :disabled="logs.loading" @click="reload">
          刷新
        </button>
        <button type="button" class="ap-btn ap-btn-ghost" title="导出功能规划中" disabled>
          导出
        </button>
      </div>
    </div>

    <LogTabs />

    <form class="ap-card ap-toolbar" @submit.prevent="search">
      <label class="ap-field">
        <span class="ap-field-label">关键字</span>
        <input v-model="filters.keyword" placeholder="内容检索" maxlength="128" />
      </label>
      <label class="ap-field">
        <span class="ap-field-label">事件</span>
        <select v-model="filters.event">
          <option value="">全部事件</option>
          <option v-for="item in SYSTEM_EVENT_OPTIONS" :key="item.value" :value="item.value">
            {{ item.label }}
          </option>
        </select>
      </label>
      <label class="ap-field">
        <span class="ap-field-label">级别</span>
        <select v-model="filters.level">
          <option value="">全部级别</option>
          <option v-for="item in LOG_LEVEL_OPTIONS" :key="item.value" :value="item.value">
            {{ item.label }}
          </option>
        </select>
      </label>
      <label class="ap-field">
        <span class="ap-field-label">开始日期</span>
        <input v-model="filters.startDate" type="date" />
      </label>
      <label class="ap-field">
        <span class="ap-field-label">结束日期</span>
        <input v-model="filters.endDate" type="date" />
      </label>
      <div class="ap-actions">
        <button type="submit" class="ap-btn ap-btn-primary" :disabled="logs.loading">查询</button>
        <button type="button" class="ap-btn ap-btn-ghost" @click="resetFilters">重置</button>
      </div>
    </form>

    <p v-if="errorMessage" class="ap-alert ap-alert-error" role="alert">{{ errorMessage }}</p>

    <SystemLogTable :logs="logs.list" :loading="logs.loading" />

    <div class="ap-pagination">
      <span>共 {{ logs.total }} 条，第 {{ logs.page }} / {{ totalPages }} 页</span>
      <button
        type="button"
        class="ap-btn ap-btn-ghost ap-btn-sm"
        :disabled="logs.page <= 1 || logs.loading"
        @click="goPage(logs.page - 1)"
      >
        上一页
      </button>
      <button
        type="button"
        class="ap-btn ap-btn-ghost ap-btn-sm"
        :disabled="logs.page >= totalPages || logs.loading"
        @click="goPage(logs.page + 1)"
      >
        下一页
      </button>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import LogTabs from '@/components/log/LogTabs.vue'
import SystemLogTable from '@/components/log/SystemLogTable.vue'
import { useLogStore } from '@/stores/log'
import { dateToRfc3339 } from '@/utils/format'
import { describeLogError, LOG_LEVEL_OPTIONS, SYSTEM_EVENT_OPTIONS } from '@/utils/log'
import type { LogLevel, SystemLogQuery } from '@/types'

// 系统日志页（PRD §3.5）：按事件/级别/时间/关键字筛选 + 分页；只读。
const store = useLogStore()
const logs = computed(() => store.systemLogs)

const filters = reactive({
  keyword: '',
  event: '',
  level: '' as LogLevel | '',
  startDate: '',
  endDate: '',
})

const errorMessage = ref('')

const totalPages = computed(() =>
  Math.max(1, Math.ceil(logs.value.total / Math.max(logs.value.pageSize, 1))),
)

/** 筛选条件 → 请求参数（page 由调用方指定）。 */
function buildQuery(page: number): SystemLogQuery {
  return {
    keyword: filters.keyword.trim() || undefined,
    event: filters.event || undefined,
    level: filters.level || undefined,
    start_time: dateToRfc3339(filters.startDate),
    end_time: dateToRfc3339(filters.endDate, true),
    page,
    page_size: 20,
  }
}

async function load(page = 1): Promise<void> {
  errorMessage.value = ''
  try {
    await store.fetchSystemLogs(buildQuery(page))
  } catch (err) {
    errorMessage.value = describeLogError(err, '加载系统日志失败')
  }
}

function search(): void {
  void load(1)
}

function reload(): void {
  void load(logs.value.page)
}

function resetFilters(): void {
  filters.keyword = ''
  filters.event = ''
  filters.level = ''
  filters.startDate = ''
  filters.endDate = ''
  void load(1)
}

function goPage(page: number): void {
  void load(page)
}

onMounted(() => {
  void load(1)
})
</script>

<style scoped>
.title-block {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.title-block .ap-muted {
  font-size: 12px;
}
</style>
