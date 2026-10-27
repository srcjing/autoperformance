<template>
  <div v-if="open" class="ap-dialog-mask" @click.self="onClose">
    <div class="ap-dialog" role="dialog" aria-modal="true">
      <div class="ap-dialog-header">
        <span>{{ isEdit ? '编辑产品' : '新建产品' }}</span>
        <button type="button" class="ap-link" :disabled="saving" @click="onClose">关闭</button>
      </div>

      <form class="ap-dialog-body" @submit.prevent="onSubmit">
        <div class="ap-form-row">
          <label class="ap-field">
            <span class="ap-field-label">产品名称 *</span>
            <input v-model="name" :disabled="saving" placeholder="如：网关服务" maxlength="128" />
          </label>
          <label class="ap-field">
            <span class="ap-field-label">产品编码 *（唯一）</span>
            <input v-model="code" :disabled="saving" placeholder="如：gateway" maxlength="64" />
          </label>
        </div>

        <div class="ap-form-row">
          <label class="ap-field">
            <span class="ap-field-label">产品版本 *</span>
            <input v-model="productVersion" :disabled="saving" placeholder="如：v1.2.3" maxlength="32" />
          </label>
          <label class="ap-field">
            <span class="ap-field-label">平台</span>
            <input v-model="platform" :disabled="saving" placeholder="如：Linux-x86 / 容器" maxlength="64" />
          </label>
        </div>

        <label class="ap-field">
          <span class="ap-field-label">描述</span>
          <textarea v-model="description" :disabled="saving" rows="2" maxlength="500" />
        </label>

        <p v-if="displayError" class="ap-alert ap-alert-error" role="alert">{{ displayError }}</p>
        <p v-else-if="isEdit" class="ap-alert ap-alert-hint">
          当前版本号 {{ product?.version }}：保存时若已被他人修改会提示并发冲突，请刷新后重试。
        </p>

        <div class="ap-dialog-footer">
          <button type="button" class="ap-btn ap-btn-ghost" :disabled="saving" @click="onClose">取消</button>
          <button type="submit" class="ap-btn ap-btn-primary" :disabled="saving">
            {{ saving ? '保存中…' : '保存' }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { ProductInfo, ProductPayload } from '@/types'

// 产品新建/编辑弹窗（全量字段 + version 乐观锁）。
// 状态（enabled/disabled）不在此维护：由列表操作走 PATCH /products/{id}/status。
const props = defineProps<{
  open: boolean
  product: ProductInfo | null
  saving: boolean
  /** 服务端返回的业务错误（22001/10402 等），由列表页透传。 */
  errorMessage?: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'submit', payload: ProductPayload): void
}>()

const name = ref('')
const code = ref('')
const productVersion = ref('')
const platform = ref('')
const description = ref('')
const formError = ref('')

const displayError = computed(() => props.errorMessage ?? formError.value)
const isEdit = computed(() => props.product !== null)

watch(
  () => props.open,
  (open) => {
    if (open) reset()
  },
  { immediate: true },
)

function reset(): void {
  const product = props.product
  name.value = product?.name ?? ''
  code.value = product?.code ?? ''
  productVersion.value = product?.product_version ?? ''
  platform.value = product?.platform ?? ''
  description.value = product?.description ?? ''
  formError.value = ''
}

function onClose(): void {
  if (props.saving) return
  emit('close')
}

function onSubmit(): void {
  if (!name.value.trim()) {
    formError.value = '请填写产品名称'
    return
  }
  if (!code.value.trim()) {
    formError.value = '请填写产品编码'
    return
  }
  if (!productVersion.value.trim()) {
    formError.value = '请填写产品版本'
    return
  }

  const payload: ProductPayload = {
    name: name.value.trim(),
    code: code.value.trim(),
    product_version: productVersion.value.trim(),
    platform: platform.value.trim(),
    description: description.value.trim(),
  }
  if (props.product) payload.version = props.product.version

  formError.value = ''
  emit('submit', payload)
}
</script>
