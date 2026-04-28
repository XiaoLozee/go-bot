<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { APIError, requestJSON } from '../lib/http'
import { cloneRecord, getBoolean, getNumber, getString, setPath } from '../lib/object-path'
import { DEFAULT_WEBUI_THEME, formatWebUIThemeLabel, webUIThemeOptions } from '../lib/webui-theme'
import type { ConfigSaveResult, RuntimeConfig, RuntimeRestartResult, WebUIBootstrap } from '../types/api'

const props = defineProps<{
  runtimeConfig: RuntimeConfig
  bootstrap: WebUIBootstrap | null
  busy: boolean
}>()

const emit = defineEmits<{
  busy: [busy: boolean]
  refresh: []
  notice: [payload: { kind: 'success' | 'error' | 'info'; title: string; text: string }]
  themeChanged: [theme: string]
  unauthorized: []
}>()

const draft = ref<Record<string, unknown>>({})
const currentPassword = ref('')
const newPassword = ref('')
const confirmPassword = ref('')
const localError = ref('')
const themeSaving = ref(false)

const configAvailable = computed(() => Object.keys(draft.value).length > 0)
const selectedTheme = computed(() => text('server.webui.theme') || props.bootstrap?.meta.webui_theme || DEFAULT_WEBUI_THEME)
const selectedStorageEngine = computed(() => normalizeStorageEngine(text('storage.engine')))
const selectedMediaBackend = computed(() => normalizeMediaStorageBackend(text('storage.media.backend')))
const configSummary = computed(() => [
  {
    label: '主题',
    value: formatWebUIThemeLabel(selectedTheme.value),
    note: '当前网站使用的配色。',
  },
  {
    label: '后台服务',
    value: boolValue('server.admin.enabled') ? '已启用' : '未启用',
    note: text('server.admin.listen') || '未设置监听地址。',
  },
  {
    label: 'WebUI',
    value: boolValue('server.webui.enabled') ? '已启用' : '未启用',
    note: text('server.webui.base_path') || '/',
  },
  {
    label: '存储引擎',
    value: selectedStorageEngine.value,
    note:
      selectedStorageEngine.value === 'mysql'
        ? `${text('storage.mysql.host') || '未设置主机'}:${numberValue('storage.mysql.port') || 3306}`
        : selectedStorageEngine.value === 'postgresql'
          ? `${text('storage.postgresql.host') || '未设置主机'}:${numberValue('storage.postgresql.port') || 5432}`
          : text('storage.sqlite.path') || text('storage.logs.dir') || '未设置路径。',
  },
])

watch(
  () => props.bootstrap?.config,
  () => resetDraft(),
  { immediate: true, deep: true },
)

function apiURL(path: string): string {
  return props.runtimeConfig.apiBasePath + path
}

function resetDraft() {
  draft.value = cloneRecord(props.bootstrap?.config)
  localError.value = ''
  emit('themeChanged', getString(draft.value, 'server.webui.theme') || props.bootstrap?.meta.webui_theme || DEFAULT_WEBUI_THEME)
}

function text(path: string): string {
  return getString(draft.value, path)
}

function numberValue(path: string): number {
  return getNumber(draft.value, path)
}

function boolValue(path: string): boolean {
  return getBoolean(draft.value, path)
}

function setText(path: string, value: string) {
  setPath(draft.value, path, value)
}

function setNumber(path: string, value: string | number) {
  const number = Number(value)
  setPath(draft.value, path, Number.isFinite(number) ? number : 0)
}

function setBool(path: string, value: boolean) {
  setPath(draft.value, path, value)
}

function normalizeStorageEngine(value: string): 'sqlite' | 'mysql' | 'postgresql' {
  const normalized = String(value || '').trim().toLowerCase()
  if (normalized === 'mysql') return 'mysql'
  if (normalized === 'postgres' || normalized === 'postgresql') return 'postgresql'
  return 'sqlite'
}

function normalizeMediaStorageBackend(value: string): 'local' | 'r2' {
  const normalized = String(value || '').trim().toLowerCase()
  if (normalized === 'r2' || normalized === 'cloudflare_r2' || normalized === 'cloudflare-r2') return 'r2'
  return 'local'
}

