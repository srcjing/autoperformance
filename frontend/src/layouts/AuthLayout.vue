<template>
  <div class="auth-layout">
    <RouterView />
  </div>
</template>

<script setup lang="ts">
// 认证布局（对齐 frontend-structure.md §2.1）：居中承载登录页。
</script>

<style scoped>
.auth-layout {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #1f2937 0%, #374151 100%);
}
</style>
