<template>
  <div class="ap-page">
    <div class="ap-page-header">
      <h2 class="ap-page-title">证书管理</h2>
      <button v-if="canWrite" type="button" class="ap-btn ap-btn-primary" @click="openImport">
        导入证书
      </button>
    </div>

    <div class="ap-card ap-toolbar">
      <p class="ap-muted toolbar-hint">
        系统内置一份自签证书（不可删除，可「重新初始化」更新有效期）。证书与私钥由服务端
        AES-256-GCM 信封加密存储，接口响应与页面均不回显密钥材料。
      </p>
      <button type="button" class="ap-btn ap-btn-ghost" :disabled="loading" @click="reload">
        刷新
      </button>
    </div>

    <p v-if="errorMessage" class="ap-alert ap-alert-error" role="alert">{{ errorMessage }}</p>

    <div class="ap-card expiry-banner" :class="expiryBannerClass">
      <span class="banner-title">过期提醒</span>
      <span>{{ expirySummary }}</span>
    </div>

    <CertTable
      :certificates="store.certificates"
      :loading="loading"
      :can-write="canWrite"
      @reinit="openReinit"
      @remove="onRemove"
    />

    <CertImportDialog
      :open="importOpen"
      :saving="saving"
      :error-message="dialogError"
      @close="closeImport"
      @submit="onImport"
    />

    <CertReinitDialog
      :open="reinitOpen"
      :cert="reinitTarget"
      :saving="saving"
      :error-message="dialogError"
      @close="closeReinit"
      @confirm="onReinit"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import CertImportDialog from '@/components/setting/CertImportDialog.vue'
import CertReinitDialog from '@/components/setting/CertReinitDialog.vue'
import CertTable from '@/components/setting/CertTable.vue'
import { useAuthStore } from '@/stores/auth'
import { useSettingStore } from '@/stores/setting'
import type { CertificateImportPayload, CertificateInfo } from '@/types'
import { describeSettingError } from '@/utils/setting'

// 证书管理页（PRD §3.6.1 / api-contract §2.7）：列表 + 导入 + 删除 + 内置证书重新初始化。
const store = useSettingStore()
const auth = useAuthStore()

const canWrite = computed(() => auth.hasPerm('setting.cert', 'write'))
const loading = computed(() => store.certificateLoading)
const errorMessage = ref('')
const dialogError = ref('')
const saving = ref(false)

const importOpen = ref(false)
const reinitOpen = ref(false)
const reinitTarget = ref<CertificateInfo | null>(null)

/** 过期提醒汇总（有效/即将过期/已过期数量）。 */
const expirySummary = computed(() => {
  const list = store.certificates
  const expired = list.filter((item) => item.status === 'expired').length
  const expiring = list.filter((item) => item.status === 'expiring').length
  const valid = list.filter((item) => item.status === 'valid').length
  if (list.length === 0) return '暂无证书'
  if (expired > 0) return `已过期 ${expired} 个证书，请尽快更换；即将过期 ${expiring} 个，有效 ${valid} 个`
  if (expiring > 0) return `即将过期 ${expiring} 个证书（30 天内到期）；有效 ${valid} 个`
  return `全部证书有效（${valid} 个）`
})

const expiryBannerClass = computed(() => {
  const list = store.certificates
  if (list.some((item) => item.status === 'expired')) return 'banner-danger'
  if (list.some((item) => item.status === 'expiring')) return 'banner-warn'
  return 'banner-ok'
})

async function reload(): Promise<void> {
  errorMessage.value = ''
  try {
    await store.fetchCertificates()
  } catch (err) {
    errorMessage.value = describeSettingError(err, '加载证书列表失败')
  }
}

function openImport(): void {
  dialogError.value = ''
  importOpen.value = true
}

function closeImport(): void {
  if (saving.value) return
  importOpen.value = false
  dialogError.value = ''
}

async function onImport(payload: CertificateImportPayload): Promise<void> {
  if (saving.value) return
  saving.value = true
  dialogError.value = ''
  try {
    await store.uploadCertificate(payload)
    importOpen.value = false
    await reload()
  } catch (err) {
    dialogError.value = describeSettingError(err, '导入证书失败')
  } finally {
    saving.value = false
  }
}

function openReinit(cert: CertificateInfo): void {
  dialogError.value = ''
  reinitTarget.value = cert
  reinitOpen.value = true
}

function closeReinit(): void {
  if (saving.value) return
  reinitOpen.value = false
  reinitTarget.value = null
  dialogError.value = ''
}

async function onReinit(): Promise<void> {
  if (saving.value || !reinitTarget.value) return
  saving.value = true
  dialogError.value = ''
  try {
    await store.reinitBuiltinCertificate(reinitTarget.value.id)
    reinitOpen.value = false
    reinitTarget.value = null
    await reload()
  } catch (err) {
    dialogError.value = describeSettingError(err, '重新初始化失败')
  } finally {
    saving.value = false
  }
}

async function onRemove(cert: CertificateInfo): Promise<void> {
  if (!window.confirm(`确认删除证书「${cert.name}」？删除为逻辑删除，关联环境将无法再引用。`)) return

  errorMessage.value = ''
  try {
    await store.removeCertificate(cert.id)
    await reload()
  } catch (err) {
    // 26004 内置证书不可删除 / 10301 不存在。
    errorMessage.value = describeSettingError(err, '删除证书失败')
  }
}

onMounted(reload)
</script>

<style scoped>
.toolbar-hint {
  margin: 0;
  flex: 1;
  font-size: 12px;
  line-height: 1.6;
}

.expiry-banner {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 13px;
  padding: 10px 14px;
}

.banner-title {
  font-weight: 600;
  white-space: nowrap;
}

.banner-ok {
  border-left: 3px solid #16a34a;
}

.banner-warn {
  border-left: 3px solid #d97706;
  background: #fffbeb;
}

.banner-danger {
  border-left: 3px solid #dc2626;
  background: #fef2f2;
}
</style>
