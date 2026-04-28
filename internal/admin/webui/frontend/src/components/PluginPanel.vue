<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { APIError, requestJSON } from '../lib/http'
import SchemaConfigEditor from './SchemaConfigEditor.vue'
import type {
  PluginConfigSaveResult,
  PluginDetail,
  PluginInstallResult,
  PluginSnapshot,
  RuntimeConfig,
  RuntimeStatus,
  WebUIBootstrap,
} from '../types/api'

type PluginFilter = 'all' | 'enabled' | 'disabled' | 'builtin' | 'external'
type PluginInstallLogTone = 'info' | 'success' | 'error'
type PluginInstallLogEntry = {
  at: string
  tone: PluginInstallLogTone
  text: string
}

const props = defineProps<{
  runtimeConfig: RuntimeConfig
  bootstrap: WebUIBootstrap | null
  busy: boolean
}>()

const emit = defineEmits<{
  busy: [value: boolean]
  unauthorized: []
  refresh: []
  notice: [payload: { kind: 'success' | 'error' | 'info'; title: string; text: string }]
}>()

const selectedFilter = ref<PluginFilter>('all')
const search = ref('')
const selectedPluginId = ref('')
const detail = ref<PluginDetail | null>(null)
const loadingDetail = ref(false)
const uploadInput = ref<HTMLInputElement | null>(null)
const installLogViewer = ref<HTMLDivElement | null>(null)
const installingPlugin = ref(false)
const installLogModalOpen = ref(false)
const installLogEntries = ref<PluginInstallLogEntry[]>([])
const installLogFileName = ref('')
const configEnabled = ref(false)
const configModalOpen = ref(false)
const detailError = ref('')

const plugins = computed(() => props.bootstrap?.plugins || [])
const selectedPlugin = computed(() => plugins.value.find((item) => item.id === selectedPluginId.value) || null)

const filteredPlugins = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  return [...plugins.value]
    .filter((item) => matchFilter(item, selectedFilter.value))
    .filter((item) => {
      if (!keyword) return true
      return [item.id, item.name, item.author, item.description, item.version]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(keyword))
    })
    .sort((left, right) => {
      if (isBuiltin(left) !== isBuiltin(right)) {
        return isBuiltin(left) ? -1 : 1
      }
      return (left.name || left.id).localeCompare(right.name || right.id)
    })
})

const filterChips = computed(() => {
  const list = plugins.value
  return [
    { value: 'all' as PluginFilter, label: '全部', count: list.length },
    { value: 'enabled' as PluginFilter, label: '已启用', count: list.filter((item) => item.configured && item.enabled).length },
    { value: 'disabled' as PluginFilter, label: '未启用', count: list.filter((item) => (item.configured || isBuiltin(item)) && !item.enabled).length },
    { value: 'builtin' as PluginFilter, label: '内置', count: list.filter((item) => isBuiltin(item)).length },
    { value: 'external' as PluginFilter, label: '外部', count: list.filter((item) => !isBuiltin(item)).length },
  ]
})

watch(
  () => props.bootstrap?.plugins,
  (newPlugins) => {
    const availableIDs = new Set((newPlugins || []).map((item) => item.id))
    if (!newPlugins?.length) {
      selectedPluginId.value = ''
      detail.value = null
      detailError.value = ''
      return
    }
    if (!selectedPluginId.value || !availableIDs.has(selectedPluginId.value)) {
      selectedPluginId.value = newPlugins[0].id
    }
  },
  { immediate: true },
)

watch(selectedPluginId, async (nextID) => {
  if (!nextID) {
    detail.value = null
    detailError.value = ''
    configEnabled.value = false
    return
  }
  if (!configModalOpen.value) return
  await loadPluginDetail(nextID)
})

function apiURL(path: string): string {
  return props.runtimeConfig.apiBasePath + path
}

function isBuiltin(snapshot: PluginSnapshot): boolean {
  return snapshot.builtin || snapshot.kind === 'builtin'
}

function presentPluginKind(snapshot: PluginSnapshot): string {
  if (isBuiltin(snapshot)) return '内置'
  if (snapshot.kind === 'external_exec') return '外部'
  return snapshot.kind || '未知'
}

