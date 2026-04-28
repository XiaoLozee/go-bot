<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { APIError, requestJSON } from '../lib/http'
import type { AdminSystemLogResponse, AuditLogEntry, AuditLogResponse, RuntimeConfig } from '../types/api'

const props = defineProps<{
  runtimeConfig: RuntimeConfig
  busy: boolean
}>()

const emit = defineEmits<{
  busy: [value: boolean]
  unauthorized: []
  notice: [payload: { kind: 'success' | 'error' | 'info'; title: string; text: string }]
}>()

const items = ref<AuditLogEntry[]>([])
const loading = ref(false)
const limit = ref(50)
const category = ref('')
const result = ref('')
const query = ref('')
const systemLogLoading = ref(false)
const systemLogLines = ref<string[]>([])
const systemLogPath = ref('')
const systemLogMessage = ref('')
const systemLogLimit = 200

function apiURL(): string {
  const params = new URLSearchParams()
  params.set('limit', String(limit.value || 50))
  if (category.value.trim()) params.set('category', category.value.trim())
  if (result.value.trim()) params.set('result', result.value.trim())
  if (query.value.trim()) params.set('q', query.value.trim())
  return props.runtimeConfig.apiBasePath + '/audit?' + params.toString()
}

function systemLogURL(): string {
  const params = new URLSearchParams()
  params.set('limit', String(systemLogLimit))
  return props.runtimeConfig.apiBasePath + '/audit/logs?' + params.toString()
}

function formatDateTime(value: string | undefined): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function resultTone(value: string): 'success' | 'error' | 'info' {
  const normalized = String(value || '').toLowerCase()
  if (normalized === 'success') return 'success'
  if (normalized === 'failed' || normalized === 'error') return 'error'
  return 'info'
}

function presentAuditResult(value: string): string {
  const normalized = String(value || '').toLowerCase()
  if (normalized === 'success') return '成功'
  if (normalized === 'failed' || normalized === 'error') return '失败'
  return value || '未知'
}

function formatError(error: unknown, fallback: string): string {
  if (error instanceof APIError) return error.message || fallback
  if (error instanceof Error) return error.message
  return fallback
}

async function loadAuditLogs() {
  if (loading.value || props.busy) return
  loading.value = true
  emit('busy', true)
  try {
    const response = await requestJSON<AuditLogResponse>(apiURL())
    items.value = response.items || []
  } catch (error) {
    if (error instanceof APIError && error.status === 401) {
      emit('unauthorized')
      return
    }
    emit('notice', {
      kind: 'error',
      title: '审计日志加载失败',
      text: formatError(error, '加载审计日志失败。'),
    })
  } finally {
    loading.value = false
    emit('busy', false)
  }
}

async function loadSystemLogs(force = false) {
  if (systemLogLoading.value || (!force && props.busy)) return
  systemLogLoading.value = true
  emit('busy', true)
  try {
    const response = await requestJSON<AdminSystemLogResponse>(systemLogURL())
    systemLogLines.value = response.lines || []
    systemLogPath.value = response.path || response.dir || ''
    systemLogMessage.value = response.message || ''
  } catch (error) {
    if (error instanceof APIError && error.status === 401) {
      emit('unauthorized')
      return
    }
    emit('notice', {
      kind: 'error',
      title: '运行日志加载失败',
      text: formatError(error, '加载运行日志失败。'),
    })
  } finally {
    systemLogLoading.value = false
    emit('busy', false)
  }
}

onMounted(async () => {
  await loadAuditLogs()
  await loadSystemLogs(true)
})
</script>

