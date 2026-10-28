<template>
  <div class="ap-card">
    <table class="ap-table">
      <thead>
        <tr>
          <th>模板名称</th>
          <th>默认环境</th>
          <th>默认接口</th>
          <th>并发模式</th>
          <th>报告格式</th>
          <th>清理动作</th>
          <th>更新时间</th>
          <th>操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="template in templates" :key="template.id">
          <td>
            <div class="tpl-name">{{ template.name }}</div>
            <div v-if="template.description" class="ap-muted tpl-desc">
              {{ template.description }}
            </div>
          </td>
          <td>{{ template.env_ids.length }} 个</td>
          <td>{{ template.interface_ids.length }} 个</td>
          <td>{{ modeLabel(template.flow?.concurrency_mode) }}</td>
          <td>{{ formatLabel(template.report_config) }}</td>
          <td>{{ cleanSummary(template.clean_config) }}</td>
          <td>{{ formatDateTime(template.updated_at ?? '') }}</td>
          <td>
            <div class="ap-actions">
              <button
                v-if="canCreateTask"
                type="button"
                class="ap-btn ap-btn-primary ap-btn-sm"
                @click="emit('create-task', template)"
              >
                创建任务
              </button>
              <template v-if="canWrite">
                <button
                  type="button"
                  class="ap-btn ap-btn-ghost ap-btn-sm"
                  @click="emit('edit', template)"
                >
                  编辑
                </button>
                <button
                  type="button"
                  class="ap-btn ap-btn-danger ap-btn-sm"
                  @click="emit('remove', template)"
                >
                  删除
                </button>
              </template>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-if="!loading && templates.length === 0" class="ap-table-empty">
      暂无模板，点击「新建模板」保存常用配置
    </p>
    <p v-if="loading" class="ap-table-empty">加载中…</p>
  </div>
</template>

<script setup lang="ts">
import { formatDateTime } from '@/utils/format'
import { CLEAN_ACTION_OPTIONS, CONCURRENCY_MODE_OPTIONS } from '@/utils/testing'
import type { CleanConfig, ReportConfig, TestTemplateInfo } from '@/types'

// 模板表格（展示用，操作通过事件上抛，便于列表页统一处理提示与刷新）。
defineProps<{
  templates: TestTemplateInfo[]
  loading: boolean
  canWrite: boolean
  canCreateTask: boolean
}>()

const emit = defineEmits<{
  (e: 'edit', template: TestTemplateInfo): void
  (e: 'remove', template: TestTemplateInfo): void
  (e: 'create-task', template: TestTemplateInfo): void
}>()

/** 并发模式展示名（历史数据缺省时按固定并发）。 */
function modeLabel(mode?: string): string {
  return CONCURRENCY_MODE_OPTIONS.find((item) => item.value === mode)?.label ?? '固定并发'
}

/** 报告格式（未配置按默认全量）。 */
function formatLabel(config: ReportConfig | null): string {
  if (!config?.formats || config.formats.length === 0) return '全量'
  return config.formats.map((format) => format.toUpperCase()).join(' / ')
}

/** 清理动作摘要（内置动作 + 脚本数量）。 */
function cleanSummary(config: CleanConfig | null): string {
  if (!config) return '-'
  const labels = (config.actions ?? [])
    .filter((action) => action !== 'script')
    .map((action) => CLEAN_ACTION_OPTIONS.find((item) => item.value === action)?.label ?? action)
  const scriptCount = config.scripts?.length ?? 0
  if (scriptCount > 0) labels.push(`脚本 ×${scriptCount}`)
  return labels.join('、') || '-'
}
</script>

<style scoped>
.tpl-name {
  font-weight: 600;
  color: #1f2933;
}

.tpl-desc {
  max-width: 260px;
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