function presentStatus(snapshot: PluginSnapshot, runtime?: RuntimeStatus): string {
  if (!snapshot.configured && isBuiltin(snapshot)) return '内置可用'
  if (!snapshot.configured) return '未安装'
  if (runtime?.circuit_open) return '熔断中'
  if (runtime?.restarting) return '恢复中'
  if (snapshot.state === 'running') return '运行中'
  if (snapshot.state === 'starting') return '启动中'
  if (snapshot.state === 'stopping') return '停止中'
  if (snapshot.state === 'failed' || snapshot.last_error) return '异常'
  if (snapshot.enabled) return '已启用'
  return '未启用'
}

function statusTone(snapshot: PluginSnapshot, runtime?: RuntimeStatus): 'success' | 'error' | 'info' {
  if (runtime?.circuit_open || snapshot.state === 'failed' || snapshot.last_error) return 'error'
  if (snapshot.state === 'running' || snapshot.enabled) return 'success'
  return 'info'
}

function matchFilter(snapshot: PluginSnapshot, filter: PluginFilter): boolean {
  if (filter === 'enabled') return snapshot.configured && snapshot.enabled
  if (filter === 'disabled') return (snapshot.configured || isBuiltin(snapshot)) && !snapshot.enabled
  if (filter === 'builtin') return isBuiltin(snapshot)
  if (filter === 'external') return !isBuiltin(snapshot)
  return true
}

function presentPluginAction(action: 'install' | 'start' | 'stop' | 'reload' | 'recover' | 'uninstall'): string {
  if (action === 'install') return '安装'
  if (action === 'start') return '启动'
  if (action === 'stop') return '停止'
  if (action === 'reload') return '重载'
  if (action === 'recover') return '恢复'
  return '删除'
}

function formatError(error: unknown, fallback: string): string {
  if (error instanceof APIError) {
    if (error.status === 401) {
      emit('unauthorized')
      return '登录状态已失效，请重新登录。'
    }
    return error.message || fallback
  }
  if (error instanceof Error) return error.message
  return fallback
}

function formatFileSize(size: number): string {
  if (!Number.isFinite(size) || size <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let value = size
  let unitIndex = 0
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024
    unitIndex++
  }
  return `${value.toFixed(unitIndex === 0 ? 0 : 2)} ${units[unitIndex]}`
}

function appendInstallLog(text: string, tone: PluginInstallLogTone = 'info') {
  installLogEntries.value.push({
    at: new Date().toLocaleTimeString(),
    tone,
    text,
  })
  void nextTick(() => {
    if (installLogViewer.value) {
      installLogViewer.value.scrollTop = installLogViewer.value.scrollHeight
    }
  })
}

function resetInstallLog(file: File) {
  installLogFileName.value = file.name
  installLogEntries.value = []
  installLogModalOpen.value = true
  appendInstallLog(`已选择插件包：${file.name}`)
  appendInstallLog(`文件大小：${formatFileSize(file.size)}`)
}

function closeInstallLogModal() {
  if (installingPlugin.value) return
  installLogModalOpen.value = false
}

async function loadPluginDetail(pluginID: string, preserveEditor = false) {
  loadingDetail.value = true
  detailError.value = ''
  try {
    const data = await requestJSON<PluginDetail>(apiURL(`/plugins/${encodeURIComponent(pluginID)}`))
    detail.value = data
    if (!preserveEditor) {
      configEnabled.value = data.snapshot.enabled
    }
  } catch (error) {
    detail.value = null
    detailError.value = formatError(error, '加载插件详情失败。')
  } finally {
    loadingDetail.value = false
  }
}

async function invokePluginActionFor(snapshot: PluginSnapshot, action: 'install' | 'start' | 'stop' | 'reload' | 'recover' | 'uninstall') {
  if (!snapshot || props.busy) return
  if (action === 'uninstall') {
    const confirmed = window.confirm(`确定永久删除插件「${snapshot.id}」吗？\n\n这会同时删除配置和 plugins/ 目录下的文件。`)
    if (!confirmed) return
  }
  emit('busy', true)
  try {
    await requestJSON<Record<string, unknown>>(apiURL(`/plugins/${encodeURIComponent(snapshot.id)}/${action}`), {
      method: 'POST',
    })
    emit('notice', { kind: 'success', title: '插件操作已完成', text: `已执行${presentPluginAction(action)}：${snapshot.id}` })
    emit('refresh')
    if (action !== 'uninstall' && selectedPluginId.value === snapshot.id) {
      await loadPluginDetail(selectedPluginId.value)
    }
  } catch (error) {
    emit('notice', { kind: 'error', title: '插件操作失败', text: formatError(error, `执行插件${presentPluginAction(action)}失败。`) })
  } finally {
    emit('busy', false)
  }
}

