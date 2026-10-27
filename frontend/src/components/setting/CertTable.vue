<template>
  <div class="ap-card">
    <table class="ap-table">
      <thead>
        <tr>
          <th>证书名称</th>
          <th>颁发机构</th>
          <th>有效期</th>
          <th>状态</th>
          <th>私钥</th>
          <th>关联服务</th>
          <th>操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="item in certificates" :key="item.id">
          <td>
            <span class="cert-name">{{ item.name }}</span>
            <span v-if="item.is_builtin" class="ap-badge ap-badge-info builtin-badge">内置自签</span>
            <div class="ap-muted cert-meta">#{{ item.id }} · {{ certExpiryHint(item.status, item.valid_to) }}</div>
          </td>
          <td>{{ item.issuer || '-' }}</td>
          <td class="validity">
            <span>{{ item.valid_from || '-' }}</span>
            <span class="ap-muted">→</span>
            <span>{{ item.valid_to || '-' }}</span>
          </td>
          <td>
            <span class="ap-badge" :class="certStatusMeta(item.status).badge">
              {{ certStatusMeta(item.status).label }}
            </span>
          </td>
          <td>
            <span v-if="item.has_key" class="ap-badge ap-badge-muted">已加密保存</span>
            <span v-else class="ap-muted">无</span>
          </td>
          <td>{{ item.associated_service || '-' }}</td>
          <td>
            <div class="ap-actions">
              <template v-if="canWrite">
                <button
                  v-if="item.is_builtin"
                  type="button"
                  class="ap-btn ap-btn-ghost ap-btn-sm"
                  @click="emit('reinit', item)"
                >
                  重新初始化
                </button>
                <button
                  v-else
                  type="button"
                  class="ap-btn ap-btn-danger ap-btn-sm"
                  @click="emit('remove', item)"
                >
                  删除
                </button>
              </template>
              <span v-else class="ap-muted">只读</span>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
    <p v-if="!loading && certificates.length === 0" class="ap-table-empty">暂无证书</p>
    <p v-if="loading" class="ap-table-empty">加载中…</p>
  </div>
</template>

<script setup lang="ts">
import type { CertificateInfo } from '@/types'
import { certExpiryHint, certStatusMeta } from '@/utils/setting'

// 证书表格（内置自签证书不可删除，仅可重新初始化有效期）。
defineProps<{
  certificates: CertificateInfo[]
  loading: boolean
  canWrite: boolean
}>()

const emit = defineEmits<{
  (e: 'reinit', cert: CertificateInfo): void
  (e: 'remove', cert: CertificateInfo): void
}>()
</script>

<style scoped>
.cert-name {
  font-weight: 600;
}

.builtin-badge {
  margin-left: 6px;
}

.cert-meta {
  font-size: 12px;
  margin-top: 2px;
}

.validity {
  white-space: nowrap;
  font-size: 12px;
}
</style>
