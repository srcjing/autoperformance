<template>
  <div v-if="open" class="ap-dialog-mask">
    <div class="ap-dialog admin-form" role="dialog" aria-modal="true">
      <div class="ap-dialog-header">
        <span>{{ isEdit ? '编辑管理员' : '新建管理员' }}</span>
        <button type="button" class="ap-link" :disabled="saving" @click="emit('close')">关闭</button>
      </div>

      <form class="ap-dialog-body" @submit.prevent="onSubmit">
        <div class="ap-form-row">
          <label class="ap-field">
            <span class="ap-field-label">用户名（登录名）*</span>
            <input
              v-if="!isEdit"
              v-model="username"
              :disabled="saving"
              maxlength="64"
              autocomplete="off"
              placeholder="如：zhangsan"
            />
            <input v-else :value="item?.username ?? ''" disabled />
            <span class="ap-muted hint">登录名唯一，创建后不可修改。</span>
          </label>
          <label class="ap-field">
            <span class="ap-field-label">姓名 *</span>
            <input v-model="name" :disabled="saving" maxlength="64" placeholder="管理员真实姓名" />
          </label>
        </div>

        <div class="ap-form-row">
          <label class="ap-field">
            <span class="ap-field-label">手机号</span>
            <input v-model="phone" :disabled="saving" maxlength="32" placeholder="选填" />
          </label>
          <label class="ap-field">
            <span class="ap-field-label">邮箱</span>
            <input v-model="email" :disabled="saving" maxlength="128" placeholder="选填" />
          </label>
        </div>

        <template v-if="!isEdit">
          <label class="ap-field">
            <span class="ap-field-label">初始密码 *</span>
            <input
              v-model="password"
              type="password"
              :disabled="saving"
              autocomplete="new-password"
              placeholder="请设置初始密码"
            />
            <span class="ap-muted hint">{{ PASSWORD_POLICY_HINT }}</span>
          </label>
        </template>

        <label class="ap-field">
          <span class="ap-field-label">描述</span>
          <textarea v-model="description" :disabled="saving" rows="2" maxlength="500" />
        </label>

        <p v-if="isEdit" class="ap-alert ap-alert-hint">
          用户名、状态与角色绑定不在本表单修改：角色请用「角色授权」，状态请用「停用/启用」，
          口令请用「重置密码」（重置后下次登录强制改密）。
        </p>

        <p v-if="displayError" class="ap-alert ap-alert-error" role="alert">{{ displayError }}</p>
        <p v-else-if="isEdit" class="ap-alert ap-alert-hint">
          当前版本号 {{ item?.version }}：保存时若已被他人修改会提示并发冲突，请刷新后重试。
        </p>

        <div class="ap-dialog-footer">
          <button type="button" class="ap-btn ap-btn-ghost" :disabled="saving" @click="emit('close')">
            取消
          </button>
          <button type="submit" class="ap-btn ap-btn-primary" :disabled="saving">
            {{ saving ? '保存中…' : '保存' }}
          </button>
        </div>
      </form>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { AdminCreatePayload, AdminInfo, AdminUpdatePayload } from '@/types'
import { PASSWORD_POLICY_HINT, validatePasswordStrength } from '@/utils/setting'

// 管理员新建/编辑表单（白名单字段，DES-E-002）。
// 新建：上行 username/name/phone/email/description/password，不发送 role/status 等提权字段；
// 编辑：用户名/状态/内置标记不可改，仅上行 name/phone/email/description + version 乐观锁。
const props = defineProps<{
  open: boolean
  item: AdminInfo | null
  saving: boolean
  /** 服务端返回的业务错误（26001/20004/10402 等），由列表页透传。 */
  errorMessage?: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'create', payload: AdminCreatePayload): void
  (e: 'update', payload: AdminUpdatePayload): void
}>()

const username = ref('')
const name = ref('')
const phone = ref('')
const email = ref('')
const description = ref('')
const password = ref('')
const formError = ref('')

const isEdit = computed(() => props.item !== null)
const displayError = computed(() => props.errorMessage || formError.value)

watch(
  () => props.open,
  (open) => {
    if (open) reset()
  },
  { immediate: true },
)

function reset(): void {
  username.value = props.item?.username ?? ''
  name.value = props.item?.name ?? ''
  phone.value = props.item?.phone ?? ''
  email.value = props.item?.email ?? ''
  description.value = props.item?.description ?? ''
  password.value = '' // 口令永不回显
  formError.value = ''
}


function onSubmit(): void {
  const trimmedName = name.value.trim()
  const trimmedPhone = phone.value.trim()
  const trimmedEmail = email.value.trim()
  const trimmedDescription = description.value.trim()

  if (!isEdit.value && !username.value.trim()) {
    formError.value = '请填写用户名'
    return
  }
  if (!trimmedName) {
    formError.value = '请填写姓名'
    return
  }
  if (trimmedEmail && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(trimmedEmail)) {
    formError.value = '邮箱格式不正确'
    return
  }

  if (isEdit.value) {
    formError.value = ''
    emit('update', {
      name: trimmedName,
      phone: trimmedPhone,
      email: trimmedEmail,
      description: trimmedDescription,
      version: props.item?.version,
    })
    return
  }

  const passwordError = validatePasswordStrength(password.value)
  if (passwordError) {
    formError.value = passwordError
    return
  }

  formError.value = ''
  emit('create', {
    username: username.value.trim(),
    name: trimmedName,
    phone: trimmedPhone,
    email: trimmedEmail,
    description: trimmedDescription,
    password: password.value,
  })
}
</script>

<style scoped>
.admin-form {
  width: 640px;
}

.hint {
  font-size: 12px;
  line-height: 1.5;
}
</style>
