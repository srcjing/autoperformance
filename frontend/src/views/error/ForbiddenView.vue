<template>
  <div class="forbidden">
    <h1>403</h1>
    <p>当前账号无权访问该页面</p>
    <p class="hint">如需访问，请联系系统管理员为账号授予对应子模块权限。</p>
    <RouterLink class="back" to="/">返回首页</RouterLink>
  </div>
</template>

<script setup lang="ts">
import { RouterLink } from 'vue-router'
</script>

<style scoped>
.forbidden {
  min-height: 60vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: #6b7280;
}

.forbidden h1 {
  margin: 0;
  font-size: 64px;
  color: #b91c1c;
}

.hint {
  font-size: 13px;
  color: #9ca3af;
}

.back {
  margin-top: 8px;
  color: var(--ap-primary);
  font-size: 14px;
}
</style>
