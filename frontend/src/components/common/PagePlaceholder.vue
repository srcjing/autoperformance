<template>
  <div class="page-placeholder">
    <h2>{{ title }}</h2>
    <p v-if="description">{{ description }}</p>
  </div>
</template>

<script setup lang="ts">
defineProps<{
  title: string
  description?: string
}>()
</script>

<style scoped>
.page-placeholder {
  min-height: 240px;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  background: #fff;
  border: 1px dashed #d1d5db;
  border-radius: 8px;
  color: #6b7280;
}

.page-placeholder h2 {
  margin: 0;
  font-size: 18px;
  color: #374151;
}
</style>
