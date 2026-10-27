<template>
  <div class="ap-card preview-card">
    <div class="preview-head">
      <span class="ap-muted">HTML 预览（沙箱渲染，脚本已禁用）</span>
      <button type="button" class="ap-btn ap-btn-ghost ap-btn-sm" :disabled="loading" @click="reload">
        重新加载
      </button>
    </div>
    <p v-if="!available" class="ap-table-empty">该报告没有 HTML 产物</p>
    <p v-else-if="error" class="ap-alert ap-alert-error" role="alert">{{ error }}</p>
    <p v-else-if="loading" class="ap-table-empty">加载中…</p>
    <iframe
      v-else-if="url"
      class="preview-frame"
      :src="url"
      sandbox=""
      title="HTML 报告预览"
    ></iframe>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useArtifactPreview } from '@/composables/useArtifactPreview'

// HTML 报告在线预览（iframe sandbox="" 阻断脚本，样式与内容仍正常渲染）。
const props = defineProps<{
  reportId: string
  available: boolean
}>()

const { url, loading, error, reload } = useArtifactPreview(
  computed(() => props.reportId),
  'html',
  computed(() => props.available),
)
</script>

<style scoped>
.preview-card {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.preview-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  font-size: 12px;
}

.preview-frame {
  width: 100%;
  height: 680px;
  border: 1px solid #e5e7eb;
  border-radius: 6px;
  background: #fff;
}
</style>
