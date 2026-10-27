<template>
  <div class="ap-page">
    <div class="ap-page-header">
      <div class="title-block">
        <h2 class="ap-page-title">测试日志</h2>
        <span class="ap-muted">
          任务执行过程日志：按任务定位，seq 为任务内增量序号；实时日志同时经 WebSocket 推送至任务详情
        </span>
      </div>
      <button type="button" class="ap-btn ap-btn-ghost" :disabled="logs.loading" @click="reload">
        刷新
      </button>
    </div>

    <form class="ap-card ap-toolbar" @submit.prevent="search">
      <label class="ap-field">
        <span class="ap-field-label">任务 ID</span>
        <input v-model="filters.taskId" placeholder="如 9（留空查全部）" maxlength="20" />
      </label>
      <label class="ap-field">
        <span class="ap-field-label">关键字</span>
        <input v-model="filters.keyword" placeholder="内容检索" maxlength="128" />
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

    <p class="ap-alert ap-alert-hint">
      {{ filters.taskId ? `当前按任务 #${filters.taskId} 过滤，按 seq 升序展示执行顺序` : '未指定任务：跨任务按时间倒序展示，指定任务后按 seq 升序' }}
    </p>

    <p v-if="errorMessage" class="ap-alert ap-alert-error" role="alert">{{ errorMessage }}</p>

    <TestLogTable :logs="logs.list" :loading="logs.loading" />

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
import { useRoute } from 'vue-router'
import TestLogTable from '@/components/log/TestLogTable.vue'
import { useLogStore } from '@/stores/log'
import { dateToRfc3339 } from '@/utils/format'
import { describeLogError, LOG_LEVEL_OPTIONS } from '@/utils/log'
import type { LogLevel, TestLogQuery } from '@/types'

// 测试日志页（PRD §3.5）：按任务定位 + 级别/关键字/时间筛选 + 分页；只读。
// 支持 ?task_id=<id> 直达（任务详情/报告页跳转场景）。
const store = useLogStore()
const route = useRoute()
const logs = computed(() => store.testLogs)

const filters = reactive({
  taskId: typeof route.query.task_id === 'string' ? route.query.task_id : '',
  keyword: '',
  level: '' as LogLevel | '',
  startDate: '',
  endDate: '',
})

const errorMessage = ref('')

const totalPages = computed(() =>
  Math.max(1, Math.ceil(logs.value.total / Math.max(logs.value.pageSize, 1))),
)

/** 筛选条件 → 请求参数（page 由调用方指定）。 */
function buildQuery(page: number): TestLogQuery {
  return {
    task_id: filters.taskId.trim() || undefined,
    keyword: filters.keyword.trim() || undefined,
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
    await store.fetchTestLogs(buildQuery(page))
  } catch (err) {
    // 25001 时间范围非法 / 10003 参数格式非法（含非法 task_id）。
    errorMessage.value = describeLogError(err, '加载测试日志失败')
  }
}

function search(): void {
  void load(1)
}

function reload(): void {
  void load(logs.value.page)
}

function resetFilters(): void {
  filters.taskId = ''
  filters.keyword = ''
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