function isPluginSwitchOn(snapshot: PluginSnapshot): boolean {
  return snapshot.enabled || snapshot.state === 'running' || snapshot.state === 'starting'
}

function isPluginSwitchBusy(snapshot: PluginSnapshot): boolean {
  return snapshot.state === 'starting' || snapshot.state === 'stopping'
}

function pluginSwitchTitle(snapshot: PluginSnapshot): string {
  if (isPluginSwitchOn(snapshot)) return `停止插件 ${snapshot.name || snapshot.id}`
  if (!isBuiltin(snapshot) && !snapshot.configured) return `安装插件 ${snapshot.name || snapshot.id}`
  return `启动插件 ${snapshot.name || snapshot.id}`
}

function pluginSwitchAction(snapshot: PluginSnapshot): 'install' | 'start' | 'stop' {
  if (isPluginSwitchOn(snapshot)) return 'stop'
  if (!isBuiltin(snapshot) && !snapshot.configured) return 'install'
  return 'start'
}

function canTogglePlugin(snapshot: PluginSnapshot): boolean {
  return !isPluginSwitchBusy(snapshot)
}

function canUninstallPlugin(snapshot: PluginSnapshot): boolean {
  return snapshot.configured && !isBuiltin(snapshot)
}

async function openPluginConfig(snapshot: PluginSnapshot) {
  configModalOpen.value = true
  detailError.value = ''
  if (selectedPluginId.value === snapshot.id) {
    await loadPluginDetail(snapshot.id)
    return
  }
  selectedPluginId.value = snapshot.id
}

function closePluginConfig() {
  configModalOpen.value = false
}

async function togglePluginCard(snapshot: PluginSnapshot) {
  if (!canTogglePlugin(snapshot)) return
  await invokePluginActionFor(snapshot, pluginSwitchAction(snapshot))
}

function openPluginPackagePicker() {
  if (props.busy || installingPlugin.value) return
  uploadInput.value?.click()
}

async function onPickPluginPackage(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  input.value = ''
  await installPluginPackage(file)
}

async function installPluginPackage(file: File) {
  if (props.busy || installingPlugin.value) return

  resetInstallLog(file)
  installingPlugin.value = true
  emit('busy', true)
  try {
    appendInstallLog('正在上传插件包到后台。')
    const formData = new FormData()
    formData.append('file', file, file.name)
    formData.append('overwrite', 'true')
    appendInstallLog('后台开始解析插件包并执行安装流程。')
    const result = await requestJSON<PluginInstallResult>(apiURL('/plugins/upload'), {
      method: 'POST',
      body: formData,
    })

    appendInstallLog(`插件 ID：${result.plugin_id || '-'}`, 'success')
    appendInstallLog(`插件类型：${result.kind || '-'}`)
    if (result.format) appendInstallLog(`插件包格式：${result.format}`)
    if (result.installed_to) appendInstallLog(`安装目录：${result.installed_to}`)
    if (result.manifest_path) appendInstallLog(`清单文件：${result.manifest_path}`)
    if (result.backup_path) appendInstallLog(`旧版本备份：${result.backup_path}`)
    if (result.dependency_env_path) appendInstallLog(`依赖环境：${result.dependency_env_path}`)
    appendInstallLog(result.dependencies_installed ? '外部依赖已安装。' : '未发现需要安装的外部依赖。')
    appendInstallLog(result.reloaded ? '插件清单已刷新。' : '插件清单未刷新。', result.reloaded ? 'success' : 'info')

    const pluginID = String(result.plugin_id || '').trim()
    if (!pluginID) {
      throw new Error('后台没有返回插件 ID，无法写入安装配置。')
    }
    appendInstallLog('正在写入插件安装配置。')
    await requestJSON<Record<string, unknown>>(apiURL(`/plugins/${encodeURIComponent(pluginID)}/install`), {
      method: 'POST',
    })
    appendInstallLog('插件已安装为未启用状态，可在插件卡片上启动。', 'success')
    appendInstallLog(result.message || `${result.plugin_id || file.name} 已安装完成。`, 'success')
    emit('notice', {
      kind: 'success',
      title: result.replaced ? '插件已覆盖' : '插件已安装',
      text: result.message || `${result.plugin_id || file.name} 已可在管理后台中使用。`,
    })
    appendInstallLog('正在刷新插件列表。')
    emit('refresh')
  } catch (error) {
    const message = formatError(error, '安装插件失败。')
    appendInstallLog(message, 'error')
    emit('notice', { kind: 'error', title: '安装失败', text: message })
  } finally {
    installingPlugin.value = false
    appendInstallLog('安装流程已结束。')
    emit('busy', false)
  }
}

