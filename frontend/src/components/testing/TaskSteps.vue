<template>
  <div class="ap-card task-steps">
    <div class="steps">
      <div v-for="(stage, index) in stages" :key="stage.key" class="step" :class="`step-${stage.state}`">
        <div class="step-dot">
          <span v-if="stage.state === 'done'">✓</span>
          <span v-else-if="stage.state === 'error'">✕</span>
          <span v-else-if="stage.state === 'warn'">!</span>
          <span v-else>{{ index + 1 }}</span>
        </div>
        <div class="step-text">
          <div class="step-name">{{ stage.name }}</div>
          <div class="step-desc">{{ stage.desc }}</div>
        </div>
      </div>
    </div>
    <p v-if="note" class="step-note" :class="noteClass">{{ note }}</p>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'

// 任务阶段进度（PRD §3.3.6 五阶段 / §7.2 状态机）。
// 阶段状态由任务状态推导：done=已跨越、active=当前阶段、error=失败所在阶段、warn=清理待确认。
interface StageView {
  key: string
  name: string
  desc: string
  state: 'done' | 'active' | 'wait' | 'error' | 'warn'
}

const props = defineProps<{
  status: string
  /** 是否已产出执行结果（存在 result 说明采集/评估阶段已跨越）。 */
  hasResult?: boolean
}>()

/** 已跨越的阶段索引（-1 表示尚未开始）。 */
const reached = computed<number>(() => {
  switch (props.status) {
    case 'pending':
      return -1
    case 'running':
      return 1
    case 'collecting':
    case 'evaluating':
      return 2
    case 'generating':
      return 3
    case 'cleaning':
    case 'finished':
    case 'ended':
      return 4
    case 'cleanup_pending':
      return 4
    case 'failed':
      return props.hasResult ? 3 : 1
    case 'cancelled':
      return 1
    default:
      return -1
  }
})

const stages = computed<StageView[]>(() => {
  const base: Array<Omit<StageView, 'state'>> = [
    { key: 'prepare', name: '准备', desc: '连通性与接口就绪检查' },
    { key: 'run', name: '压测', desc: 'k6 子进程执行' },
    { key: 'collect', name: '采集评估', desc: '指标归一化 + 阈值判定' },
    { key: 'report', name: '报告生成', desc: 'html / pdf / word' },
    { key: 'cleanup', name: '环境清理', desc: '内置动作 + 可信脚本' },
  ]

  return base.map((stage, index) => {
    let state: StageView['state'] = 'wait'
    if (index < reached.value) {
      state = 'done'
    } else if (index === reached.value) {
      state = props.status === 'failed' || props.status === 'cancelled' ? 'error' : 'active'
    }

    if (stage.key === 'cleanup') {
      if (props.status === 'cleanup_pending') {
        state = 'warn'
      } else if (props.status === 'cleaning') {
        state = 'active'
      } else if (props.status === 'finished' || props.status === 'ended') {
        state = 'done'
      } else if (props.status === 'failed' || props.status === 'cancelled') {
        state = 'wait'
      }
    }
    return { ...stage, state }
  })
})

const note = computed<string>(() => {
  switch (props.status) {
    case 'pending':
      return '任务待执行：点击「开始执行」后将占用环境并拉起压测。'
    case 'running':
      return '压测执行中：可实时查看下方日志与指标。'
    case 'collecting':
      return '采集中：正在解析 k6 结果并归一化指标。'
    case 'evaluating':
      return '评估中：按任务创建时固化的阈值快照逐条判定。'
    case 'generating':
      return '报告生成中：按报告配置输出 html / pdf / word。'
    case 'finished':
      return '任务已完成：报告已生成，环境已自动清理。'
    case 'ended':
      return '任务已结束。'
    case 'failed':
      return '任务执行失败：已保留现场（环境未自动清理），可确认清理或重试。'
    case 'cancelled':
      return '任务已取消：k6 子进程已终止，按失败口径保留现场。'
    case 'cleanup_pending':
      return '清理待确认：失败/取消后保留现场，确认清理将执行环境清理流程。'
    case 'cleaning':
      return '正在执行环境清理（内置动作 + 可信脚本）…'
    default:
      return ''
  }
})

const noteClass = computed(() => {
  if (props.status === 'failed' || props.status === 'cleanup_pending') return 'note-error'
  if (props.status === 'finished') return 'note-success'
  return ''
})
</script>

<style scoped>
.task-steps {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.steps {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}

.step {
  flex: 1;
  display: flex;
  align-items: flex-start;
  gap: 8px;
  position: relative;
}

.step:not(:last-child)::after {
  content: '';
  position: absolute;
  top: 14px;
  left: 30px;
  right: -8px;
  height: 2px;
  background: #e5e7eb;
}

.step-done:not(:last-child)::after {
  background: #93c5fd;
}

.step-dot {
  width: 28px;
  height: 28px;
  flex-shrink: 0;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 13px;
  font-weight: 600;
  background: #f3f4f6;
  color: #6b7280;
  border: 1px solid #e5e7eb;
  z-index: 1;
}

.step-done .step-dot {
  background: #eff6ff;
  border-color: #93c5fd;
  color: #1d4ed8;
}

.step-active .step-dot {
  background: var(--ap-primary);
  border-color: var(--ap-primary);
  color: #fff;
  box-shadow: 0 0 0 4px rgba(37, 99, 235, 0.15);
}

.step-error .step-dot {
  background: #fef2f2;
  border-color: #fca5a5;
  color: #b91c1c;
}

.step-warn .step-dot {
  background: #fffbeb;
  border-color: #fcd34d;
  color: #b45309;
}

.step-text {
  min-width: 0;
  padding-top: 2px;
}

.step-name {
  font-size: 13px;
  font-weight: 600;
  color: #1f2933;
}

.step-wait .step-name {
  color: #9ca3af;
}

.step-desc {
  font-size: 11px;
  color: #9ca3af;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.step-note {
  margin: 0;
  padding: 8px 12px;
  border-radius: 6px;
  font-size: 13px;
  background: #f9fafb;
  color: #6b7280;
}

.note-error {
  background: #fef2f2;
  color: #b91c1c;
}

.note-success {
  background: #ecfdf5;
  color: #047857;
}
</style>