<template>
  <section class="audit-panel">
    <section class="card audit-filter-card">
      <div class="section-head compact">
        <div>
          <span class="eyebrow">筛选</span>
          <h3>过滤条件</h3>
        </div>
        <button class="secondary-btn" type="button" :disabled="loading || props.busy" @click="loadAuditLogs">
          {{ loading ? '加载中...' : '刷新' }}
        </button>
      </div>

      <div class="filter-row audit-filter-row">
        <input v-model="query" class="search-input" type="search" placeholder="搜索摘要、详情或目标对象" @keyup.enter="loadAuditLogs" />
        <select v-model="category" class="text-control small-control">
          <option value="">全部分类</option>
          <option value="auth">认证</option>
          <option value="plugin">插件</option>
          <option value="config">配置</option>
          <option value="webui">WebUI</option>
        </select>
        <select v-model="result" class="text-control small-control">
          <option value="">全部结果</option>
          <option value="success">成功</option>
          <option value="failed">失败</option>
        </select>
        <select v-model="limit" class="text-control small-control">
          <option :value="25">25</option>
          <option :value="50">50</option>
          <option :value="100">100</option>
          <option :value="200">200</option>
        </select>
        <button class="primary-btn" type="button" :disabled="loading || props.busy" @click="loadAuditLogs">应用筛选</button>
      </div>
    </section>

    <section class="card audit-timeline-card">
      <div class="section-head compact">
        <div>
          <span class="eyebrow">时间线</span>
          <h3>最近记录</h3>
        </div>
      </div>

      <div v-if="!items.length" class="empty-state">
        当前筛选条件下没有命中任何审计记录。
      </div>

      <div v-else class="audit-list audit-timeline">
        <article v-for="(item, index) in items" :key="`${item.at}-${item.category}-${item.action}-${index}`" class="log-entry audit-entry">
          <div class="audit-entry-rail">
            <span class="audit-entry-dot" :class="`status-${resultTone(item.result)}`"></span>
          </div>
          <div class="audit-entry-body">
            <div class="log-entry-head">
              <strong>{{ item.category || 'unknown' }} / {{ item.action || '-' }}</strong>
              <span class="status-pill" :class="`status-${resultTone(item.result)}`">{{ presentAuditResult(item.result) }}</span>
              <time>{{ formatDateTime(item.at) }}</time>
            </div>
            <p class="audit-entry-summary">{{ item.summary || '暂无摘要。' }}</p>
            <p v-if="item.detail" class="inline-note audit-entry-detail">{{ item.detail }}</p>
            <dl class="audit-meta-list">
              <div><dt>目标</dt><dd>{{ item.target || '-' }}</dd></div>
              <div><dt>用户</dt><dd>{{ item.username || '-' }}</dd></div>
              <div><dt>来源地址</dt><dd>{{ item.remote_addr || '-' }}</dd></div>
              <div><dt>请求路径</dt><dd>{{ item.method || '' }} {{ item.path || '' }}</dd></div>
            </dl>
          </div>
        </article>
      </div>
    </section>

    <section class="card audit-log-card">
      <div class="section-head compact">
        <div>
          <span class="eyebrow">日志查看</span>
          <h3>运行日志</h3>
          <p class="section-subtitle">显示最新日志文件的最近 {{ systemLogLimit }} 行。</p>
        </div>
        <div class="audit-log-actions">
          <span v-if="systemLogPath" class="audit-log-path" :title="systemLogPath">{{ systemLogPath }}</span>
          <button class="secondary-btn" type="button" :disabled="systemLogLoading || props.busy" @click="loadSystemLogs()">
            {{ systemLogLoading ? '加载中...' : '刷新日志' }}
          </button>
        </div>
      </div>

      <div v-if="systemLogMessage && !systemLogLines.length" class="empty-state">
        {{ systemLogMessage }}
      </div>
      <pre v-else class="audit-log-viewer">{{ systemLogLines.join('\n') || '暂无运行日志。' }}</pre>
    </section>
  </section>
</template>

<style scoped>
.audit-panel {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.audit-filter-card,
.audit-timeline-card,
.audit-log-card {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.audit-log-actions {
  min-width: 0;
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
}

.audit-log-path {
  max-width: min(420px, 42vw);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  border-radius: 999px;
  border: 1px solid var(--soft-border);
  background: var(--surface-soft);
  color: var(--text-soft);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', monospace;
  font-size: 12px;
  padding: 7px 10px;
}

.audit-log-viewer {
  margin: 0;
  min-height: 260px;
  max-height: 520px;
  overflow: auto;
  border-radius: 20px;
  border: 1px solid var(--soft-border);
  background: var(--code-surface);
  color: var(--text-secondary);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', monospace;
  font-size: 12px;
  line-height: 1.75;
  padding: 16px;
  white-space: pre-wrap;
  word-break: break-word;
}

@media (max-width: 720px) {
  .audit-log-actions {
    width: 100%;
    align-items: stretch;
    flex-direction: column;
  }

  .audit-log-path {
    max-width: 100%;
  }
}
</style>