function setStorageEngine(value: string) {
  setText('storage.engine', normalizeStorageEngine(value))
}

function setMediaStorageBackend(value: string) {
  setText('storage.media.backend', normalizeMediaStorageBackend(value))
}

async function setTheme(value: string) {
  const nextTheme = String(value || '').trim()
  const previousTheme = selectedTheme.value
  if (!nextTheme || nextTheme === previousTheme || themeSaving.value) return

  localError.value = ''
  setText('server.webui.theme', nextTheme)
  emit('themeChanged', nextTheme)
  themeSaving.value = true
  try {
    const result = await requestJSON<ConfigSaveResult>(apiURL('/webui/theme'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ theme: nextTheme }),
    })
    setText('server.webui.theme', nextTheme)
    notify('success', '配色已保存', result.message || '网站配色已自动保存。')
    emit('refresh')
  } catch (error) {
    const message = formatError(error, '保存网站配色失败。')
    setText('server.webui.theme', previousTheme)
    emit('themeChanged', previousTheme)
    localError.value = message
    notify('error', '配色保存失败', message)
  } finally {
    themeSaving.value = false
  }
}

function notify(kind: 'success' | 'error' | 'info', title: string, text: string) {
  emit('notice', { kind, title, text })
}

function formatError(error: unknown, fallback: string): string {
  if (error instanceof APIError) {
    if (error.status === 401) emit('unauthorized')
    return error.message || fallback
  }
  if (error instanceof Error) return error.message
  return fallback
}

async function saveConfig() {
  if (props.busy || !configAvailable.value) return
  localError.value = ''
  emit('busy', true)
  try {
    const result = await requestJSON<ConfigSaveResult>(apiURL('/config/save'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(draft.value),
    })
    if (result.normalized_config) {
      draft.value = cloneRecord(result.normalized_config)
    }
    emit('themeChanged', getString(result.normalized_config || draft.value, 'server.webui.theme') || selectedTheme.value)
    notify('success', '配置已保存', result.message || '系统配置已保存。')
    emit('refresh')
  } catch (error) {
    const message = formatError(error, '保存系统配置失败。')
    localError.value = message
    notify('error', '保存失败', message)
  } finally {
    emit('busy', false)
  }
}

async function hotRestart() {
  if (props.busy) return
  emit('busy', true)
  try {
    const result = await requestJSON<RuntimeRestartResult>(apiURL('/config/restart'), { method: 'POST' })
    notify('success', '运行时已重启', result.message || `当前运行状态：${result.state}`)
    emit('refresh')
  } catch (error) {
    notify('error', '重启失败', formatError(error, '热重启运行时失败。'))
  } finally {
    emit('busy', false)
  }
}

async function changePassword() {
  if (props.busy) return
  localError.value = ''
  if (!currentPassword.value || !newPassword.value) {
    localError.value = '当前密码和新密码都不能为空。'
    return
  }
  if (newPassword.value !== confirmPassword.value) {
    localError.value = '两次输入的新密码不一致。'
    return
  }
  emit('busy', true)
  try {
    await requestJSON<Record<string, unknown>>(apiURL('/auth/password'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ current_password: currentPassword.value, new_password: newPassword.value }),
    })
    currentPassword.value = ''
    newPassword.value = ''
    confirmPassword.value = ''
    notify('success', '密码已更新', '后台密码已更新。')
  } catch (error) {
    const message = formatError(error, '更新后台密码失败。')
    localError.value = message
    notify('error', '密码更新失败', message)
  } finally {
    emit('busy', false)
  }
}
</script>

