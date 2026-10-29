<template>
  <label class="ap-field">
    <span class="ap-field-label">证书</span>
    <select :value="modelValue" :disabled="disabled || !canLoad || loading" @change="onChange">
      <option value="">暂不关联</option>
      <option v-for="item in options" :key="item.id" :value="item.id">
        {{ item.name }}<template v-if="item.is_builtin">（内置自签）</template>
      </option>
    </select>
    <span v-if="!canLoad" class="ap-muted">无「证书管理」权限，可留空</span>
    <span v-else-if="loadError" class="ap-muted">{{ loadError }}</span>
  </label>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { listCertificates, type CertificateOption } from '@/api/setting'
import { useAuthStore } from '@/stores/auth'

// 证书选择器（环境表单可选引用，Phase 7 维护数据源）。
// 权限隔离：仅具备 setting.cert:write 的账号才请求列表，避免无权限账号被 403 拦截跳转。
defineProps<{
  modelValue: string
  disabled?: boolean
}>()

const emit = defineEmits<{ (e: 'update:modelValue', value: string): void }>()

const auth = useAuthStore()
const options = ref<CertificateOption[]>([])
const loading = ref(false)
const loadError = ref('')

const canLoad = computed(() => auth.hasPerm('setting.cert', 'write'))

async function load(): Promise<void> {
  if (!canLoad.value) return
  loading.value = true
  loadError.value = ''
  try {
    const res = await listCertificates()
    options.value = res.list
    if (res.list.length === 0) loadError.value = '暂无可选项，可在系统设置中维护'
  } catch {
    // 接口未就绪/依赖不可用时降级为空列表，不阻断环境表单。
    options.value = []
    loadError.value = '暂无可选项'
  } finally {
    loading.value = false
  }
}

function onChange(event: Event): void {
  const target = event.target as HTMLSelectElement
  emit('update:modelValue', target.value)
}

onMounted(load)
watch(canLoad, (value) => {
  if (value) void load()
})
</script>

<style scoped>
.ap-field select {
  min-width: 200px;
}
</style>
