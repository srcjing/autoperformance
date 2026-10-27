<template>
  <div class="ap-card">
    <table class="ap-table">
      <thead>
        <tr>
          <th>标签名称</th>
          <th>颜色</th>
          <th>说明</th>
          <th>关联环境数</th>
          <th>操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="tag in tags" :key="tag.id">
          <td>
            <span class="tag-dot" :style="{ background: tag.color || '#9ca3af' }" />
            {{ tag.name }}
          </td>
          <td>{{ tag.color || '-' }}</td>
          <td>{{ tag.description || '-' }}</td>
          <td>
            <button type="button" class="ap-link" @click="emit('show-envs', tag)">
              {{ tag.environment_count ?? 0 }}
            </button>
          </td>
          <td>
            <div class="ap-actions">
              <button type="button" class="ap-btn ap-btn-ghost ap-btn-sm" @click="emit('show-envs', tag)">
                关联环境
              </button>
              <template v-if="canWrite">
                <button type="button" class="ap-btn ap-btn-ghost ap-btn-sm" @click="emit('edit', tag)">
                  编辑
                </button>
                <button type="button" class="ap-btn ap-btn-danger ap-btn-sm" @click="emit('remove', tag)">
                  删除
                </button>
              </template>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-if="!loading && tags.length === 0" class="ap-table-empty">暂无标签，点击「新建标签」开始维护</p>
    <p v-if="loading" class="ap-table-empty">加载中…</p>
  </div>
</template>

<script setup lang="ts">
import type { TagInfo } from '@/types'

// 标签表格（含关联环境数；点击可反查关联环境明细）。
defineProps<{
  tags: TagInfo[]
  loading: boolean
  canWrite: boolean
}>()

const emit = defineEmits<{
  (e: 'edit', tag: TagInfo): void
  (e: 'remove', tag: TagInfo): void
  (e: 'show-envs', tag: TagInfo): void
}>()
</script>

<style scoped>
.tag-dot {
  display: inline-block;
  width: 10px;
  height: 10px;
  margin-right: 6px;
  border-radius: 50%;
  vertical-align: middle;
}
</style>