<template>
  <section class="config-panel-shell">
    <section class="card config-hero-card">
      <div>
        <span class="eyebrow">系统配置</span>
        <h2>集中维护运行参数与后台设置</h2>
        <p>保存系统配置、切换 WebUI 主题、热重启运行时，并更新后台密码。</p>
      </div>

      <div class="config-hero-metrics">
        <article v-for="item in configSummary" :key="item.label" class="config-hero-metric">
          <span>{{ item.label }}</span>
          <strong>{{ item.value }}</strong>
          <small>{{ item.note }}</small>
        </article>
      </div>
    </section>

    <div v-if="localError" class="banner banner-danger">
      <strong>配置错误</strong>
      <span>{{ localError }}</span>
    </div>

    <div v-if="!configAvailable" class="empty-state">配置仍在加载中。</div>

    <section v-else class="config-workbench-grid">
      <div class="config-main-column">
        <article class="card config-section-card">
          <div class="section-head compact">
            <div>
              <span class="eyebrow">应用</span>
              <h3>机器人信息</h3>
            </div>
          </div>
          <div class="config-grid">
            <label class="schema-field">
              <span class="schema-label-row"><strong>机器人昵称</strong><small>app.name</small></span>
              <input class="text-control" type="text" :value="text('app.name')" @input="setText('app.name', ($event.target as HTMLInputElement).value)" />
            </label>
            <label class="schema-field">
              <span class="schema-label-row"><strong>环境</strong><small>app.env</small></span>
              <input class="text-control" type="text" :value="text('app.env')" @input="setText('app.env', ($event.target as HTMLInputElement).value)" />
            </label>
            <label class="schema-field">
              <span class="schema-label-row"><strong>主人 QQ</strong><small>app.owner_qq</small></span>
              <input class="text-control" type="text" :value="text('app.owner_qq')" @input="setText('app.owner_qq', ($event.target as HTMLInputElement).value)" />
            </label>
            <label class="schema-field">
              <span class="schema-label-row"><strong>数据目录</strong><small>app.data_dir</small></span>
              <input class="text-control" type="text" :value="text('app.data_dir')" @input="setText('app.data_dir', ($event.target as HTMLInputElement).value)" />
            </label>
            <label class="schema-field ai-wide-field">
              <span class="schema-label-row"><strong>日志级别</strong><small>app.log_level</small></span>
              <select class="text-control" :value="text('app.log_level')" @change="setText('app.log_level', ($event.target as HTMLSelectElement).value)">
                <option value="debug">debug</option>
                <option value="info">info</option>
                <option value="warn">warn</option>
                <option value="error">error</option>
              </select>
            </label>
          </div>
        </article>

        <article class="card config-section-card">
          <div class="section-head compact">
            <div>
              <span class="eyebrow">后台服务</span>
              <h3>WebUI 与 HTTP 服务</h3>
            </div>
          </div>
          <div class="config-grid">
            <label class="checkbox-row config-checkbox-card">
              <input type="checkbox" :checked="boolValue('server.admin.enabled')" @change="setBool('server.admin.enabled', ($event.target as HTMLInputElement).checked)" />
              <span>启用后台 HTTP 服务</span>
            </label>
            <label class="schema-field">
              <span class="schema-label-row"><strong>后台监听地址</strong><small>server.admin.listen</small></span>
              <input class="text-control" type="text" :value="text('server.admin.listen')" @input="setText('server.admin.listen', ($event.target as HTMLInputElement).value)" />
            </label>
            <label class="checkbox-row config-checkbox-card">
              <input type="checkbox" :checked="boolValue('server.webui.enabled')" @change="setBool('server.webui.enabled', ($event.target as HTMLInputElement).checked)" />
              <span>启用 WebUI</span>
            </label>
            <label class="schema-field">
              <span class="schema-label-row"><strong>WebUI 基础路径</strong><small>server.webui.base_path</small></span>
              <input class="text-control" type="text" :value="text('server.webui.base_path')" @input="setText('server.webui.base_path', ($event.target as HTMLInputElement).value)" />
            </label>
            <label class="schema-field ai-wide-field">
              <span class="schema-label-row"><strong>主题</strong><small>server.webui.theme</small></span>
              <div class="theme-picker-grid" role="radiogroup" aria-label="网站配色">
                <button
                  v-for="theme in webUIThemeOptions"
                  :key="theme.value"
                  class="theme-picker-card"
                  :class="{ active: selectedTheme === theme.value }"
                  type="button"
                  role="radio"
                  :disabled="busy || themeSaving"
                  :aria-checked="selectedTheme === theme.value"
                  @click="setTheme(theme.value)"
                >
                  <span class="theme-picker-swatch" :style="{ background: theme.preview }">
                    <span v-if="selectedTheme === theme.value" class="theme-picker-check">✓</span>
                  </span>
                  <span class="theme-picker-copy">
                    <strong>{{ theme.label }}</strong>
                    <small>{{ theme.note }}</small>
                  </span>
                </button>
              </div>
              <p class="inline-note">
                点击配色块会立即切换并自动保存当前网站配色{{ themeSaving ? '，正在写入配置…' : '。' }}
              </p>
            </label>
          </div>
        </article>

        <article class="card config-section-card">
          <div class="section-head compact">
            <div>
              <span class="eyebrow">存储</span>
              <h3>数据库与日志</h3>
            </div>
          </div>
          <div class="config-grid">
            <label class="schema-field">
              <span class="schema-label-row"><strong>存储引擎</strong><small>storage.engine</small></span>
              <select class="text-control" :value="selectedStorageEngine" @change="setStorageEngine(($event.target as HTMLSelectElement).value)">
                <option value="sqlite">sqlite</option>
                <option value="mysql">mysql</option>
                <option value="postgresql">postgresql</option>
              </select>
            </label>

            <template v-if="selectedStorageEngine === 'sqlite'">
              <label class="schema-field">
                <span class="schema-label-row"><strong>SQLite 路径</strong><small>storage.sqlite.path</small></span>
                <input class="text-control" type="text" :value="text('storage.sqlite.path')" @input="setText('storage.sqlite.path', ($event.target as HTMLInputElement).value)" />
              </label>
            </template>

            <template v-else-if="selectedStorageEngine === 'mysql'">
              <label class="schema-field">
                <span class="schema-label-row"><strong>MySQL 主机</strong><small>storage.mysql.host</small></span>
                <input class="text-control" type="text" :value="text('storage.mysql.host')" @input="setText('storage.mysql.host', ($event.target as HTMLInputElement).value)" />
              </label>
              <label class="schema-field">
                <span class="schema-label-row"><strong>MySQL 端口</strong><small>storage.mysql.port</small></span>
                <input class="text-control" type="number" :value="numberValue('storage.mysql.port')" @input="setNumber('storage.mysql.port', ($event.target as HTMLInputElement).value)" />
              </label>
              <label class="schema-field">
                <span class="schema-label-row"><strong>MySQL 用户名</strong><small>storage.mysql.username</small></span>
                <input class="text-control" type="text" :value="text('storage.mysql.username')" @input="setText('storage.mysql.username', ($event.target as HTMLInputElement).value)" />
              </label>
              <label class="schema-field">
                <span class="schema-label-row"><strong>MySQL 密码</strong><small>storage.mysql.password</small></span>
                <input class="text-control" type="password" :value="text('storage.mysql.password')" autocomplete="new-password" @input="setText('storage.mysql.password', ($event.target as HTMLInputElement).value)" />
              </label>
              <label class="schema-field">
                <span class="schema-label-row"><strong>MySQL 数据库</strong><small>storage.mysql.database</small></span>
                <input class="text-control" type="text" :value="text('storage.mysql.database')" @input="setText('storage.mysql.database', ($event.target as HTMLInputElement).value)" />
              </label>
              <label class="schema-field">
                <span class="schema-label-row"><strong>MySQL 参数</strong><small>storage.mysql.params</small></span>
                <input class="text-control" type="text" :value="text('storage.mysql.params')" @input="setText('storage.mysql.params', ($event.target as HTMLInputElement).value)" />
              </label>
            </template>

            <template v-else>
              <label class="schema-field">
                <span class="schema-label-row"><strong>PostgreSQL 主机</strong><small>storage.postgresql.host</small></span>
                <input class="text-control" type="text" :value="text('storage.postgresql.host')" @input="setText('storage.postgresql.host', ($event.target as HTMLInputElement).value)" />
              </label>
              <label class="schema-field">
                <span class="schema-label-row"><strong>PostgreSQL 端口</strong><small>storage.postgresql.port</small></span>
                <input class="text-control" type="number" :value="numberValue('storage.postgresql.port')" @input="setNumber('storage.postgresql.port', ($event.target as HTMLInputElement).value)" />
              </label>
              <label class="schema-field">
                <span class="schema-label-row"><strong>PostgreSQL 用户名</strong><small>storage.postgresql.username</small></span>
                <input class="text-control" type="text" :value="text('storage.postgresql.username')" @input="setText('storage.postgresql.username', ($event.target as HTMLInputElement).value)" />
              </label>
              <label class="schema-field">
                <span class="schema-label-row"><strong>PostgreSQL 密码</strong><small>storage.postgresql.password</small></span>
                <input class="text-control" type="password" :value="text('storage.postgresql.password')" autocomplete="new-password" @input="setText('storage.postgresql.password', ($event.target as HTMLInputElement).value)" />
              </label>
              <label class="schema-field">
                <span class="schema-label-row"><strong>PostgreSQL 数据库</strong><small>storage.postgresql.database</small></span>
                <input class="text-control" type="text" :value="text('storage.postgresql.database')" @input="setText('storage.postgresql.database', ($event.target as HTMLInputElement).value)" />
              </label>
              <label class="schema-field">
                <span class="schema-label-row"><strong>SSL 模式</strong><small>storage.postgresql.ssl_mode</small></span>
                <select class="text-control" :value="text('storage.postgresql.ssl_mode') || 'disable'" @change="setText('storage.postgresql.ssl_mode', ($event.target as HTMLSelectElement).value)">
                  <option value="disable">disable</option>
                  <option value="require">require</option>
                  <option value="verify-ca">verify-ca</option>
                  <option value="verify-full">verify-full</option>
                </select>
              </label>
              <label class="schema-field ai-wide-field">
                <span class="schema-label-row"><strong>Schema</strong><small>storage.postgresql.schema</small></span>
                <input class="text-control" type="text" :value="text('storage.postgresql.schema')" @input="setText('storage.postgresql.schema', ($event.target as HTMLInputElement).value)" />
              </label>
            </template>

            <label class="schema-field">
              <span class="schema-label-row"><strong>日志目录</strong><small>storage.logs.dir</small></span>
              <input class="text-control" type="text" :value="text('storage.logs.dir')" @input="setText('storage.logs.dir', ($event.target as HTMLInputElement).value)" />
            </label>
            <label class="schema-field">
              <span class="schema-label-row"><strong>日志最大大小（MB）</strong><small>storage.logs.max_size_mb</small></span>
              <input class="text-control" type="number" :value="numberValue('storage.logs.max_size_mb')" @input="setNumber('storage.logs.max_size_mb', ($event.target as HTMLInputElement).value)" />
            </label>
            <label class="schema-field">
              <span class="schema-label-row"><strong>日志保留份数</strong><small>storage.logs.max_backups</small></span>
              <input class="text-control" type="number" :value="numberValue('storage.logs.max_backups')" @input="setNumber('storage.logs.max_backups', ($event.target as HTMLInputElement).value)" />
            </label>
            <label class="schema-field">
              <span class="schema-label-row"><strong>日志保留天数</strong><small>storage.logs.max_age_days</small></span>
              <input class="text-control" type="number" :value="numberValue('storage.logs.max_age_days')" @input="setNumber('storage.logs.max_age_days', ($event.target as HTMLInputElement).value)" />
            </label>
          </div>
        </article>

        <article class="card config-section-card">
          <div class="section-head compact">
            <div>
              <span class="eyebrow">媒体</span>
              <h3>媒体存储</h3>
            </div>
          </div>
          <div class="config-grid">
            <label class="checkbox-row config-checkbox-card">
              <input type="checkbox" :checked="boolValue('storage.media.enabled')" @change="setBool('storage.media.enabled', ($event.target as HTMLInputElement).checked)" />
              <span>启用媒体存储</span>
            </label>
            <label class="schema-field">
              <span class="schema-label-row"><strong>后端</strong><small>storage.media.backend</small></span>
              <select class="text-control" :value="selectedMediaBackend" @change="setMediaStorageBackend(($event.target as HTMLSelectElement).value)">
                <option value="local">本地存储</option>
                <option value="r2">Cloudflare R2</option>
              </select>
            </label>

            <template v-if="selectedMediaBackend === 'local'">
              <label class="schema-field ai-wide-field">
                <span class="schema-label-row"><strong>本地目录</strong><small>storage.media.local.dir</small></span>
                <input class="text-control" type="text" :value="text('storage.media.local.dir')" @input="setText('storage.media.local.dir', ($event.target as HTMLInputElement).value)" />
              </label>
            </template>

            <template v-else>
              <label class="schema-field">
                <span class="schema-label-row"><strong>R2 Account ID</strong><small>storage.media.r2.account_id</small></span>
                <input class="text-control" type="text" :value="text('storage.media.r2.account_id')" @input="setText('storage.media.r2.account_id', ($event.target as HTMLInputElement).value)" />
              </label>
              <label class="schema-field">
                <span class="schema-label-row"><strong>R2 Endpoint</strong><small>storage.media.r2.endpoint</small></span>
                <input class="text-control" type="text" :value="text('storage.media.r2.endpoint')" @input="setText('storage.media.r2.endpoint', ($event.target as HTMLInputElement).value)" />
              </label>
              <label class="schema-field">
                <span class="schema-label-row"><strong>R2 Bucket</strong><small>storage.media.r2.bucket</small></span>
                <input class="text-control" type="text" :value="text('storage.media.r2.bucket')" @input="setText('storage.media.r2.bucket', ($event.target as HTMLInputElement).value)" />
              </label>
              <label class="schema-field">
                <span class="schema-label-row"><strong>Access Key ID</strong><small>storage.media.r2.access_key_id</small></span>
                <input class="text-control" type="text" :value="text('storage.media.r2.access_key_id')" @input="setText('storage.media.r2.access_key_id', ($event.target as HTMLInputElement).value)" />
              </label>
              <label class="schema-field">
                <span class="schema-label-row"><strong>Secret Access Key</strong><small>storage.media.r2.secret_access_key</small></span>
                <input class="text-control" type="password" :value="text('storage.media.r2.secret_access_key')" autocomplete="new-password" @input="setText('storage.media.r2.secret_access_key', ($event.target as HTMLInputElement).value)" />
              </label>
              <label class="schema-field">
                <span class="schema-label-row"><strong>Public Base URL</strong><small>storage.media.r2.public_base_url</small></span>
                <input class="text-control" type="text" :value="text('storage.media.r2.public_base_url')" @input="setText('storage.media.r2.public_base_url', ($event.target as HTMLInputElement).value)" />
              </label>
              <label class="schema-field ai-wide-field">
                <span class="schema-label-row"><strong>Key Prefix</strong><small>storage.media.r2.key_prefix</small></span>
                <input class="text-control" type="text" :value="text('storage.media.r2.key_prefix')" @input="setText('storage.media.r2.key_prefix', ($event.target as HTMLInputElement).value)" />
              </label>
            </template>

            <label class="schema-field">
              <span class="schema-label-row"><strong>最大大小（MB）</strong><small>storage.media.max_size_mb</small></span>
              <input class="text-control" type="number" :value="numberValue('storage.media.max_size_mb')" @input="setNumber('storage.media.max_size_mb', ($event.target as HTMLInputElement).value)" />
            </label>
            <label class="schema-field">
              <span class="schema-label-row"><strong>下载超时（秒）</strong><small>storage.media.download_timeout_seconds</small></span>
              <input class="text-control" type="number" :value="numberValue('storage.media.download_timeout_seconds')" @input="setNumber('storage.media.download_timeout_seconds', ($event.target as HTMLInputElement).value)" />
            </label>
          </div>
        </article>
      </div>

      <aside class="config-side-column">
        <article class="card config-side-card">
          <div class="section-head compact">
            <div>
              <span class="eyebrow">操作</span>
              <h3>发布与重启</h3>
            </div>
          </div>
          <div class="config-side-actions">
            <button class="secondary-btn" type="button" :disabled="busy" @click="resetDraft">重置草稿</button>
            <button class="secondary-btn" type="button" :disabled="busy" @click="hotRestart">热重启</button>
            <button class="primary-btn" type="button" :disabled="busy || !configAvailable" @click="saveConfig">
              {{ busy ? '保存中...' : '保存配置' }}
            </button>
          </div>
          <p class="inline-note">保存后会尝试热应用配置；如果部分项不能热更新，会在通知中说明。</p>
        </article>

        <article class="card config-side-card">
          <div class="section-head compact">
            <div>
              <span class="eyebrow">后台密码</span>
              <h3>更新密码</h3>
            </div>
          </div>
          <label class="schema-field">
            <span class="schema-label-row"><strong>当前密码</strong></span>
            <input v-model="currentPassword" class="text-control" type="password" autocomplete="current-password" />
          </label>
          <label class="schema-field">
            <span class="schema-label-row"><strong>新密码</strong></span>
            <input v-model="newPassword" class="text-control" type="password" autocomplete="new-password" />
          </label>
          <label class="schema-field">
            <span class="schema-label-row"><strong>确认密码</strong></span>
            <input v-model="confirmPassword" class="text-control" type="password" autocomplete="new-password" />
          </label>
          <button class="primary-btn" type="button" :disabled="busy" @click="changePassword">更新密码</button>
          <p class="inline-note">密码更新使用独立认证接口，不会混入普通配置保存请求。</p>
        </article>
      </aside>
    </section>
  </section>
