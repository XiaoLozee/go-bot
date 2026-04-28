<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { APIError, requestJSON } from '../lib/http'
import type {
  ConnectionConfig,
  ConnectionDetail,
  ConnectionSaveResult,
  ConnectionSnapshot,
  RuntimeConfig,
  WebUIBootstrap,
} from '../types/api'

type NoticeKind = 'success' | 'error' | 'info'
type ConnectionFilter = 'all' | 'ws_server' | 'ws_reverse' | 'http_callback'

const props = defineProps<{
  runtimeConfig: RuntimeConfig
  bootstrap: WebUIBootstrap | null
  busy: boolean
}>()

const emit = defineEmits<{
  busy: [value: boolean]
  refresh: []
  notice: [value: { kind: NoticeKind; title: string; text: string }]
  unauthorized: []
}>()

const selectedConnectionId = ref('')
const selectedFilter = ref<ConnectionFilter>('all')
const search = ref('')
const detail = ref<ConnectionDetail | null>(null)
const detailError = ref('')
const loadingDetail = ref(false)
const editorMode = ref<'create' | 'edit'>('create')
const editorModalOpen = ref(false)
const formError = ref('')

const draft = reactive<ConnectionConfig>(buildDefaultConnectionConfig('ws_server'))

const connections = computed(() => props.bootstrap?.connections || [])
const selectedSnapshot = computed(() => connections.value.find((item) => item.id === selectedConnectionId.value) || null)
const selectedTypeMeta = computed(() => describeConnectionType(draft.ingress.type))
const actionType = computed(() => inferActionType(draft.ingress.type, draft.action.type))
const usesHTTPAction = computed(() => actionType.value === 'napcat_http')
const filteredConnections = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  return [...connections.value]
    .filter((item) => selectedFilter.value === 'all' || item.ingress_type === selectedFilter.value)
    .filter((item) => {
      if (!keyword) return true
      return [item.id, item.platform, item.ingress_type, item.action_type, item.self_id, item.self_nickname, item.last_error]
        .filter(Boolean)
        .some((value) => String(value).toLowerCase().includes(keyword))
    })
    .sort((left, right) => left.id.localeCompare(right.id))
})
const filterChips = computed(() => {
  const list = connections.value
  return connectionTypeOptions().map((item) => ({
    ...item,
    count: item.value === 'all' ? list.length : list.filter((conn) => conn.ingress_type === item.value).length,
  }))
})
watch(
  () => props.bootstrap?.connections,
  (items) => {
    const list = items || []
    if (!list.length) {
      selectedConnectionId.value = ''
      detail.value = null
      startCreate('ws_server')
      return
    }
    if (!selectedConnectionId.value || !list.some((item) => item.id === selectedConnectionId.value)) {
      if (editorMode.value === 'create' && editorModalOpen.value) return
      selectConnection(list[0].id)
    }
  },
  { immediate: true },
)

watch(
  () => draft.ingress.type,
  (type) => {
    draft.action.type = inferActionType(type, draft.action.type)
    if (type === 'ws_reverse') {
      draft.ingress.listen = ''
      draft.ingress.path = ''
      draft.ingress.retry_interval_ms = draft.ingress.retry_interval_ms || 30000
    } else {
      draft.ingress.url = ''
      draft.ingress.path = draft.ingress.path || (type === 'http_callback' ? '/callback' : '/ws')
    }
    if (draft.action.type === 'onebot_ws') {
      draft.action.base_url = ''
    }
  },
)

