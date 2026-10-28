<template>
  <div class="clean-config">
    <div class="cfg-block">
      <span class="ap-field-label">内置清理动作（可多选）</span>
      <div class="action-list">
        <label v-for="action in CLEAN_ACTION_OPTIONS" :key="action.value" class="check">
          <input
            type="checkbox"
            :checked="modelValue.actions.includes(action.value)"
            :disabled="disabled"
            @change="onToggleAction(action.value, $event)"
          />
          <span class="action-label">{{ action.label }}</span>
          <span class="ap-muted action-hint">{{ action.hint }}</span>
        </label>
      </div>
    </div>

    <div class="cfg-row">
      <label class="ap-field">
        <span class="ap-field-label">执行时机</span>
        <select :value="modelValue.timing" :disabled="disabled" @change="onTiming">
          <option value="auto">自动（任务成功即执行）</option>
          <option value="manual">人工确认（成功也需确认）</option>
        </select>
      </label>
      <p class="ap-alert ap-alert-hint timing-hint">
        任务失败/取消时按平台规则保留现场，需在任务详情「确认清理」后执行；成功任务默认自动清理。
      </p>
    </div>

    <div class="cfg-block">
      <div class="cfg-block-head">
        <span class="ap-field-label">用户清理脚本（经可信 SSH/Host 远程执行）</span>
        <button
          type="button"
          class="ap-btn ap-btn-ghost ap-btn-sm"
          :disabled="disabled || (modelValue.scripts ?? []).length >= 10"
          @click="addScript"
        >
          添加脚本
        </button>
      </div>

      <p v-if="!canLoadHosts" class="ap-muted">
        无「SSH 与 Host 管理」权限，无法选择脚本执行主机（内置动作不受影响）
      </p>
      <p v-else-if="hostsLoading" class="ap-muted">加载可信主机…</p>
      <p v-else-if="hostsError" class="ap-muted">{{ hostsError }}</p>

      <table v-if="(modelValue.scripts ?? []).length > 0" class="ap-table script-table">
        <thead>
          <tr>
            <th>可信主机</th>
            <th>脚本路径</th>
            <th>SHA256（可选）</th>
            <th>操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="(script, index) in modelValue.scripts" :key="index">
            <td>
              <!-- hosts 到位前不渲染 select，避免原生 select 在选项缺失时丢掉已选值。 -->
              <select
                v-if="hosts.length > 0"
                :value="script.host_id"
                :disabled="disabled || !canLoadHosts"
                @change="onScriptHost(index, $event)"
              >
                <option value="">请选择主机</option>
                <option v-for="host in hosts" :key="host.id" :value="host.id">
                  {{ host.host }}<template v-if="host.port">:{{ host.port }}</template>
                </option>
              </select>
              <span v-else class="ap-muted">无可信主机</span>
            </td>
            <td>
              <input
                :value="script.path"
                class="cell-input"
                placeholder="/opt/cleanup.sh"
                :disabled="disabled"
                @input="onScriptPath(index, $event)"
              />
            </td>
            <td>
              <input
                :value="script.sha256 ?? ''"
                class="cell-input"
                placeholder="64 位十六进制（可选）"
                :disabled="disabled"
                @input="onScriptSha(index, $event)"
              />
            </td>
            <td>
              <button
                type="button"
                class="ap-btn ap-btn-danger ap-btn-sm"
                :disabled="disabled"
                @click="removeScript(index)"
              >
                删除
              </button>
            </td>
          </tr>
        </tbody>
      </table>
      <p v-else class="ap-muted">未添加脚本：仅执行内置动作</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { listSshHosts, type SshHostOption } from '@/api/setting'
import { useAuthStore } from '@/stores/auth'
import { CLEAN_ACTION_OPTIONS } from '@/utils/testing'
import type { CleanAction, CleanConfig, CleanScript } from '@/types'

// 环境清理配置（PRD §3.3.7 / 决策 #15）。
//   - 内置动作走受控清理助手（参数固定，不来自用户文本）；
//   - 用户脚本仅在可信 SSH 主机执行，并可声明内容 SHA256 进入白名单校验（DES-T-003）。
const props = defineProps<{
  modelValue: CleanConfig
  disabled?: boolean
}>()

