<template>
  <div v-if="open" class="ap-dialog-mask" @click.self="onMaskClick">
    <div class="ap-dialog password-dialog" role="dialog" aria-modal="true">
      <div class="ap-dialog-header">
        <span>重置密码 · {{ admin?.username ?? '' }}</span>
        <button type="button" class="ap-link" :disabled="saving" @click="emit('close')">关闭</button>
      </div>

      <form class="ap-dialog-body" @submit.prevent="onSubmit">
        <p class="ap-alert ap-alert-hint">
          重置后该账号下次登录会被强制改密；当前已签发的会话在权限/状态变更后由服务端即时失效。
        </p>

        <label class="ap-field">
          <span class="ap-field-label">新密码 *</span>
          <input
            v-model="newPassword"
            type="password"
            :disabled="saving"
            autocomplete="new-password"
            placeholder="请输入新密码"
          />
          <span class="ap-muted hint">{{ PASSWORD_POLICY_HINT }}</span>
        </label>
        <label class="ap-field">
          <span class="ap-field-label">确认新密码 *</span>
          <input
            v-model="confirmPassword"
            type="password"
            :disabled="saving"
            autocomplete="new-password"
            placeholder="请再次输入新密码"
          />
        </label>

        <p v-if="displayError" class="ap-alert ap-alert-error" role="alert">{{ displayError }}</p>

        <div class="ap-dialog-footer">
          <button type="button" class="ap-btn ap-btn-ghost" :disabled="saving" @click="emit('close')">
            取消
          </button>
          <button type="submit" class="ap-btn ap-btn-primary" :disabled="saving">
            {{ saving ? '提交中…' : '确认重置' }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { AdminInfo } from '@/types'
import { PASSWORD_POLICY_HINT, validatePasswordStrength } from '@/utils/setting'

// 重置管理员口令（POST /admins/{id}/reset-password，body { new_password }）。
// 前端做复杂度预校验，最终以服务端 20004 为准。
const props = defineProps<{
  open: boolean
  admin: AdminInfo | null
  saving: boolean
  errorMessage?: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'submit', newPassword: string): void
}>()

const newPassword = ref('')
const confirmPassword = ref('')
const formError = ref('')

const displayError = computed(() => props.errorMessage || formError.value)

watch(
  () => props.open,
  (open) => {
    if (open) {
      newPassword.value = ''
      confirmPassword.value = ''
      formError.value = ''
    }
  },
  { immediate: true },
)

function onMaskClick(): void {
  if (!props.saving) emit('close')
}

function onSubmit(): void {
  const passwordError = validatePasswordStrength(newPassword.value)
  if (passwordError) {
    formError.value = passwordError
    return
  }
  if (newPassword.value !== confirmPassword.value) {
    formError.value = '两次输入的新密码不一致'
    return
  }
  formError.value = ''
  emit('submit', newPassword.value)
}
</script>

<style scoped>
.password-dialog {
  width: 480px;
}

.hint {
  font-size: 12px;
  line-height: 1.5;
}
</style>
