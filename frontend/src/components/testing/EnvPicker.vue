<template>
  <label class="ap-field env-picker">
    <span class="ap-field-label">{{ label }}</span>

    <template v-if="multiple">
      <select
        v-model="multiModel"
        multiple
        class="env-multi"
        :size="Math.min(Math.max(options.length, 3), 6)"
        :disabled="disabled || loading || !canRead"
      >
        <option
          v-for="item in options"
          :key="item.id"
          :value="item.id"
          :disabled="item.status === 'disabled' && !selectedValues.includes(item.id)"
        >
          {{ optionLabel(item) }}
        </option>
      </select>
      <span class="ap-muted">已选 {{ selectedValues.length }} 个环境（Ctrl/⌘ + 点击可多选）</span>
    </template>

    <template v-else>
      <select v-model="singleModel" :disabled="disabled || loading || !canRead">
        <option value="">请选择环境</option>
        <option
          v-for="item in options"
          :key="item.id"
          :value="item.id"
          :disabled="item.status === 'disabled' && item.id !== singleModel"
        >
          {{ optionLabel(item) }}
        </option>
        <option v-if="missingSingle" :value="singleModel">
          #{{ singleModel }}（已下线/无读取权限）
        </option>
      </select>
    </template>

    <span v-if="!canRead" class="ap-muted">缺少「环境列表」读取权限，无法选择环境</span>
    <span v-else-if="loading" class="ap-muted">加载中…</span>
    <span v-else-if="loadError" class="ap-muted">{{ loadError }}</span>
  </label>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { listEnvironments } from '@/api/environment'
import { useAuthStore } from '@/stores/auth'
import type { EnvironmentInfo } from '@/types'

// 环境选择器（测试模板/任务的环境要素）。
//   单选用途：任务 environment_id（未选为空串）；
//   多选用途：模板 env_ids 默认选择集。
// 用 v-model 代理而非 :value，保证异步选项到位后仍能正确回显（原生 select 的 selected 行为）。
// 已下线（disabled）环境不进入新任务可选（PRD §3.1.5），但历史回显仍展示并标注。
const props = defineProps<{
  modelValue: string | string[]
  label?: string
  multiple?: boolean
  disabled?: boolean
}>()

const emit = defineEmits<{ (e: 'update:modelValue', value: string | string[]): void }>()

const auth = useAuthStore()
const options = ref<EnvironmentInfo[]>([])
const loading = ref(false)
const loadError = ref('')

const canRead = computed(() => auth.hasPerm('env.list', 'read'))

const selectedValues = computed<string[]>(() => {
  if (Array.isArray(props.modelValue)) return props.modelValue
  return props.modelValue ? [props.modelValue] : []
})

const singleModel = computed<string>({
  get: () => (Array.isArray(props.modelValue) ? (props.modelValue[0] ?? '') : props.modelValue),
  set: (value) => emit('update:modelValue', value),
})

const multiModel = computed<string[]>({
  get: () => selectedValues.value,
  set: (value) => emit('update:modelValue', value),
})

const missingSingle = computed(
  () => Boolean(singleModel.value) && !options.value.some((item) => item.id === singleModel.value),
)

/** 选项文案（标注测试中/已下线，避免误选被占用环境）。 */
function optionLabel(item: EnvironmentInfo): string {
  if (item.status === 'disabled') return `${item.name}（已下线）`
  if (item.status === 'testing') return `${item.name}（测试中）`
  return item.name
}

async function load(): Promise<void> {
  if (!canRead.value) return
  loading.value = true
  loadError.value = ''
  try {
    // 上限 200（api-contract §1.3），足够覆盖环境选择场景。
    const res = await listEnvironments({ page: 1, page_size: 200 })
    options.value = res.list
  } catch {
    loadError.value = '加载环境列表失败'
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  void load()
})
</script>

<style scoped>
.env-multi {
  min-height: 96px;
}

.env-picker select {
  min-width: 220px;
}
</style>
