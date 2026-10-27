<template>
  <div class="ap-chips">
    <button
      v-for="tag in tags"
      :key="tag.id"
      type="button"
      class="ap-chip"
      :class="{ active: isActive(tag.id) }"
      :disabled="disabled"
      @click="toggle(tag.id)"
    >
      {{ tag.name }}
    </button>
    <span v-if="tags.length === 0" class="ap-muted">暂无可选标签，请先在「标签维护」中创建</span>
  </div>
</template>

<script setup lang="ts">
import type { TagInfo } from '@/types'

// 标签多选（环境侧打标签，PRD §3.1.1「标签反向维护」）。
// 只允许选择预定义标签（不可自由输入），保证标签 ↔ 环境双向关联一致。
const props = defineProps<{
  modelValue: string[]
  tags: TagInfo[]
  disabled?: boolean
}>()

const emit = defineEmits<{ (e: 'update:modelValue', value: string[]): void }>()

function isActive(id: string): boolean {
  return props.modelValue.includes(id)
}

function toggle(id: string): void {
  if (props.disabled) return
  const next = isActive(id)
    ? props.modelValue.filter((item) => item !== id)
    : [...props.modelValue, id]
  emit('update:modelValue', next)
}
</script>
