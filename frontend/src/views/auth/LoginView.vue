<template>
  <div class="login-card">
    <h1>auto_performance</h1>
    <p class="subtitle">性能测试平台</p>

    <form @submit.prevent="onLogin">
      <label class="field">
        <span>用户名</span>
        <input v-model="username" name="username" autocomplete="username" />
      </label>
      <label class="field">
        <span>密码</span>
        <input v-model="password" name="password" type="password" autocomplete="current-password" />
      </label>
      <button type="submit" class="submit">登录</button>
    </form>

    <p class="hint">骨架阶段：任意账号密码点击登录即可进入（后端返回占位 token）。</p>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()

const username = ref('admin')
const password = ref('')

function onLogin(): void {
  // 骨架：占位会话（后续对接 POST /api/v1/auth/login 并回填真实权限集合）
  auth.setSession('dev-token', { id: '1', username: username.value, name: username.value }, [])
  const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/'
  router.push(redirect)
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

.hint {
  margin-top: 18px;
  font-size: 12px;
  color: #9ca3af;
  text-align: center;
}
</style>
