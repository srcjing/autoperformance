<template>
  <div v-if="open" class="ap-dialog-mask">
    <div class="ap-dialog" role="dialog" aria-modal="true">
      <div class="ap-dialog-header">
        <span>{{ isEdit ? '编辑环境' : '新建环境' }}</span>
        <button type="button" class="ap-link" :disabled="saving" @click="onClose">关闭</button>
      </div>

      <form class="ap-dialog-body" @submit.prevent="onSubmit">
        <div class="ap-form-row">
          <label class="ap-field">
            <span class="ap-field-label">环境名称 *</span>
            <input v-model="name" :disabled="saving" placeholder="如：测试集群 A" maxlength="128" />
          </label>
          <label class="ap-field">
            <span class="ap-field-label">环境编码 *（唯一）</span>
            <input v-model="code" :disabled="saving" placeholder="如：test-cluster-a" maxlength="64" />
          </label>
        </div>

        <div class="ap-form-row">
          <label class="ap-field">
            <span class="ap-field-label">环境分类 *</span>
            <select v-model="categoryId" :disabled="saving">
              <option value="">请选择分类</option>
              <option v-for="item in categories" :key="item.id" :value="item.id">{{ item.name }}</option>
            </select>
          </label>
          <label class="ap-field">
            <span class="ap-field-label">环境类型</span>
            <select v-model="type" :disabled="saving">
              <option value="single">单机</option>
              <option value="cluster">集群</option>
            </select>
          </label>
        </div>

        <label class="ap-field">
          <span class="ap-field-label">主机地址 *（IP/域名，每行一个）</span>
          <textarea
            v-model="hostsText"
            :disabled="saving"
            placeholder="10.0.0.1&#10;10.0.0.2"
            rows="4"
          />
        </label>

        <div class="ap-form-row">
          <label class="ap-field">
            <span class="ap-field-label">操作系统</span>
            <input v-model="os" :disabled="saving" placeholder="如：Linux / Windows" maxlength="64" />
          </label>
        </div>

        <div class="ap-field">
          <span class="ap-field-label">标签（预定义多选）</span>
          <TagSelector v-model="tagIds" :tags="tags" :disabled="saving" />
        </div>

        <div class="ap-form-row">
          <SshHostSelector v-model="sshHostId" :disabled="saving" />
          <CertificateSelector v-model="certId" :disabled="saving" />
        </div>

        <label class="ap-field">
          <span class="ap-field-label">描述</span>
          <textarea v-model="description" :disabled="saving" rows="2" maxlength="500" />
        </label>

        <p v-if="displayError" class="ap-alert ap-alert-error" role="alert">{{ displayError }}</p>
        <p v-else-if="isEdit" class="ap-alert ap-alert-hint">
          当前版本号 {{ environment?.version }}：保存时若已被他人修改会提示并发冲突，请刷新后重试。
        </p>

        <div class="ap-dialog-footer">
          <button type="button" class="ap-btn ap-btn-ghost" :disabled="saving" @click="onClose">取消</button>
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
import CertificateSelector from './CertificateSelector.vue'
import SshHostSelector from './SshHostSelector.vue'
import TagSelector from './TagSelector.vue'
import type {
  EnvironmentCategory,
  EnvironmentInfo,
  EnvironmentPayload,
  EnvironmentType,
  TagInfo,
} from '@/types'

// 环境新建/编辑弹窗（全量字段 + version 乐观锁）。
// 状态（pending/testing/disabled）不在此维护：由列表操作走 PATCH /status。
const props = defineProps<{
  open: boolean
  environment: EnvironmentInfo | null
  categories: EnvironmentCategory[]
  tags: TagInfo[]
  saving: boolean
  /** 服务端返回的业务错误（21001/10402 等），由列表页透传。 */
  errorMessage?: string
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'submit', payload: EnvironmentPayload): void
}>()

const name = ref('')
const code = ref('')
const categoryId = ref('')
const type = ref<EnvironmentType>('single')
const hostsText = ref('')
const os = ref('')
const tagIds = ref<string[]>([])
const sshHostId = ref('')
const certId = ref('')
const description = ref('')
const formError = ref('')

const displayError = computed(() => props.errorMessage ?? formError.value)

const isEdit = computed(() => props.environment !== null)

watch(
  () => props.open,
  (open) => {
    if (!open) return
    reset()
  },
  { immediate: true },
)

function reset(): void {
  const env = props.environment
  name.value = env?.name ?? ''
  code.value = env?.code ?? ''
  categoryId.value = env?.category_id ?? ''
  type.value = env?.type ?? 'single'
  hostsText.value = (env?.hosts ?? []).join('\n')
  os.value = env?.os ?? ''
  tagIds.value = (env?.tags ?? []).map((tag) => tag.id)
  sshHostId.value = env?.ssh_host_id ?? ''
  certId.value = env?.cert_id ?? ''
	description.value = env?.description ?? ''
  formError.value = ''
}

/** 解析主机地址：支持换行/逗号/空格分隔。 */
function parseHosts(): string[] {
  return hostsText.value
    .split(/[\n,，;；\s]+/)
    .map((item) => item.trim())
    .filter((item) => item.length > 0)
}

function validate(hosts: string[]): string {
  if (!name.value.trim()) return '请填写环境名称'
  if (!code.value.trim()) return '请填写环境编码'
  if (!categoryId.value) return '请选择环境分类'
  if (hosts.length === 0) return '请填写至少一个主机地址'
  if (hosts.length > 100) return '主机地址数量不能超过 100 个'
  return ''
}

function onClose(): void {
  if (props.saving) return
  emit('close')
}

function onSubmit(): void {
  const hosts = parseHosts()
  const invalid = validate(hosts)
  if (invalid) {
    formError.value = invalid
    return
  }

  const payload: EnvironmentPayload = {
    name: name.value.trim(),
    code: code.value.trim(),
    category_id: categoryId.value,
    tag_ids: tagIds.value,
    type: type.value,
    hosts,
    os: os.value.trim(),
    description: description.value.trim(),
  }
  if (sshHostId.value) payload.ssh_host_id = sshHostId.value
  if (certId.value) payload.cert_id = certId.value
  if (props.environment) payload.version = props.environment.version

  formError.value = ''
  emit('submit', payload)
}
</script>
