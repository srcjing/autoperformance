<template>
  <div class="report-config">
    <div class="format-list">
      <label v-for="format in REPORT_FORMAT_OPTIONS" :key="format.value" class="check">
        <input
          type="checkbox"
          :checked="modelValue.formats.includes(format.value)"
          :disabled="disabled"
          @change="onToggle(format.value, $event)"
        />
        <span>{{ format.label }}</span>
      </label>
    </div>
    <p class="ap-muted">
      默认全量生成 html + pdf + word；取消勾选后该格式不生成。报告包含归一化指标、阈值达标结论与执行日志摘要。
    </p>
  </div>
</template>

<script setup lang="ts">
import { REPORT_FORMAT_OPTIONS } from '@/utils/testing'
import type { ReportConfig, ReportFormat } from '@/types'

// 报告输出配置（PRD §3.3.6 第 4 阶段 / 决策 #4）。
// 提交结构对齐后端 domain.ReportConfig：{ formats: ["html","pdf","word"] }。
const props = defineProps<{
  modelValue: ReportConfig
  disabled?: boolean
}>()

const emit = defineEmits<{ (e: 'update:modelValue', value: ReportConfig): void }>()

function onToggle(format: ReportFormat, event: Event): void {
  const checked = (event.target as HTMLInputElement).checked
  const formats = checked
    ? Array.from(new Set([...props.modelValue.formats, format]))
    : props.modelValue.formats.filter((item) => item !== format)
  emit('update:modelValue', { formats })
}
</script>

<style scoped>
.report-config {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.format-list {
  display: flex;
  flex-wrap: wrap;
  gap: 16px;
}

.check {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: #374151;
  cursor: pointer;
}
</style>
