<template>
  <div class="login-card">
    <h1>auto_performance</h1>
    <p class="subtitle">性能测试平台</p>

    <form @submit.prevent="onLogin">
      <label class="field">
        <span>用户名</span>
        <input
          v-model="username"
          name="username"
          autocomplete="username"
          :disabled="loading"
          placeholder="请输入用户名"
        />
      </label>
      <label class="field">
        <span>密码</span>
        <input
          v-model="password"
          name="password"
          type="password"
          autocomplete="current-password"
          :disabled="loading"
          placeholder="请输入密码"
        />
      </label>

      <p v-if="errorMessage" class="error" role="alert">{{ errorMessage }}</p>

      <button type="submit" class="submit" :disabled="loading">
        {{ loading ? '登录中…' : '登录' }}
      </button>
    </form>

    <p class="hint">首次登录需修改初始密码；账号连续登录失败会被临时锁定。</p>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ApiError } from '@/api/http'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()

const username = ref('admin')
const password = ref('')
const errorMessage = ref('')
const loading = ref(false)

async function onLogin(): Promise<void> {
  if (loading.value) return
  errorMessage.value = ''

  const name = username.value.trim()
  if (!name || !password.value) {
    errorMessage.value = '请输入用户名与密码'
    return
  }

  loading.value = true
  try {
    const res = await auth.signIn(name, password.value)
    if (res.must_change_password) {
      await router.replace({ name: 'change-password' })
      return
    }
    const redirect = typeof route.query.redirect === 'string' && route.query.redirect
      ? route.query.redirect
      : auth.homePath
    await router.replace(redirect)
  } catch (err) {
    // 后端统一提示「用户名或密码错误」（20001）；锁定返回 10502 的提示文本。
    errorMessage.value = err instanceof ApiError ? err.message : '登录失败，请稍后重试'
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.login-card {
  width: 360px;
  padding: 32px;
  background: #fff;
  border-radius: 10px;
  box-shadow: 0 10px 30px rgba(0, 0, 0, 0.2);
}

.login-card h1 {
  margin: 0;
  font-size: 22px;
  color: #1f2933;
  text-align: center;
}

.subtitle {
  margin: 6px 0 24px;
  text-align: center;
  color: #6b7280;
  font-size: 14px;
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

.field input:disabled {
  background: #f3f4f6;
}

.error {
  margin: 0 0 14px;
  padding: 8px 10px;
  border-radius: 6px;
  background: #fef2f2;
  color: #b91c1c;
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
