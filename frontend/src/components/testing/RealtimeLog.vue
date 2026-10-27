<template>
  <div class="ap-card log-panel">
    <div class="log-head">
      <div class="log-title">
        实时日志
        <span class="ap-badge" :class="connected ? 'ap-badge-success' : 'ap-badge-muted'">
          {{ connected ? '实时通道已连接' : '实时通道未连接' }}
        </span>
      </div>
      <div class="log-tools">
        <select v-model="level" class="log-select">
          <option value="all">全部级别</option>
          <option value="info">INFO</option>
          <option value="warn">WARN</option>
          <option value="error">ERROR</option>
        </select>
        <input v-model="keyword" class="log-search" placeholder="关键字过滤" maxlength="64" />
        <label class="check">
          <input v-model="autoScroll" type="checkbox" />
          <span>自动滚动</span>
        </label>
        <button
          type="button"
          class="ap-btn ap-btn-ghost ap-btn-sm"
          :disabled="logs.length === 0"
          @click="emit('clear')"
        >
          清空
        </button>
      </div>
    </div>

    <div ref="body" class="log-body">
      <p v-if="filtered.length === 0" class="log-empty">{{ hint }}</p>
      <div
        v-for="(line, index) in filtered"
        :key="`${line.seq}-${index}`"
        class="log-line"
        :class="`level-${levelClass(line.level)}`"
      >
        <span class="log-time">{{ timeOf(line.time) }}</span>
        <span class="log-level">{{ levelText(line.level) }}</span>
        <span class="log-msg">{{ line.message }}</span>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import type { TaskLogEvent } from '@/types'

// 实时日志面板（task:{id} → event=log / logs:test）。
// 日志已在服务端脱敏（DES-I-001），前端仅做级别着色与本地过滤，不回写、不改写内容。
const props = defineProps<{
  logs: TaskLogEvent[]
  connected: boolean
  hint?: string
}>()

const emit = defineEmits<{ (e: 'clear'): void }>()

const level = ref<'all' | 'info' | 'warn' | 'error'>('all')
const keyword = ref('')
const autoScroll = ref(true)
const body = ref<HTMLElement | null>(null)

const filtered = computed(() => {
  const text = keyword.value.trim().toLowerCase()
  return props.logs.filter((line) => {
    if (level.value !== 'all' && levelClass(line.level) !== level.value) return false
    if (!text) return true
    return line.message.toLowerCase().includes(text)
  })
})

/** 归一化日志级别（未知级别按 info 着色）。 */
function levelClass(value: string): 'info' | 'warn' | 'error' {
  const text = (value || '').toLowerCase()
  if (text === 'warn' || text === 'warning') return 'warn'
  if (text === 'error' || text === 'fatal') return 'error'
  return 'info'
}

function levelText(value: string): string {
  return levelClass(value).toUpperCase()
}

function timeOf(value: string): string {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleTimeString()
}

async function scrollToBottom(): Promise<void> {
  await nextTick()
  const element = body.value
  if (element) element.scrollTop = element.scrollHeight
}

watch(
  () => filtered.value.length,
  () => {
    if (autoScroll.value) void scrollToBottom()
  },
)

onMounted(() => {
  void scrollToBottom()
})
</script>

<style scoped>
.log-panel {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: 0;
}

.log-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  flex-wrap: wrap;
}

.log-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: 600;
  color: #1f2933;
}

.log-tools {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.log-select,
.log-search {
  padding: 5px 8px;
  border: 1px solid #d1d5db;
  border-radius: 6px;
  font-size: 12px;
  font-family: inherit;
}

.log-search {
  min-width: 140px;
}

.check {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: #4b5563;
  cursor: pointer;
}

.log-body {
  height: 320px;
  overflow-y: auto;
  padding: 10px 12px;
  border-radius: 8px;
  background: #111827;
  font-family: 'JetBrains Mono', Consolas, 'Courier New', monospace;
  font-size: 12px;
  line-height: 1.6;
}

.log-line {
  display: flex;
  gap: 8px;
  color: #d1d5db;
  word-break: break-all;
}

.log-time {
  flex-shrink: 0;
  color: #6b7280;
}

.log-level {
  flex-shrink: 0;
  width: 48px;
  font-weight: 600;
}

.level-info .log-level {
  color: #60a5fa;
}

.level-warn {
  color: #fcd34d;
}

.level-warn .log-level {
  color: #fbbf24;
}

.level-error {
  color: #fca5a5;
}

.level-error .log-level {
  color: #f87171;
}

.log-msg {
  white-space: pre-wrap;
}

.log-empty {
  margin: 0;
  color: #6b7280;
  text-align: center;
  padding: 24px 0;
}
</style>
