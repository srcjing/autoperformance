<template>
  <div class="ap-page">
    <div class="ap-page-header">
      <div class="title-block">
        <h2 class="ap-page-title">日志设置</h2>
        <span class="ap-muted">
          日志服务开关、最大条数、存储时间范围与启用类型；设置生效于日志写入与过期清理
        </span>
      </div>
      <div class="ap-actions">
        <button type="button" class="ap-btn ap-btn-ghost" :disabled="loading" @click="load">
          刷新
        </button>
        <button
          v-if="canWrite"
          type="button"
          class="ap-btn ap-btn-primary"
          :disabled="loading || saving"
          @click="save"
        >
          {{ saving ? '保存中…' : '保存设置' }}
        </button>
      </div>
    </div>

    <p v-if="errorMessage" class="ap-alert ap-alert-error" role="alert">{{ errorMessage }}</p>
    <p v-if="notice" class="ap-alert ap-alert-hint" role="status">{{ notice }}</p>

    <form class="ap-card settings" @submit.prevent="save">
      <section class="row">
        <div class="row-label">
          <span class="row-title">日志服务</span>
          <span class="ap-muted">
            关闭后停止写入请求详情日志（历史数据保留、仍可查询）；审计/系统/测试日志为运行与取证必需，
            不受此开关控制
          </span>
        </div>
        <div class="row-control">
          <label class="radio">
            <input v-model="form.enabled" type="radio" :value="true" :disabled="!canWrite" /> 开启
          </label>
          <label class="radio">
            <input v-model="form.enabled" type="radio" :value="false" :disabled="!canWrite" /> 关闭
          </label>
          <span class="ap-badge" :class="form.enabled ? 'ap-badge-success' : 'ap-badge-muted'">
            {{ form.enabled ? '运行中' : '已关闭' }}
          </span>
        </div>
      </section>

      <section class="row">
        <div class="row-label">
          <span class="row-title">最大日志条数</span>
          <span class="ap-muted">超过后保留最新记录、清理最旧记录（1000 ~ 10000000）</span>
        </div>
        <div class="row-control">
          <input
            v-model.number="form.maxCount"
            type="number"
            min="1000"
            max="10000000"
            step="1000"
            :disabled="!canWrite"
          />
          <span class="ap-muted">条</span>
        </div>
      </section>

      <section class="row">
        <div class="row-label">
          <span class="row-title">存储时间范围</span>
          <span class="ap-muted">超过保留天数的日志在清理时删除（1 ~ 3650 天）</span>
        </div>
        <div class="row-control">
          <input
            v-model.number="form.retentionDays"
            type="number"
            min="1"
            max="3650"
            :disabled="!canWrite"
          />
          <span class="ap-muted">天</span>
        </div>
      </section>

      <section class="row">
        <div class="row-label">
          <span class="row-title">日志类型</span>
          <span class="ap-muted">
            勾选类型决定日志写入范围（当前生效于请求详情）；请求详情数据量大，按任务采样限量
            （默认每任务 1000 条），保留条数/天数由上方策略控制
          </span>
        </div>
        <div class="row-control types">
          <label v-for="item in LOG_TYPE_OPTIONS" :key="item.value" class="checkbox">
            <input
              v-model="form.logTypes"
              type="checkbox"
              :value="item.value"
              :disabled="!canWrite"
            />
            {{ item.label }}
          </label>
        </div>
      </section>

      <p class="ap-muted foot">
        最近更新：{{ setting?.updated_at ? formatDateTime(setting.updated_at) : '-' }}
        <template v-if="!canWrite"> · 当前账号无保存权限（setting.log:write）</template>
      </p>
    </form>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { usePermission } from '@/composables/usePermission'
import { useLogStore } from '@/stores/log'
import { formatDateTime } from '@/utils/format'
import { describeLogError, LOG_TYPE_OPTIONS } from '@/utils/log'
import type { LogType } from '@/types'