async function savePluginConfig(payload: { enabled: boolean; config: Record<string, unknown> }) {
  const snapshot = selectedPlugin.value
  if (!snapshot || props.busy) return

  emit('busy', true)
  try {
    const result = await requestJSON<PluginConfigSaveResult>(apiURL(`/plugins/${encodeURIComponent(snapshot.id)}/config`), {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(payload),
    })

    if (result.detail) {
      detail.value = result.detail
      configEnabled.value = result.detail.snapshot.enabled
    }
    configModalOpen.value = false
    emit('notice', { kind: 'success', title: '插件配置已保存', text: result.message || `${snapshot.id} 的配置已保存。` })
    emit('refresh')
  } catch (error) {
    emit('notice', { kind: 'error', title: '保存失败', text: formatError(error, '保存插件配置失败。') })
  } finally {
    emit('busy', false)
  }
}
</script>

<template>
  <div class="plugin-panel-wrapper">
    <section class="plugin-workbench-grid">
      <section class="card plugin-list-card">
        <div class="section-head compact">
          <div>
            <span class="eyebrow">插件目录</span>
            <h3>插件列表</h3>
          </div>
          <div class="plugin-list-head-actions">
            <span class="muted">{{ filteredPlugins.length }} / {{ plugins.length }}</span>
            <input
              ref="uploadInput"
              class="plugin-upload-input"
              type="file"
              accept=".zip,.tgz,.tar.gz,application/zip,application/gzip"
              @change="onPickPluginPackage"
            />
            <button class="primary-btn plugin-install-btn" type="button" :disabled="busy || installingPlugin" @click="openPluginPackagePicker">
              {{ installingPlugin ? '安装中...' : '安装插件' }}
            </button>
          </div>
        </div>

        <div class="toolbar-row plugin-toolbar-row">
          <input v-model="search" class="search-input" type="search" placeholder="按 ID、名称、作者或版本搜索" />
          <div class="filter-row">
            <button
              v-for="chip in filterChips"
              :key="chip.value"
              class="chip"
              :class="{ active: selectedFilter === chip.value }"
              type="button"
              @click="selectedFilter = chip.value"
            >
              {{ chip.label }} <span>{{ chip.count }}</span>
            </button>
          </div>
        </div>

        <div v-if="!filteredPlugins.length" class="empty-state">
          当前筛选条件下没有匹配的插件。
        </div>

        <div v-else class="plugin-list plugin-list-scroll">
          <article
            v-for="plugin in filteredPlugins"
            :key="plugin.id"
            class="plugin-card plugin-row-card"
            :class="{ active: selectedPluginId === plugin.id }"
            role="button"
            tabindex="0"
            @click="selectedPluginId = plugin.id"
            @keydown.enter.prevent="selectedPluginId = plugin.id"
            @keydown.space.prevent="selectedPluginId = plugin.id"
          >
            <div class="plugin-card-actions">
              <button
                class="plugin-switch-button"
                :class="{ active: isPluginSwitchOn(plugin) }"
                type="button"
                :disabled="busy || !canTogglePlugin(plugin)"
                :aria-label="pluginSwitchTitle(plugin)"
                :title="pluginSwitchTitle(plugin)"
                @click.stop="togglePluginCard(plugin)"
              >
                <span class="plugin-switch-thumb"></span>
              </button>
              <div class="plugin-icon-actions">
                <button
                  class="plugin-icon-btn"
                  type="button"
                  :aria-label="`配置插件 ${plugin.name || plugin.id}`"
                  :title="`配置插件 ${plugin.name || plugin.id}`"
                  @click.stop="openPluginConfig(plugin)"
                >
                  <svg viewBox="0 0 24 24" aria-hidden="true">
                    <path d="M12 15.5A3.5 3.5 0 1 0 12 8a3.5 3.5 0 0 0 0 7.5Z" />
                    <path d="M19.4 13.5a7.8 7.8 0 0 0 0-3l2-1.5-2-3.4-2.4 1a8.6 8.6 0 0 0-2.6-1.5L14 2.5h-4l-.4 2.6A8.6 8.6 0 0 0 7 6.6l-2.4-1-2 3.4 2 1.5a7.8 7.8 0 0 0 0 3l-2 1.5 2 3.4 2.4-1a8.6 8.6 0 0 0 2.6 1.5l.4 2.6h4l.4-2.6a8.6 8.6 0 0 0 2.6-1.5l2.4 1 2-3.4-2-1.5Z" />
                  </svg>
                </button>
                <button
                  class="plugin-icon-btn danger"
                  type="button"
                  :disabled="busy || !canUninstallPlugin(plugin)"
                  :aria-label="`卸载插件 ${plugin.name || plugin.id}`"
                  :title="`卸载插件 ${plugin.name || plugin.id}`"
                  @click.stop="invokePluginActionFor(plugin, 'uninstall')"
                >
                  <svg viewBox="0 0 24 24" aria-hidden="true">
                    <path d="M9 3h6l1 2h4v2H4V5h4l1-2Z" />
                    <path d="M6 9h12l-.8 11H6.8L6 9Zm4 2v7h2v-7h-2Zm4 0v7h2v-7h-2Z" />
                  </svg>
                </button>
              </div>
            </div>
            <div class="plugin-card-head">
              <div>
                <strong>{{ plugin.name || plugin.id }}</strong>
                <span>{{ plugin.id }} · {{ presentPluginKind(plugin) }}</span>
              </div>
              <span class="status-pill" :class="`status-${statusTone(plugin)}`">
                {{ presentStatus(plugin) }}
              </span>
            </div>
            <p>{{ plugin.description || '当前没有可展示的插件说明。' }}</p>
            <div class="plugin-card-meta">
              <span>{{ plugin.version || 'v0.0.0' }}</span>
              <span>{{ plugin.author || '未知作者' }}</span>
              <span>{{ plugin.enabled ? '已启用' : '未启用' }}</span>
            </div>
            <p v-if="plugin.last_error" class="error-copy">{{ plugin.last_error }}</p>
          </article>
        </div>
      </section>

    </section>

    <div v-if="configModalOpen" class="plugin-modal-backdrop" role="presentation" @click.self="closePluginConfig">
      <section class="plugin-config-modal" role="dialog" aria-modal="true" :aria-label="`配置插件 ${selectedPlugin?.name || selectedPlugin?.id || ''}`">
        <div class="plugin-modal-head">
          <div>
            <span class="eyebrow">插件配置</span>
            <h3>{{ selectedPlugin?.name || selectedPlugin?.id || '插件配置' }}</h3>
            <p>{{ selectedPlugin?.description || '编辑插件启用状态和运行参数。' }}</p>
          </div>
          <button class="plugin-modal-close" type="button" aria-label="关闭配置弹窗" title="关闭" @click="closePluginConfig">
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path d="m6.4 5 12.6 12.6-1.4 1.4L5 6.4 6.4 5Z" />
              <path d="M17.6 5 19 6.4 6.4 19 5 17.6 17.6 5Z" />
            </svg>
          </button>
        </div>

        <div v-if="detailError" class="banner banner-danger">
          <strong>详情加载失败</strong>
          <span>{{ detailError }}</span>
        </div>

        <div v-if="loadingDetail" class="empty-state compact">正在加载插件配置...</div>

        <template v-else-if="detail">
          <div v-if="detail.snapshot.last_error || detail.runtime?.last_error || detail.runtime?.circuit_open" class="banner banner-danger">
            <strong>插件异常</strong>
            <span>{{ detail.runtime?.last_error || detail.snapshot.last_error || detail.runtime?.circuit_reason }}</span>
          </div>

          <SchemaConfigEditor
            v-model:enabled="configEnabled"
            :config="detail.config || {}"
            :schema="detail.config_schema"
            :busy="busy"
            @save="savePluginConfig"
            @reset="selectedPlugin && loadPluginDetail(selectedPlugin.id)"
          />
        </template>

        <div v-else class="empty-state compact">没有可编辑的插件配置。</div>
      </section>
    </div>

    <div v-if="installLogModalOpen" class="plugin-modal-backdrop" role="presentation" @click.self="closeInstallLogModal">
      <section class="plugin-install-log-modal" role="dialog" aria-modal="true" :aria-label="`安装插件 ${installLogFileName}`">
        <div class="plugin-modal-head">
          <div>
            <span class="eyebrow">安装日志</span>
            <h3>{{ installLogFileName || '插件安装' }}</h3>
            <p>选择插件包后自动安装，下面显示本次安装流程日志。</p>
          </div>
          <button
            class="plugin-modal-close"
            type="button"
            aria-label="关闭安装日志"
            title="关闭"
            :disabled="installingPlugin"
            @click="closeInstallLogModal"
          >
            <svg viewBox="0 0 24 24" aria-hidden="true">
              <path d="m6.4 5 12.6 12.6-1.4 1.4L5 6.4 6.4 5Z" />
              <path d="M17.6 5 19 6.4 6.4 19 5 17.6 17.6 5Z" />
            </svg>
          </button>
        </div>

        <div class="plugin-install-log-status">
          <span class="status-pill" :class="installingPlugin ? 'status-info' : 'status-success'">
            {{ installingPlugin ? '安装中' : '已结束' }}
          </span>
          <span class="muted">{{ installLogEntries.length }} 条日志</span>
        </div>

        <div ref="installLogViewer" class="plugin-install-log-viewer" role="log" aria-live="polite">
          <div
            v-for="(entry, index) in installLogEntries"
            :key="`${entry.at}-${index}`"
            class="plugin-install-log-line"
            :class="`tone-${entry.tone}`"
          >
            <time>{{ entry.at }}</time>
            <span>{{ entry.text }}</span>
          </div>
        </div>

        <div class="plugin-install-log-footer">
          <button class="secondary-btn" type="button" :disabled="installingPlugin" @click="closeInstallLogModal">
            {{ installingPlugin ? '安装中...' : '关闭' }}
          </button>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.plugin-panel-wrapper {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.plugin-list-card {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.plugin-workbench-grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: 18px;
  align-items: start;
}

.plugin-toolbar-row {
  margin-bottom: 0;
}

.plugin-list-head-actions {
  display: inline-flex;
  align-items: center;
  justify-content: flex-end;
  gap: 12px;
}

.plugin-upload-input {
  display: none;
}

.plugin-install-btn {
  min-height: 38px;
  padding-inline: 16px;
  white-space: nowrap;
}

.plugin-list-scroll {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
  max-height: min(86vh, 1040px);
  overflow: auto;
  padding: 2px 6px 2px 2px;
}

.plugin-row-card {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 196px;
  background: var(--card-bg);
  cursor: pointer;
}

.plugin-row-card:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px var(--selection-shadow);
}

