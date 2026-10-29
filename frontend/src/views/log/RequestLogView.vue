<template>
  <div class="ap-page">
    <div class="ap-page-header">
      <div class="title-block">
        <h2 class="ap-page-title">请求详情</h2>
        <span class="ap-muted">
          压测时对被测接口的逐请求明细：方法 / URL / 入参 / 响应状态 / 响应摘要 / 错误码 / 耗时；
          数据量大，按任务采样限量（策略见「日志设置」）
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
        <span class="ap-field-label">任务 ID</span>
        <input v-model="filters.taskId" placeholder="如 9（留空查全部）" maxlength="20" />
      </label>
      <label class="ap-field">
        <span class="ap-field-label">接口 ID</span>
        <input v-model="filters.interfaceId" placeholder="如 11（留空查全部）" maxlength="20" />
      </label>
      <label class="ap-field">
        <span class="ap-field-label">关键字</span>
        <input v-model="filters.keyword" placeholder="URL 或错误信息检索" maxlength="128" />
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
      {{
        filters.taskId
          ? `当前按任务 #${filters.taskId} 过滤，按请求发生顺序展示`
          : '未指定任务：跨任务按发生时间倒序展示，指定任务后按请求顺序展示'
      }}
    </p>

    <p v-if="errorMessage" class="ap-alert ap-alert-error" role="alert">{{ errorMessage }}</p>

    <RequestLogTable :logs="logs.list" :loading="logs.loading" />

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
import LogTabs from '@/components/log/LogTabs.vue'
import RequestLogTable from '@/components/log/RequestLogTable.vue'
import { useLogStore } from '@/stores/log'
import { dateToRfc3339 } from '@/utils/format'
import { describeLogError } from '@/utils/log'
import type { RequestLogQuery } from '@/types'

// 请求详情页（PRD §3.5）：按任务/接口定位 + 关键字/时间筛选 + 分页；只读。
// 支持 ?task_id=<id> 直达（任务详情/报告页跳转场景）。
const store = useLogStore()
const route = useRoute()
const logs = computed(() => store.requestLogs)

const filters = reactive({
  taskId: typeof route.query.task_id === 'string' ? route.query.task_id : '',
  interfaceId: typeof route.query.interface_id === 'string' ? route.query.interface_id : '',
  keyword: '',
  startDate: '',
  endDate: '',
})

const errorMessage = ref('')

const totalPages = computed(() =>
  Math.max(1, Math.ceil(logs.value.total / Math.max(logs.value.pageSize, 1))),
)

/** 筛选条件 → 请求参数（page 由调用方指定）。 */
function buildQuery(page: number): RequestLogQuery {
  return {
    task_id: filters.taskId.trim() || undefined,
    interface_id: filters.interfaceId.trim() || undefined,
    keyword: filters.keyword.trim() || undefined,
    start_time: dateToRfc3339(filters.startDate),
    end_time: dateToRfc3339(filters.endDate, true),
    page,
    page_size: 20,
  }
}

async function load(page = 1): Promise<void> {
  errorMessage.value = ''
  try {
    await store.fetchRequestLogs(buildQuery(page))
  } catch (err) {
    // 25001 时间范围非法 / 10003 参数格式非法（含非法 task_id、interface_id）。
    errorMessage.value = describeLogError(err, '加载请求详情失败')
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
  filters.interfaceId = ''
  filters.keyword = ''
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