</template>

<style scoped>
.config-panel-shell {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.config-hero-card {
  display: grid;
  grid-template-columns: minmax(320px, 0.92fr) minmax(0, 1.08fr);
  gap: 18px;
  border-color: transparent;
  background: var(--hero-gradient);
  box-shadow: var(--hero-shadow);
  color: #ffffff;
}

.config-hero-card h2,
.config-hero-card p,
.config-hero-card :deep(.eyebrow) {
  margin: 0;
}

.config-hero-card p {
  color: var(--hero-soft-text);
}

.config-hero-card :deep(.eyebrow) {
  color: var(--hero-kicker);
}

.config-hero-metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}

.config-hero-metric {
  min-width: 0;
  padding: 16px;
  border-radius: 20px;
  border: 1px solid var(--hero-pill-border);
  background: var(--hero-panel-surface);
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.config-hero-metric span {
  color: var(--hero-kicker);
}

.config-hero-metric small {
  color: var(--hero-soft-text);
}

.config-hero-metric strong {
  font-size: 24px;
  line-height: 1.2;
  text-transform: none;
}

.config-workbench-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.15fr) 360px;
  gap: 18px;
  align-items: start;
}

.config-main-column,
.config-side-column,
.config-side-card,
.config-side-actions {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.config-section-card {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.config-checkbox-card {
  min-height: 54px;
  padding: 16px 18px;
  border-radius: 18px;
  border: 1px solid var(--soft-border);
  background: var(--surface-soft);
}

.theme-picker-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(112px, 1fr));
  gap: 12px;
}