const emit = defineEmits<{ (e: 'update:modelValue', value: CleanConfig): void }>()

const auth = useAuthStore()
const canLoadHosts = computed(() => auth.hasPerm('setting.ssh', 'write'))
const hosts = ref<SshHostOption[]>([])
const hostsLoading = ref(false)
const hostsError = ref('')

function patch(next: Partial<CleanConfig>): void {
  emit('update:modelValue', { ...props.modelValue, ...next })
}

/** 勾选/取消内置动作（保留已有 script 动作与脚本列表的一致性）。 */
function onToggleAction(action: CleanAction, event: Event): void {
  const checked = (event.target as HTMLInputElement).checked
  const set = new Set<CleanAction>(props.modelValue.actions.filter((item) => item !== 'script'))
  if (checked) set.add(action)
  else set.delete(action)
  const actions: CleanAction[] = Array.from(set)
  if ((props.modelValue.scripts ?? []).length > 0) actions.push('script')
  patch({ actions })
}

function onTiming(event: Event): void {
  const timing = (event.target as HTMLSelectElement).value === 'manual' ? 'manual' : 'auto'
  patch({ timing })
}

function addScript(): void {
  patch({
    scripts: [...props.modelValue.scripts, { host_id: hosts.value[0]?.id ?? '', path: '' }],
  })
}

function removeScript(index: number): void {
  const scripts = props.modelValue.scripts.filter((_, i) => i !== index)
  const actions: CleanAction[] = props.modelValue.actions.filter((item) => item !== 'script')
  if (scripts.length > 0) actions.push('script')
  patch({ scripts, actions })
}

function updateScript(index: number, next: Partial<CleanScript>): void {
  patch({
    scripts: props.modelValue.scripts.map((script, i) =>
      i === index ? { ...script, ...next } : script,
    ),
  })
}

function onScriptHost(index: number, event: Event): void {
  updateScript(index, { host_id: (event.target as HTMLSelectElement).value })
}

function onScriptPath(index: number, event: Event): void {
  updateScript(index, { path: (event.target as HTMLInputElement).value })
}

function onScriptSha(index: number, event: Event): void {
  updateScript(index, { sha256: (event.target as HTMLInputElement).value })
}

async function loadHosts(): Promise<void> {
  if (!canLoadHosts.value) return
  hostsLoading.value = true
  hostsError.value = ''
  try {
    const res = await listSshHosts()
    hosts.value = res.list
    if (res.list.length === 0) hostsError.value = '暂无可信主机，可在「SSH 与 Host 管理」中维护'
  } catch {
    hostsError.value = '加载 SSH/Host 列表失败'
  } finally {
    hostsLoading.value = false
  }
}

/** 可信主机到位后，为未选主机的脚本补上默认主机（首个可信主机）。 */
watch(hosts, (list) => {
  const first = list[0]
  if (!first) return
  if (!props.modelValue.scripts.some((script) => !script.host_id)) return
  patch({
    scripts: props.modelValue.scripts.map((script) =>
      script.host_id ? script : { ...script, host_id: first.id },
    ),
  })
})

onMounted(() => {
  void loadHosts()
})
</script>

<style scoped>
.clean-config {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.cfg-block {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 8px 10px;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  background: #fcfcfd;
}

.cfg-block-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.cfg-row {
  display: flex;
  gap: 12px;
  align-items: flex-start;
  flex-wrap: wrap;
}

.timing-hint {
  flex: 1;
  min-width: 240px;
}

.action-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.check {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: #374151;
  cursor: pointer;
}

.action-label {
  min-width: 72px;
}

.action-hint {
  font-size: 12px;
}

.script-table {
  font-size: 13px;
}

.script-table select,
.script-table .cell-input {
  width: 100%;
  padding: 5px 8px;
  border: 1px solid #d1d5db;
  border-radius: 6px;
  font-size: 13px;
  font-family: inherit;
}
</style>