.plugin-card-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.plugin-switch-button,
.plugin-icon-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border: 1px solid var(--soft-border);
  background: var(--surface-soft-alt);
  color: var(--text-secondary);
  cursor: pointer;
  transition:
    background 180ms ease,
    border-color 180ms ease,
    color 180ms ease,
    box-shadow 180ms ease;
}

.plugin-switch-button:disabled,
.plugin-icon-btn:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.plugin-switch-button {
  width: 48px;
  height: 28px;
  padding: 3px;
  border-radius: 999px;
}

.plugin-switch-button.active {
  border-color: transparent;
  background: var(--control-active-bg);
}

.plugin-switch-button:not(:disabled):hover,
.plugin-icon-btn:not(:disabled):hover {
  border-color: var(--selection-border);
  background: var(--selection-bg);
  color: var(--accent-strong);
}

.plugin-switch-button.active:not(:disabled):hover {
  border-color: transparent;
  background: var(--control-active-hover-bg);
  color: var(--button-primary-text);
}

.plugin-switch-button:not(:disabled):focus-visible,
.plugin-icon-btn:not(:disabled):focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px var(--selection-shadow);
}

.plugin-switch-thumb {
  display: block;
  width: 20px;
  height: 20px;
  border-radius: 999px;
  background: var(--control-thumb-bg);
  box-shadow: var(--control-thumb-shadow);
  transform: translateX(-9px);
  transition: transform 180ms ease;
}