.theme-picker-card {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 10px;
  padding: 12px;
  border-radius: 18px;
  border: 1px solid var(--soft-border);
  background: var(--surface-soft);
  color: var(--text-primary);
  transition: border-color 0.18s ease, box-shadow 0.18s ease, transform 0.18s ease, background 0.18s ease;
}

.theme-picker-card:hover {
  transform: translateY(-1px);
  border-color: var(--accent-border);
  box-shadow: 0 10px 24px rgba(15, 23, 42, 0.06);
}

.theme-picker-card.active {
  border-color: var(--accent);
  background: var(--chip-active-bg);
  box-shadow: 0 0 0 3px var(--accent-ring);
}

.theme-picker-swatch {
  position: relative;
  width: 64px;
  height: 64px;
  border-radius: 20px;
  border: 1px solid rgba(255, 255, 255, 0.72);
  box-shadow: inset 0 1px 0 rgba(255, 255, 255, 0.68), 0 12px 26px rgba(15, 23, 42, 0.08);
}

.theme-picker-check {
  position: absolute;
  right: 8px;
  bottom: 8px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border-radius: 999px;
  background: rgba(15, 23, 42, 0.72);
  color: #ffffff;
  font-size: 12px;
  font-weight: 800;
}

.theme-picker-copy {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.theme-picker-copy strong {
  font-size: 14px;
  line-height: 1.2;
}

.theme-picker-copy small {
  color: var(--text-soft);
  line-height: 1.4;
}

@media (max-width: 1320px) {
  .config-hero-card,
  .config-hero-metrics,
  .config-workbench-grid {
    grid-template-columns: 1fr;
  }
}
</style>