function apiURL(path: string): string {
  return props.runtimeConfig.apiBasePath + path
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

function formatDateTime(value?: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function connectionTypeOptions(): Array<{ value: ConnectionFilter; label: string }> {
  return [
    { value: 'all', label: '全部' },
    { value: 'ws_server', label: 'WebSocket 服务端' },
    { value: 'ws_reverse', label: 'WebSocket 客户端' },
    { value: 'http_callback', label: 'HTTP 回调' },
  ]
}

function describeConnectionType(type: string) {
  switch (type) {
    case 'ws_server':
      return {
        label: 'WebSocket 服务端',
        note: 'NapCat 主动连接 Go-bot，动作请求复用当前 WebSocket 通道，并可附带访问令牌。',
        addressLabel: '监听地址',
        pathLabel: '接入路径',
        addressPlaceholder: ':3001',
        pathPlaceholder: '/ws',
      }
    case 'ws_reverse':
      return {
        label: 'WebSocket 客户端',
        note: 'Go-bot 主动连接远端 WebSocket 地址，动作请求复用当前通道，并可附带访问令牌。',
        addressLabel: '远端 URL',
        pathLabel: '重试间隔（毫秒）',
        addressPlaceholder: 'ws://127.0.0.1:8080/ws',
        pathPlaceholder: '30000',
      }
    case 'http_callback':
      return {
        label: 'HTTP 回调',
        note: 'Go-bot 监听 HTTP 回调，动作请求通过 NapCat HTTP API 发出。',
        addressLabel: '监听地址',
        pathLabel: '回调路径',
        addressPlaceholder: ':3002',
        pathPlaceholder: '/callback',
      }
    default:
      return {
        label: '网络',
        note: '请先选择接入类型，再继续保存。',
        addressLabel: '地址',
        pathLabel: '路径',
        addressPlaceholder: '',
        pathPlaceholder: '',
      }
  }
}

function presentIngressType(type?: string): string {
  return describeConnectionType(String(type || '')).label
}

function presentState(snapshot: ConnectionSnapshot): string {
  if (snapshot.last_error) return '异常'
  if (snapshot.online || snapshot.state === 'running') return '运行中'
  if (!snapshot.enabled) return '未启用'
  return snapshot.state || '未知'
}

function statusTone(snapshot: ConnectionSnapshot): 'success' | 'error' | 'info' {
  if (snapshot.last_error || snapshot.state === 'failed') return 'error'
  if (snapshot.online || snapshot.good || snapshot.state === 'running') return 'success'
  return 'info'
}

function isConnectionSwitchOn(snapshot: ConnectionSnapshot): boolean {
  return !!snapshot.enabled
}

function connectionSwitchTitle(snapshot: ConnectionSnapshot): string {
  if (isConnectionSwitchOn(snapshot)) return `停用连接 ${snapshot.id}`
  return `启用连接 ${snapshot.id}`
}

function connectionSwitchAction(snapshot: ConnectionSnapshot): 'start' | 'stop' {
  return isConnectionSwitchOn(snapshot) ? 'stop' : 'start'
}

function presentConnectionAction(action: 'start' | 'stop'): string {
  if (action === 'start') return '启用'
  return '停用'
}

function inferActionType(ingressType: string, current?: string): string {
  const normalized = String(current || '').trim().toLowerCase()
  if (normalized === 'napcat_http') return 'napcat_http'
  if (normalized === 'onebot_ws' && ingressType !== 'http_callback') return 'onebot_ws'
  return ingressType === 'http_callback' ? 'napcat_http' : 'onebot_ws'
}

function buildDefaultConnectionConfig(type: string): ConnectionConfig {
  const ingressType = type || 'ws_server'
  return {
    id: '',
    enabled: true,
    platform: 'onebot_v11',
    ingress: {
      type: ingressType,
      listen: '',
      path: ingressType === 'ws_reverse' ? '' : ingressType === 'http_callback' ? '/callback' : '/ws',
      url: '',
      retry_interval_ms: 30000,
    },
    action: {
      type: ingressType === 'http_callback' ? 'napcat_http' : 'onebot_ws',
      base_url: '',
      timeout_ms: 10000,
      access_token: '',
    },
  }
}

function replaceDraft(value: ConnectionConfig) {
  draft.id = value.id || ''
  draft.enabled = value.enabled !== false
  draft.platform = value.platform || 'onebot_v11'
  draft.ingress = {
    type: value.ingress?.type || 'ws_server',
    listen: value.ingress?.listen || '',
    url: value.ingress?.url || '',
    path: value.ingress?.path || '',
    retry_interval_ms: Number(value.ingress?.retry_interval_ms || 30000),
  }
  draft.action = {
    type: inferActionType(draft.ingress.type, value.action?.type),
    base_url: value.action?.base_url || '',
    timeout_ms: Number(value.action?.timeout_ms || 10000),
    access_token: value.action?.access_token || '',
  }
}

function buildPayload(): { value?: ConnectionConfig; error?: string } {
  const id = draft.id.trim()
  const ingressType = draft.ingress.type.trim()
  const timeoutMS = Number(draft.action.timeout_ms || 10000)
  const retryMS = Number(draft.ingress.retry_interval_ms || 30000)
  const nextActionType = inferActionType(ingressType, draft.action.type)
  const baseURL = String(draft.action.base_url || '').trim()

  if (!id) return { error: '连接 ID 不能为空。' }
  if (!ingressType) return { error: '接入类型不能为空。' }
  if (!Number.isFinite(timeoutMS) || timeoutMS <= 0) return { error: '动作超时时间必须大于 0。' }
  if (nextActionType === 'napcat_http' && !baseURL) return { error: 'NapCat HTTP API 基础地址不能为空。' }

  const payload: ConnectionConfig = {
    id,
    enabled: !!draft.enabled,
    platform: draft.platform || 'onebot_v11',
    ingress: { type: ingressType },
    action: {
      type: nextActionType,
      timeout_ms: Math.trunc(timeoutMS),
      access_token: String(draft.action.access_token || '').trim(),
    },
  }

  if (nextActionType === 'napcat_http') {
    payload.action.base_url = baseURL
  }

  if (ingressType === 'ws_reverse') {
    const url = String(draft.ingress.url || '').trim()
    if (!url) return { error: '远端 WebSocket URL 不能为空。' }
    if (!Number.isFinite(retryMS) || retryMS <= 0) return { error: '重试间隔必须大于 0。' }
    payload.ingress.url = url
    payload.ingress.retry_interval_ms = Math.trunc(retryMS)
  } else {
    const listen = String(draft.ingress.listen || '').trim()
    if (!listen) return { error: '监听地址不能为空。' }
    payload.ingress.listen = listen
    payload.ingress.path = String(draft.ingress.path || '').trim()
  }

  return { value: payload }
}

function startCreate(type: string) {
  selectedConnectionId.value = ''
  detail.value = null
  editorMode.value = 'create'
  formError.value = ''
  detailError.value = ''
  replaceDraft(buildDefaultConnectionConfig(type))
}

function openCreateConnection(type: string) {
  startCreate(type)
  editorModalOpen.value = true
}

async function openConnectionEditor(id: string) {
  editorModalOpen.value = true
  await selectConnection(id)
}

function closeConnectionEditor() {
  editorModalOpen.value = false
}

async function selectConnection(id: string) {
  selectedConnectionId.value = id
  editorMode.value = 'edit'
  await loadConnectionDetail(id)
}

async function loadConnectionDetail(id: string) {
  if (!id) return
  loadingDetail.value = true
  detailError.value = ''
  try {
    const data = await requestJSON<ConnectionDetail>(apiURL(`/connections/${encodeURIComponent(id)}`))
    detail.value = data
    replaceDraft(data.config)
  } catch (error) {
    detail.value = null
    detailError.value = formatError(error, '加载连接详情失败。')
  } finally {
    loadingDetail.value = false
  }
}

async function saveConnection() {
  if (props.busy) return
  const built = buildPayload()
  if (built.error || !built.value) {
    formError.value = built.error || '连接配置无效。'
    return
  }
  formError.value = ''
  emit('busy', true)
  try {
    const result = await requestJSON<ConnectionSaveResult>(apiURL('/connections'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(built.value),
    })
    if (result.detail) {
      detail.value = result.detail
      selectedConnectionId.value = result.detail.snapshot.id
      replaceDraft(result.detail.config)
      editorMode.value = 'edit'
    }
    editorModalOpen.value = false
    emit('notice', {
      kind: result.hot_apply_error ? 'error' : 'success',
      title: result.hot_apply_error ? '连接已保存（带警告）' : '连接已保存',
      text: result.message || `${built.value.id} 已保存。`,
    })
    emit('refresh')
  } catch (error) {
    emit('notice', { kind: 'error', title: '连接保存失败', text: formatError(error, '保存连接失败。') })
  } finally {
    emit('busy', false)
  }
}

async function probeConnection() {
  await probeConnectionById(selectedConnectionId.value)
}

async function probeConnectionById(id: string) {
  if (!id || props.busy) return
  selectedConnectionId.value = id
  emit('busy', true)
  try {
    const data = await requestJSON<ConnectionDetail>(apiURL(`/connections/${encodeURIComponent(id)}/probe`), {
      method: 'POST',
    })
    detail.value = data
    replaceDraft(data.config)
    editorMode.value = 'edit'
    emit('notice', { kind: 'success', title: '探测完成', text: `${id} 状态已刷新。` })
    emit('refresh')
  } catch (error) {
    emit('notice', { kind: 'error', title: '探测失败', text: formatError(error, '探测连接失败。') })
  } finally {
    emit('busy', false)
  }
}

async function toggleConnectionCard(snapshot: ConnectionSnapshot) {
  if (!snapshot.id || props.busy) return
  const action = connectionSwitchAction(snapshot)
  selectedConnectionId.value = snapshot.id
  emit('busy', true)
  try {
    const result = await requestJSON<ConnectionSaveResult>(apiURL(`/connections/${encodeURIComponent(snapshot.id)}/${action}`), {
      method: 'POST',
    })
    if (result.detail) {
      detail.value = result.detail
      if (editorModalOpen.value) {
        replaceDraft(result.detail.config)
        editorMode.value = 'edit'
      }
    }
    emit('notice', {
      kind: result.hot_apply_error ? 'error' : 'success',
      title: result.hot_apply_error ? '连接启停完成（带警告）' : '连接启停完成',
      text: result.message || `已${presentConnectionAction(action)}：${snapshot.id}`,
    })
    emit('refresh')
  } catch (error) {
    emit('notice', { kind: 'error', title: '连接启停失败', text: formatError(error, `执行连接${presentConnectionAction(action)}失败。`) })
  } finally {
    emit('busy', false)
  }
}

async function deleteConnection() {
  await deleteConnectionById(selectedConnectionId.value)
}

async function deleteConnectionById(id: string) {
  if (!id || props.busy) return
  const confirmed = window.confirm(`确定永久删除连接「${id}」吗？`)
  if (!confirmed) return
  emit('busy', true)
  try {
    await requestJSON<ConnectionSaveResult>(apiURL(`/connections/${encodeURIComponent(id)}/delete`), {
      method: 'POST',
    })
    emit('notice', { kind: 'success', title: '连接已删除', text: `${id} 已从配置中移除。` })
    if (selectedConnectionId.value === id) {
      selectedConnectionId.value = ''
      detail.value = null
      editorModalOpen.value = false
      startCreate('ws_server')
    }
    emit('refresh')
  } catch (error) {
    emit('notice', { kind: 'error', title: '删除失败', text: formatError(error, '删除连接失败。') })
  } finally {
    emit('busy', false)
  }
}
</script>

<template>
  <div class="connection-panel">
    <section class="connection-layout">
      <article class="card connection-list-card">
        <div class="section-head">
          <div>
            <span class="eyebrow">连接目录</span>
            <h3>网络适配器</h3>
          </div>
          <button class="primary-btn" type="button" :disabled="busy" @click="openCreateConnection('ws_server')">新建</button>
        </div>

        <div class="toolbar-row connection-toolbar-row">
          <input v-model="search" class="search-input" type="search" placeholder="按 ID、机器人账号、类型或报错搜索" />
          <div class="filter-row connection-filter-row">
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

        <div v-if="!filteredConnections.length" class="empty-state compact">当前没有匹配的连接。</div>
        <div v-else class="connection-list connection-list-scroll">
          <article
            v-for="item in filteredConnections"
            :key="item.id"
            class="plugin-card connection-card"
            :class="{ active: item.id === selectedConnectionId }"
            role="button"
            tabindex="0"
            @click="openConnectionEditor(item.id)"
            @keydown.enter.prevent="openConnectionEditor(item.id)"
            @keydown.space.prevent="openConnectionEditor(item.id)"
          >
            <div class="connection-card-actions">
              <button
                class="connection-switch-button"
                :class="{ active: isConnectionSwitchOn(item) }"
                type="button"
                :disabled="busy"
                :aria-label="connectionSwitchTitle(item)"
                :title="connectionSwitchTitle(item)"
                @click.stop="toggleConnectionCard(item)"
              >
                <span class="connection-switch-thumb"></span>
              </button>
              <div class="connection-icon-actions">
                <button
                  class="connection-icon-btn probe"
                  type="button"
                  :disabled="busy"
                  :aria-label="`探测连接 ${item.id}`"
                  :title="`探测连接 ${item.id}`"
                  @click.stop="probeConnectionById(item.id)"
                >
                  <svg viewBox="0 0 24 24" aria-hidden="true">
                    <path d="M12 3a9 9 0 0 1 9 9h-2a7 7 0 1 0-2.05 4.95l1.41 1.41A9 9 0 1 1 12 3Z" />
                    <path d="M12 7a5 5 0 0 1 5 5h-2a3 3 0 1 0-.88 2.12l1.42 1.42A5 5 0 1 1 12 7Z" />
                    <path d="M12 11a1 1 0 0 1 1 1h8v2h-8a3 3 0 1 1-1-3Z" />
                  </svg>
                </button>
                <button
                  class="connection-icon-btn"
                  type="button"
                  :aria-label="`配置连接 ${item.id}`"
                  :title="`配置连接 ${item.id}`"
                  @click.stop="openConnectionEditor(item.id)"
                >
                  <svg viewBox="0 0 24 24" aria-hidden="true">
                    <path d="M12 15.5A3.5 3.5 0 1 0 12 8a3.5 3.5 0 0 0 0 7.5Z" />
                    <path d="M19.4 13.5a7.8 7.8 0 0 0 0-3l2-1.5-2-3.4-2.4 1a8.6 8.6 0 0 0-2.6-1.5L14 2.5h-4l-.4 2.6A8.6 8.6 0 0 0 7 6.6l-2.4-1-2 3.4 2 1.5a7.8 7.8 0 0 0 0 3l-2 1.5 2 3.4 2.4-1a8.6 8.6 0 0 0 2.6 1.5l.4 2.6h4l.4-2.6a8.6 8.6 0 0 0 2.6-1.5l2.4 1 2-3.4-2-1.5Z" />
                  </svg>
                </button>
                <button
                  class="connection-icon-btn danger"
                  type="button"
                  :disabled="busy"
                  :aria-label="`删除连接 ${item.id}`"
                  :title="`删除连接 ${item.id}`"
                  @click.stop="deleteConnectionById(item.id)"
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
                <strong>{{ item.id }}</strong>
                <span>{{ presentIngressType(item.ingress_type) }} / {{ item.action_type || '-' }}</span>
              </div>
              <span class="status-pill" :class="`status-${statusTone(item)}`">{{ presentState(item) }}</span>
            </div>
            <p>
              <span>{{ item.self_nickname || '暂时还没有观测到机器人资料。' }}</span>
              <span v-if="item.self_id" class="connection-bot-qq">机器人 QQ：{{ item.self_id }}</span>
            </p>
            <div class="plugin-card-meta">
              <span>{{ item.enabled ? '已启用' : '未启用' }}</span>
              <span>{{ item.connected_clients || 0 }} 个客户端</span>
              <span>{{ formatDateTime(item.updated_at) }}</span>
            </div>
            <p v-if="item.last_error" class="error-copy">{{ item.last_error }}</p>
          </article>
        </div>
      </article>
    </section>

    <div v-if="editorModalOpen" class="connection-modal-backdrop" role="presentation" @click.self="closeConnectionEditor">
      <section class="card detail-card connection-editor-card">
        <div class="section-head">
          <div>
            <span class="eyebrow">{{ editorMode === 'edit' ? '编辑连接' : '新建连接' }}</span>
            <h3>{{ editorMode === 'edit' ? draft.id || '连接' : '新建连接' }}</h3>
          </div>
          <div class="connection-modal-actions">
            <div class="action-row">
              <button class="secondary-btn" type="button" :disabled="!selectedConnectionId || busy" @click="probeConnection">探测</button>
              <button class="danger-btn" type="button" :disabled="!selectedConnectionId || busy" @click="deleteConnection">删除</button>
            </div>
            <button class="connection-modal-close" type="button" aria-label="关闭连接配置" title="关闭" @click="closeConnectionEditor">
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path d="m6.4 5 12.6 12.6-1.4 1.4L5 6.4 6.4 5Z" />
                <path d="M17.6 5 19 6.4 6.4 19 5 17.6 17.6 5Z" />
              </svg>
            </button>
          </div>
        </div>

        <div v-if="detailError" class="banner banner-danger">
          <strong>详情加载失败</strong>
          <span>{{ detailError }}</span>
        </div>
        <div v-if="formError" class="banner banner-danger">
          <strong>配置无效</strong>
          <span>{{ formError }}</span>
        </div>
        <div v-if="loadingDetail" class="empty-state compact">正在加载连接详情...</div>

        <div class="banner banner-info">
          <strong>{{ selectedTypeMeta.label }}</strong>
          <span>{{ selectedTypeMeta.note }}</span>
        </div>

        <div class="config-grid connection-form-grid">
          <label class="field">
            <span>连接 ID</span>
            <input v-model.trim="draft.id" class="text-control" type="text" :disabled="editorMode === 'edit'" placeholder="gobot-dev" />
          </label>
          <label class="field">
            <span>接入类型</span>
            <select v-model="draft.ingress.type" class="text-control" :disabled="busy">
              <option value="ws_server">WebSocket 服务端</option>
              <option value="ws_reverse">WebSocket 客户端</option>
              <option value="http_callback">HTTP 回调</option>
            </select>
          </label>
          <label class="checkbox-row connection-enabled-row">
            <input v-model="draft.enabled" type="checkbox" />
            <span>启用这个连接</span>
          </label>
          <label class="field">
            <span>平台</span>
            <input v-model.trim="draft.platform" class="text-control" type="text" placeholder="onebot_v11" />
          </label>

          <label v-if="draft.ingress.type === 'ws_reverse'" class="field">
            <span>{{ selectedTypeMeta.addressLabel }}</span>
            <input v-model.trim="draft.ingress.url" class="text-control" type="text" :placeholder="selectedTypeMeta.addressPlaceholder" />
          </label>
          <label v-else class="field">
            <span>{{ selectedTypeMeta.addressLabel }}</span>
            <input v-model.trim="draft.ingress.listen" class="text-control" type="text" :placeholder="selectedTypeMeta.addressPlaceholder" />
          </label>

          <label v-if="draft.ingress.type === 'ws_reverse'" class="field">
            <span>{{ selectedTypeMeta.pathLabel }}</span>
            <input v-model.number="draft.ingress.retry_interval_ms" class="text-control" type="number" min="1" step="1000" :placeholder="selectedTypeMeta.pathPlaceholder" />
          </label>
          <label v-else class="field">
            <span>{{ selectedTypeMeta.pathLabel }}</span>
            <input v-model.trim="draft.ingress.path" class="text-control" type="text" :placeholder="selectedTypeMeta.pathPlaceholder" />
          </label>

          <label class="field">
            <span>动作类型</span>
            <select v-model="draft.action.type" class="text-control" :disabled="draft.ingress.type === 'http_callback'">
              <option value="onebot_ws">复用 WebSocket</option>
              <option value="napcat_http">NapCat HTTP API</option>
            </select>
          </label>
          <label class="field">
            <span>动作超时（毫秒）</span>
            <input v-model.number="draft.action.timeout_ms" class="text-control" type="number" min="1" step="1000" placeholder="10000" />
          </label>
          <label v-if="usesHTTPAction" class="field">
            <span>NapCat HTTP 基础地址</span>
            <input v-model.trim="draft.action.base_url" class="text-control" type="text" placeholder="http://127.0.0.1:3000" />
          </label>
          <label class="field">
            <span>访问令牌</span>
            <input v-model.trim="draft.action.access_token" class="text-control" type="password" autocomplete="new-password" placeholder="留空，或保留已脱敏的原值" />
          </label>
        </div>

        <div class="action-row">
          <button class="primary-btn" type="button" :disabled="busy" @click="saveConnection">保存连接</button>
          <button class="secondary-btn" type="button" :disabled="busy" @click="editorMode === 'edit' && selectedConnectionId ? loadConnectionDetail(selectedConnectionId) : startCreate(draft.ingress.type)">重置</button>
        </div>

        <div v-if="selectedSnapshot" class="detail-grid">
          <article class="subcard">
            <h4>运行状态</h4>
            <dl class="detail-list">
              <div><dt>状态</dt><dd>{{ presentState(selectedSnapshot) }}</dd></div>
              <div><dt>接入</dt><dd>{{ selectedSnapshot.ingress_state || '-' }}</dd></div>
              <div><dt>在线</dt><dd>{{ selectedSnapshot.online ? '是' : '否' }}</dd></div>
              <div><dt>事件数</dt><dd>{{ selectedSnapshot.observed_events || 0 }}</dd></div>
            </dl>
          </article>
          <article class="subcard">
            <h4>机器人</h4>
            <dl class="detail-list">
              <div><dt>机器人 ID</dt><dd>{{ selectedSnapshot.self_id || '-' }}</dd></div>
              <div><dt>昵称</dt><dd>{{ selectedSnapshot.self_nickname || '-' }}</dd></div>
              <div><dt>客户端</dt><dd>{{ selectedSnapshot.connected_clients || 0 }}</dd></div>
              <div><dt>更新时间</dt><dd>{{ formatDateTime(selectedSnapshot.updated_at) }}</dd></div>
            </dl>
          </article>
        </div>

        <div v-if="selectedSnapshot?.last_error" class="banner banner-danger">
          <strong>最近错误</strong>
          <span>{{ selectedSnapshot.last_error }}</span>
        </div>
      </section>
    </div>
  </div>
</template>

<style scoped>
.connection-panel {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.connection-layout {
  grid-template-columns: minmax(0, 1fr);
}

.connection-list-card {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.connection-toolbar-row {
  align-items: center;
  margin-bottom: 0;
}

.connection-toolbar-row .search-input {
  flex: 1 1 280px;
}

.connection-filter-row {
  flex: 999 1 420px;
}

.connection-list-scroll {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
  max-height: min(72vh, 720px);
  overflow: auto;
  padding: 2px 6px 2px 2px;
}

.connection-card {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 196px;
  background: var(--card-bg);
  cursor: pointer;
}

.connection-card:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px var(--selection-shadow);
}

.connection-card-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.connection-switch-button,
.connection-icon-btn,
.connection-modal-close {
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

.connection-switch-button:disabled,
.connection-icon-btn:disabled {
  cursor: not-allowed;
  opacity: 0.45;
}

.connection-switch-button {
  width: 48px;
  height: 28px;
  padding: 3px;
  border-radius: 999px;
}

.connection-switch-button.active {
  border-color: transparent;
  background: var(--control-active-bg);
}

.connection-switch-thumb {
  display: block;
  width: 20px;
  height: 20px;
  border-radius: 999px;
  background: var(--control-thumb-bg);
  box-shadow: var(--control-thumb-shadow);
  transform: translateX(-9px);
  transition: transform 180ms ease;
}

.connection-switch-button.active .connection-switch-thumb {
  transform: translateX(9px);
}

.connection-icon-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}

.connection-icon-btn,
.connection-modal-close {
  width: 34px;
  height: 34px;
  padding: 0;
  border-radius: 12px;
}

.connection-icon-btn svg,
.connection-modal-close svg {
  width: 17px;
  height: 17px;
  fill: currentColor;
}

.connection-switch-button:not(:disabled):hover,
.connection-icon-btn:not(:disabled):hover,
.connection-modal-close:hover {
  border-color: var(--selection-border);
  background: var(--selection-bg);
  color: var(--accent-strong);
}

.connection-switch-button.active:not(:disabled):hover {
  border-color: transparent;
  background: var(--control-active-hover-bg);
  color: var(--button-primary-text);
}

.connection-switch-button:not(:disabled):focus-visible,
.connection-icon-btn:not(:disabled):focus-visible,
.connection-modal-close:focus-visible {
  outline: none;
  box-shadow: 0 0 0 3px var(--selection-shadow);
}

.connection-icon-btn.danger:not(:disabled) {
  color: var(--danger-text);
}

.connection-icon-btn.probe:not(:disabled) {
  color: var(--accent-strong);
}

.connection-icon-btn.danger:not(:disabled):hover {
  border-color: var(--danger-border);
  background: var(--danger-bg-soft);
}

.connection-card p {
  display: -webkit-box;
  overflow: hidden;
  line-height: 1.6;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 3;
}

.connection-card p span {
  display: block;
}

.connection-bot-qq {
  color: var(--text-muted);
  font-size: 12px;
}

.connection-card .plugin-card-head {
  gap: 10px;
}

.connection-card .plugin-card-head > div {
  min-width: 0;
}

.connection-card .plugin-card-head strong,
.connection-card .plugin-card-head span {
  overflow: hidden;
  text-overflow: ellipsis;
}

.connection-card .plugin-card-head span {
  white-space: nowrap;
}

.connection-card .plugin-card-meta {
  margin-top: auto;
}

.connection-modal-backdrop {
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

.connection-editor-card {
  width: min(1040px, 100%);
  max-height: min(86vh, 920px);
  overflow: auto;
}

.connection-modal-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}

@media (max-width: 1220px) {
  .connection-list-scroll {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 760px) {
  .connection-modal-backdrop {
    align-items: stretch;
    padding: 12px;
  }

  .connection-editor-card {
    max-height: calc(100vh - 24px);
    border-radius: 22px;
  }

  .connection-list-scroll {
    grid-template-columns: 1fr;
    max-height: none;
  }

  .connection-modal-actions {
    align-items: stretch;
    flex-direction: column-reverse;
  }

  .connection-modal-close {
    align-self: flex-end;
  }
}
</style>