// 日志设置页（PRD §3.5 日志设置）：
//   - 日志服务开关 enabled / 最大条数 max_count / 存储时间范围 retention_days / 类型勾选 log_types；
//   - 保存后服务端立即按新策略清理一次（删过期 + 超量），因此表单与后端边界保持一致校验。
const MAX_COUNT_MIN = 1000
const MAX_COUNT_MAX = 10_000_000
const RETENTION_MIN = 1
const RETENTION_MAX = 3650

const store = useLogStore()
const { hasPerm } = usePermission()
// 保存需 setting.log:write；查看需 setting.log:read（路由已校验）。
const canWrite = computed(() => hasPerm('setting.log', 'write'))

const setting = computed(() => store.setting)
const loading = computed(() => store.settingLoading)
const saving = computed(() => store.settingSaving)

const form = reactive({
  enabled: true,
  maxCount: 100000,
  retentionDays: 30,
  logTypes: ['user', 'system', 'test', 'request'] as LogType[],
})

const errorMessage = ref('')
const notice = ref('')

/** 用服务端设置回填表单（保留已勾选类型的顺序与未知项过滤）。 */
function applySetting(): void {
  const current = store.setting
  if (!current) return
  form.enabled = current.enabled
  form.maxCount = current.max_count
  form.retentionDays = current.retention_days
  form.logTypes = [...current.log_types]
}

async function load(): Promise<void> {
  errorMessage.value = ''
  try {
    await store.fetchSetting()
    applySetting()
  } catch (err) {
    errorMessage.value = describeLogError(err, '加载日志设置失败')
  }
}

/** 前端预校验（与后端 domain.LogSetting.Validate 一致，避免无谓请求）。 */
function validate(): string {
  if (!Number.isInteger(form.maxCount) || form.maxCount < MAX_COUNT_MIN || form.maxCount > MAX_COUNT_MAX) {
    return `最大日志条数需为 ${MAX_COUNT_MIN} ~ ${MAX_COUNT_MAX} 之间的整数`
  }
  if (
    !Number.isInteger(form.retentionDays) ||
    form.retentionDays < RETENTION_MIN ||
    form.retentionDays > RETENTION_MAX
  ) {
    return `存储时间范围需为 ${RETENTION_MIN} ~ ${RETENTION_MAX} 天之间的整数`
  }
  if (form.enabled && form.logTypes.length === 0) {
    return '开启日志服务时至少勾选一种日志类型'
  }
  return ''
}

async function save(): Promise<void> {
  if (!canWrite.value) return
  errorMessage.value = ''
  notice.value = ''
  const invalid = validate()
  if (invalid) {
    errorMessage.value = invalid
    return
  }
  try {
    const saved = await store.saveSetting({
      enabled: form.enabled,
      max_count: form.maxCount,
      retention_days: form.retentionDays,
      log_types: [...form.logTypes],
    })
    applySetting()
    notice.value = `设置已保存（${formatDateTime(saved.updated_at ?? '')}），服务端已按新策略触发一次日志清理`
  } catch (err) {
    errorMessage.value = describeLogError(err, '保存日志设置失败')
  }
}

onMounted(() => {
  void load()
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

.settings {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.row {
  display: flex;
  gap: 24px;
  padding: 16px 0;
  border-bottom: 1px solid #f1f2f4;
}

.row:last-of-type {
  border-bottom: none;
}

.row-label {
  display: flex;
  flex-direction: column;
  gap: 4px;
  width: 320px;
  flex-shrink: 0;
}

.row-title {
  font-size: 14px;
  color: #1f2933;
  font-weight: 500;
}

.row-label .ap-muted {
  font-size: 12px;
  line-height: 1.6;
}

.row-control {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  font-size: 13px;
  color: #374151;
}

.row-control input[type='number'] {
  width: 160px;
  padding: 7px 9px;
  border: 1px solid #d1d5db;
  border-radius: 6px;
  font-size: 13px;
  font-family: inherit;
}

.radio,
.checkbox {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: #374151;
  cursor: pointer;
}

.types {
  flex-direction: column;
  align-items: flex-start;
  gap: 8px;
}

.foot {
  margin: 8px 0 0;
  font-size: 12px;
}
</style>