.plugin-switch-button.active .plugin-switch-thumb {
  transform: translateX(9px);
}

.plugin-icon-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.plugin-icon-btn {
  width: 34px;
  height: 34px;
  padding: 0;
  border-radius: 12px;
}

.plugin-icon-btn svg {
  width: 17px;
  height: 17px;
  fill: currentColor;
}

.plugin-icon-btn.danger:not(:disabled) {
  color: var(--danger-text);
}

.plugin-icon-btn.danger:not(:disabled):hover {
  border-color: var(--danger-border);
  background: var(--danger-bg-soft);
}

.plugin-row-card p {
  display: -webkit-box;
  overflow: hidden;
  line-height: 1.6;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 3;
}

.plugin-card-head {
  gap: 10px;
}

.plugin-card-head > div {
  min-width: 0;
}

.plugin-card-head strong,
.plugin-card-head span {
  overflow: hidden;
  text-overflow: ellipsis;
}

.plugin-card-head span {
  white-space: nowrap;
}

.plugin-card-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: auto;
}

.plugin-card-meta span {
  color: var(--text-muted);
  font-size: 12px;
}

.plugin-modal-backdrop {
  position: fixed;
  inset: 0;
  z-index: 80;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 28px;
  background: rgba(15, 23, 42, 0.46);
  backdrop-filter: blur(10px);
}

