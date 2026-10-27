<template>
  <div class="ap-page">
    <div class="ap-page-header">
      <div class="title-block">
        <h2 class="ap-page-title">用户日志</h2>
        <span class="ap-muted">
          管理员操作审计（5W1H）：操作人 / 时间 / IP / 动作 / 对象 / 结果。审计记录只读，HMAC 链式签名防篡改
        </span>
      </div>
      <button type="button" class="ap-btn ap-btn-ghost" :disabled="logs.loading" @click="reload">
        刷新
      </button>
    </div>

    <form class="ap-card ap-toolbar" @submit.prevent="search">
      <label class="ap-field">
        <span class="ap-field-label">关键字</span>
        <input v-model="filters.keyword" placeholder="详情检索（操作路径/状态码）" maxlength="128" />
      </label>
      <label class="ap-field">
        <span class="ap-field-label">动作</span>
        <select v-model="filters.action">
          <option value="">全部动作</option>
          <option v-for="item in AUDIT_ACTION_OPTIONS" :key="item.value" :value="item.value">
            {{ item.label }}
          </option>
        </select>
      </label>
      <label class="ap-field">
        <span class="ap-field-label">对象类型</span>
        <select v-model="filters.resourceType">
          <option value="">全部对象</option>
          <option v-for="item in AUDIT_RESOURCE_OPTIONS" :key="item.value" :value="item.value">
            {{ item.label }}
          </option>
        </select>
      </label>
      <label class="ap-field">
        <span class="ap-field-label">对象 ID</span>
        <input v-model="filters.resourceId" placeholder="如 12" maxlength="20" />
      </label>
      <label class="ap-field">
        <span class="ap-field-label">操作人 ID</span>
        <input v-model="filters.adminId" placeholder="如 7" maxlength="20" />
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

    <UserLogTable :logs="logs.list" :loading="logs.loading" />

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
import UserLogTable from '@/components/log/UserLogTable.vue'
import { useLogStore } from '@/stores/log'
import { dateToRfc3339 } from '@/utils/format'
import {
  AUDIT_ACTION_OPTIONS,
  AUDIT_RESOURCE_OPTIONS,
  describeLogError,
  LOG_LEVEL_OPTIONS,
} from '@/utils/log'
import type { LogLevel, UserLogQuery } from '@/types'

// 用户日志页（PRD §3.5）：按类型/时间/关键字/关联对象筛选 + 分页；只读，无删除/修改入口。
const store = useLogStore()
const logs = computed(() => store.userLogs)

const filters = reactive({
  keyword: '',
  action: '',
  resourceType: '',
  resourceId: '',
  adminId: '',
  level: '' as LogLevel | '',
  startDate: '',
  endDate: '',
})

const errorMessage = ref('')

const totalPages = computed(() =>
  Math.max(1, Math.ceil(logs.value.total / Math.max(logs.value.pageSize, 1))),
)

/** 筛选条件 → 请求参数（page 由调用方指定）。 */
function buildQuery(page: number): UserLogQuery {
  return {
    keyword: filters.keyword.trim() || undefined,
    action: filters.action || undefined,
    resource_type: filters.resourceType || undefined,
    resource_id: filters.resourceId.trim() || undefined,
    admin_id: filters.adminId.trim() || undefined,
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
    await store.fetchUserLogs(buildQuery(page))
  } catch (err) {
    // 25001 时间范围非法 / 10003 参数格式非法 / 10201 无权访问。
    errorMessage.value = describeLogError(err, '加载用户日志失败')
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
  filters.action = ''
  filters.resourceType = ''
  filters.resourceId = ''
  filters.adminId = ''
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
