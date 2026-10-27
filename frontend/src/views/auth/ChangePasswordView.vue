<template>
  <div class="pwd-card">
    <h1>修改密码</h1>
    <p class="subtitle">
      {{ auth.mustChangePassword ? '首次登录需修改初始密码后才能继续操作' : '修改当前账号密码' }}
    </p>

    <form @submit.prevent="onSubmit">
      <label class="field">
        <span>原密码</span>
        <input
          v-model="oldPassword"
          name="old_password"
          type="password"
          autocomplete="current-password"
          :disabled="loading"
        />
      </label>
      <label class="field">
        <span>新密码</span>
        <input
          v-model="newPassword"
          name="new_password"
          type="password"
          autocomplete="new-password"
          :disabled="loading"
          placeholder="≥8 位，含大小写/数字/特殊字符"
        />
      </label>
      <label class="field">
        <span>确认新密码</span>
        <input
          v-model="confirmPassword"
          name="confirm_password"
          type="password"
          autocomplete="new-password"
          :disabled="loading"
        />
      </label>

      <p v-if="errorMessage" class="error" role="alert">{{ errorMessage }}</p>
      <p v-if="successMessage" class="success">{{ successMessage }}</p>

      <button type="submit" class="submit" :disabled="loading">
        {{ loading ? '提交中…' : '确认修改' }}
      </button>
    </form>

    <p class="hint">修改成功后旧登录凭证立即失效，需使用新密码重新登录。</p>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { changePassword } from '@/api/auth'
import { ApiError } from '@/api/http'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const auth = useAuthStore()

const oldPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const errorMessage = ref('')
const successMessage = ref('')
const loading = ref(false)

function validate(): string {
  if (!oldPassword.value || !newPassword.value) return '请填写原密码与新密码'
  if (newPassword.value !== confirmPassword.value) return '两次输入的新密码不一致'
  if (newPassword.value.length < 8) return '新密码长度不少于 8 位'
  if (newPassword.value.length > 72) return '新密码长度不超过 72 位'
  if (oldPassword.value === newPassword.value) return '新密码不能与原密码相同'
  return ''
}

async function onSubmit(): Promise<void> {
  if (loading.value) return
  errorMessage.value = ''
  successMessage.value = ''

  const invalid = validate()
  if (invalid) {
    errorMessage.value = invalid
    return
  }

  loading.value = true
  try {
    await changePassword({ old_password: oldPassword.value, new_password: newPassword.value })
    successMessage.value = '密码修改成功，请重新登录'
    auth.clearSession()
    // 给用户短暂反馈后跳转登录页。
    window.setTimeout(() => {
      void router.replace({ name: 'login' })
    }, 800)
  } catch (err) {
    // 20003 原密码错误 / 20004 复杂度不足 / 20005 新旧相同均由后端统一返回
    errorMessage.value = err instanceof ApiError ? err.message : '修改失败，请稍后重试'
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.pwd-card {
  width: 400px;
  padding: 32px;
  background: #fff;
  border-radius: 10px;
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.2);
}

h1 {
  margin: 0;
  font-size: 22px;
  color: #1f2933;
  text-align: center;
}

.subtitle {
  margin: 6px 0 24px;
  text-align: center;
  color: #6b7280;
  font-size: 13px;
}

.field {
  display: block;
  margin-bottom: 16px;
  font-size: 13px;
  color: #374151;
}

.field input {
  width: 100%;
  margin-top: 6px;
  padding: 9px 10px;
  border: 1px solid #d1d5db;
  border-radius: 6px;
  font-size: 14px;
}

.error {
  margin: 0 0 14px;
  padding: 8px 10px;
  border-radius: 6px;
  background: #fef2f2;
  color: #b91c1c;
  font-size: 13px;
}

.success {
  margin: 0 0 14px;
  padding: 8px 10px;
  border-radius: 6px;
  background: #ecfdf5;
  color: #047857;
  font-size: 13px;
}

.submit {
  width: 100%;
  padding: 10px;
  border: none;
  border-radius: 6px;
  background: var(--ap-primary);
  color: #fff;
  font-size: 15px;
  cursor: pointer;
}

.submit:disabled {
  opacity: 0.7;
  cursor: not-allowed;
}

.hint {
  margin-top: 18px;
  font-size: 12px;
  color: #9ca3af;
  text-align: center;
}
</style>