.plugin-config-modal {
  width: min(960px, 100%);
  max-height: min(86vh, 920px);
  overflow: auto;
  padding: 22px;
  border: 1px solid var(--soft-border);
  border-radius: 26px;
  background: var(--card-bg);
  box-shadow: var(--shadow-strong);
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.plugin-install-log-modal {
  width: min(820px, 100%);
  max-height: min(86vh, 820px);
  overflow: hidden;
  padding: 22px;
  border: 1px solid var(--soft-border);
  border-radius: 26px;
  background: var(--card-bg);
  box-shadow: var(--shadow-strong);
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.plugin-install-log-status,
.plugin-install-log-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.plugin-install-log-viewer {
  min-height: 260px;
  max-height: min(52vh, 480px);
  overflow: auto;
  border-radius: 20px;
  border: 1px solid var(--soft-border);
  background: var(--code-surface);
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.plugin-install-log-line {
  display: grid;
  grid-template-columns: 84px minmax(0, 1fr);
  gap: 10px;
  color: var(--text-secondary);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', monospace;
  font-size: 12px;
  line-height: 1.7;
}

.plugin-install-log-line time {
  color: var(--text-muted);
}

.plugin-install-log-line span {
  min-width: 0;
  overflow-wrap: anywhere;
}

.plugin-install-log-line.tone-success span {
  color: var(--success-text);
}

.plugin-install-log-line.tone-error span {
  color: var(--danger-text);
}

.plugin-install-log-footer {
  justify-content: flex-end;
}

.plugin-modal-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
}

.plugin-modal-head h3,
.plugin-modal-head p {
  margin: 0;
}

.plugin-modal-head p {
  margin-top: 6px;
  color: var(--text-soft);
  line-height: 1.6;
}

.plugin-modal-close {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 auto;
  width: 38px;
  height: 38px;
  border: 1px solid var(--soft-border);
  border-radius: 14px;
  background: var(--surface-soft-alt);
  color: var(--text-secondary);
  cursor: pointer;
  transition:
    background-color 180ms ease,
    border-color 180ms ease,
    color 180ms ease,
    box-shadow 180ms ease;
}

.plugin-modal-close:not(:disabled):hover {
  border-color: var(--selection-border);
  background: var(--selection-bg);
  color: var(--accent-strong);
}

.plugin-modal-close:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.plugin-modal-close:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px var(--selection-shadow);
}

.plugin-modal-close svg {
  width: 18px;
  height: 18px;
  fill: currentColor;
}

.plugin-config-modal :deep(.editor-card) {
  border-radius: 20px;
}

@media (max-width: 1220px) {
  .plugin-list-scroll {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 760px) {
  .plugin-list-head-actions {
    align-items: flex-end;
    flex-direction: column;
    gap: 8px;
  }

  .plugin-modal-backdrop {
    align-items: stretch;
    padding: 12px;
  }

  .plugin-config-modal {
    max-height: calc(100vh - 24px);
    padding: 16px;
    border-radius: 22px;
  }

  .plugin-install-log-modal {
    max-height: calc(100vh - 24px);
    padding: 16px;
    border-radius: 22px;
  }

  .plugin-install-log-status {
    align-items: flex-start;
    flex-direction: column;
  }

  .plugin-install-log-line {
    grid-template-columns: 1fr;
    gap: 2px;
  }

  .plugin-list-scroll {
    grid-template-columns: 1fr;
    max-height: none;
  }
}
</style>
