<script setup lang="ts">
import { computed } from 'vue'
import { cloneRecord, isRecord } from '../lib/object-path'
import type { AIMCPServerView, AISkillView, AIView } from '../types/api'

type MCPServerConfig = {
  id: string
  name: string
  enabled: boolean
  transport: string
  command: string
  args: string[]
  env: Record<string, string>
  url: string
  headers: Record<string, string>
  protocol_version: string
  timeout_seconds: number
  max_output_bytes: number
  allowed_tools: string[]
}

const props = defineProps<{
  view?: AIView | null
  config: Record<string, unknown>
  configLoaded: boolean
  busy: boolean
}>()

const emit = defineEmits<{
  'update:config': [value: Record<string, unknown>]
  notice: [payload: { kind: 'success' | 'error' | 'info'; title: string; text: string }]
}>()

const mcpEnabled = computed(() => boolValue(mcpConfig().enabled))
const serverConfigs = computed<MCPServerConfig[]>(() => {
  const servers = mcpConfig().servers
  if (!Array.isArray(servers)) return []
  return servers.map(normalizeServerConfig)
})
const serverStatuses = computed<AIMCPServerView[]>(() => props.view?.debug?.mcp_servers || [])
const mcpSkills = computed<AISkillView[]>(() => (props.view?.skills || []).filter((item) => item.source === 'mcp'))
const registeredMCPToolCount = computed(() => mcpSkills.value.reduce((sum, item) => sum + Number(item.tool_count || 0), 0))
const readyServerCount = computed(() => serverStatuses.value.filter((item) => item.state === 'ready').length)
const enabledServerCount = computed(() => serverConfigs.value.filter((item) => item.enabled).length)

function mcpConfig(): Record<string, unknown> {
  const raw = props.config.mcp
  return isRecord(raw) ? raw : {}
}

function boolValue(value: unknown): boolean {
  return value === true
}

function stringValue(value: unknown): string {
  if (typeof value === 'string') return value
  if (typeof value === 'number') return String(value)
  return ''
}

function numberValue(value: unknown, fallback: number): number {
  const parsed = Number(value)
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback
}

function stringArray(value: unknown): string[] {
  if (!Array.isArray(value)) return []
  return value.map((item) => String(item || '').trim()).filter(Boolean)
}

function stringMap(value: unknown): Record<string, string> {
  if (!isRecord(value)) return {}
  const out: Record<string, string> = {}
  for (const [key, item] of Object.entries(value)) {
    const normalizedKey = String(key || '').trim()
    if (!normalizedKey) continue
    out[normalizedKey] = String(item ?? '').trim()
  }
  return out
}

function normalizeServerConfig(value: unknown): MCPServerConfig {
  const raw = isRecord(value) ? value : {}
  return {
    id: stringValue(raw.id),
    name: stringValue(raw.name),
    enabled: raw.enabled !== false,
    transport: stringValue(raw.transport) || 'stdio',
    command: stringValue(raw.command),
    args: stringArray(raw.args),
    env: stringMap(raw.env),
    url: stringValue(raw.url),
    headers: stringMap(raw.headers),
    protocol_version: stringValue(raw.protocol_version) || '2025-06-18',
    timeout_seconds: numberValue(raw.timeout_seconds, 15),
    max_output_bytes: numberValue(raw.max_output_bytes, 65536),
    allowed_tools: stringArray(raw.allowed_tools),
  }
}

function emitConfig(nextMCP: Record<string, unknown>) {
  const next = cloneRecord(props.config)
  next.mcp = nextMCP
  emit('update:config', next)
}

function updateMCP(patch: Record<string, unknown>) {
  emitConfig({ ...mcpConfig(), ...patch })
}

function updateServer(index: number, patch: Partial<MCPServerConfig>) {
  const servers = serverConfigs.value.map((item) => ({ ...item }))
  if (!servers[index]) return
  servers[index] = { ...servers[index], ...patch }
  updateMCP({ servers })
}

function addServer() {
  const servers = serverConfigs.value.map((item) => ({ ...item }))
  const id = nextServerID(servers)
  servers.push({
    id,
    name: '本地 MCP 服务',
    enabled: true,
    transport: 'stdio',
    command: '',
    args: [],
    env: {},
    url: '',
    headers: {},
    protocol_version: '2025-06-18',
    timeout_seconds: 15,
    max_output_bytes: 65536,
    allowed_tools: [],
  })
  updateMCP({ enabled: true, servers })
  emit('notice', { kind: 'info', title: '已添加 MCP 服务', text: '填写命令或 HTTP 地址后，点击顶部“保存 AI 配置”生效。' })
}

function removeServer(index: number) {
  const servers = serverConfigs.value.map((item) => ({ ...item }))
  if (!servers[index]) return
  const removed = servers.splice(index, 1)[0]
  updateMCP({ servers })
  emit('notice', { kind: 'info', title: '已移除 MCP 服务', text: `保存 AI 配置后移除 ${removed.name || removed.id}。` })
}

function nextServerID(servers: MCPServerConfig[]): string {
  const used = new Set(servers.map((item) => item.id))
  for (let index = servers.length + 1; index < servers.length + 100; index += 1) {
    const id = `mcp_server_${index}`
    if (!used.has(id)) return id
  }
  return `mcp_server_${Date.now().toString(36)}`
}

function statusFor(serverID: string): AIMCPServerView | null {
  return serverStatuses.value.find((item) => item.id === serverID) || null
}

function stateLabel(state: string): string {
  switch (state) {
    case 'ready':
      return '已就绪'
    case 'failed':
      return '连接失败'
    case 'waiting_ai':
      return '等待 AI 启用'
    case 'connecting':
      return '连接中'
    case 'disabled':
      return '未启用'
    default:
      return state || '未知'
  }
}

function stateClass(state: string): string {
  switch (state) {
    case 'ready':
      return 'state-ready'
    case 'failed':
      return 'state-failed'
    case 'waiting_ai':
      return 'state-waiting'
    default:
      return 'state-muted'
  }
}

function sseStateLabel(state?: string): string {
  switch (state) {
    case 'listening':
      return 'SSE 监听中'
    case 'connecting':
      return 'SSE 连接中'
    case 'reconnecting':
      return 'SSE 重连中'
    case 'refresh_failed':
      return '工具刷新失败'
    case 'unsupported':
      return '未提供 SSE GET'
    default:
      return 'SSE 未连接'
  }
}

function formatDateTime(value?: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return '-'
  return date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    second: '2-digit',
  })
}

function formatList(value: string[]): string {
  return value.join('\n')
}

function parseList(value: string): string[] {
  return value.split(/[\n,]/g).map((item) => item.trim()).filter(Boolean)
}

function formatKeyValueBlock(value: Record<string, string>): string {
  return Object.entries(value).map(([key, item]) => `${key}=${item}`).join('\n')
}

function parseKeyValueBlock(value: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const line of value.split('\n')) {
    const trimmed = line.trim()
    if (!trimmed) continue
    const splitAt = trimmed.indexOf('=')
    if (splitAt <= 0) continue
    const key = trimmed.slice(0, splitAt).trim()
    if (!key) continue
    out[key] = trimmed.slice(splitAt + 1).trim()
  }
  return out
}
</script>

<template>
  <section class="ai-mcp-center">
    <section class="subcard ai-mcp-hero-card">
      <div>
        <span class="eyebrow">MCP</span>
        <h4>MCP 工具接入</h4>
        <p>连接真实 MCP 服务后，AI 会把服务暴露的 tools/list 注册为可调用工具，并在回复时通过 tools/call 调用。</p>
      </div>
      <label class="switch ai-mcp-main-switch">
        <input type="checkbox" :checked="mcpEnabled" :disabled="busy || !configLoaded" @change="updateMCP({ enabled: ($event.target as HTMLInputElement).checked })" />
        <span class="slider"></span>
      </label>
    </section>

    <div class="ai-mcp-summary-grid">
      <article class="subcard ai-mcp-summary-card">
        <span>全局状态</span>
        <strong>{{ mcpEnabled ? '已启用' : '未启用' }}</strong>
        <p>关闭后不会启动 MCP 进程，也不会向 AI 注入 MCP 工具。</p>
      </article>
      <article class="subcard ai-mcp-summary-card">
        <span>服务状态</span>
        <strong>{{ readyServerCount }} / {{ enabledServerCount }}</strong>
        <p>已就绪 / 已启用服务。失败原因会显示在服务卡片里。</p>
      </article>
      <article class="subcard ai-mcp-summary-card">
        <span>已注册工具</span>
        <strong>{{ registeredMCPToolCount }}</strong>
        <p>保存配置并连接成功后，这些工具会进入 AI Tool 调用链路。</p>
      </article>
    </div>

    <section class="subcard ai-mcp-section-card">
      <div class="ai-mcp-section-head">
        <div>
          <span class="eyebrow">Servers</span>
          <h4>MCP 服务配置</h4>
          <p>当前支持 stdio 与 streamable HTTP。修改后需要点击顶部“保存 AI 配置”才会启动、重连并重新发现工具。</p>
        </div>
        <button class="primary-btn" type="button" :disabled="busy || !configLoaded" @click="addServer">添加 MCP 服务</button>
      </div>

      <div v-if="!serverConfigs.length" class="empty-state compact">还没有 MCP 服务。点击右上角添加一个本地 stdio 或 HTTP MCP 服务。</div>
      <div v-else class="ai-mcp-server-grid">
        <article v-for="(server, index) in serverConfigs" :key="`${server.id}-${index}`" class="subcard ai-mcp-server-card">
          <div class="ai-mcp-server-head">
            <div>
              <h4>{{ server.name || server.id || '未命名 MCP 服务' }}</h4>
              <p>{{ server.transport === 'stdio' ? server.command || '未填写启动命令' : server.url || '未填写服务地址' }}</p>
            </div>
            <div class="ai-mcp-server-actions">
              <span class="ai-mcp-state-chip" :class="stateClass(statusFor(server.id)?.state || (server.enabled ? 'waiting_ai' : 'disabled'))">
                {{ stateLabel(statusFor(server.id)?.state || (server.enabled ? 'waiting_ai' : 'disabled')) }}
              </span>
              <label class="switch">
                <input type="checkbox" :checked="server.enabled" :disabled="busy" @change="updateServer(index, { enabled: ($event.target as HTMLInputElement).checked })" />
                <span class="slider"></span>
              </label>
            </div>
          </div>

          <div class="ai-mcp-form-grid">
            <label>
              <span>服务 ID</span>
              <input class="text-control" type="text" :value="server.id" placeholder="local_search" :disabled="busy" @input="updateServer(index, { id: ($event.target as HTMLInputElement).value })" />
            </label>
            <label>
              <span>显示名称</span>
              <input class="text-control" type="text" :value="server.name" placeholder="本地搜索 MCP" :disabled="busy" @input="updateServer(index, { name: ($event.target as HTMLInputElement).value })" />
            </label>
            <label>
              <span>传输方式</span>
              <select class="text-control" :value="server.transport" :disabled="busy" @change="updateServer(index, { transport: ($event.target as HTMLSelectElement).value })">
                <option value="stdio">stdio 本地进程</option>
                <option value="streamable_http">Streamable HTTP</option>
                <option value="http">HTTP</option>
              </select>
            </label>
            <label>
              <span>协议版本</span>
              <input class="text-control" type="text" :value="server.protocol_version" :disabled="busy" @input="updateServer(index, { protocol_version: ($event.target as HTMLInputElement).value })" />
            </label>
            <label>
              <span>超时秒数</span>
              <input class="text-control" type="number" min="1" max="300" :value="server.timeout_seconds" :disabled="busy" @input="updateServer(index, { timeout_seconds: Number(($event.target as HTMLInputElement).value) })" />
            </label>
            <label>
              <span>最大输出字节</span>
              <input class="text-control" type="number" min="4096" max="4194304" :value="server.max_output_bytes" :disabled="busy" @input="updateServer(index, { max_output_bytes: Number(($event.target as HTMLInputElement).value) })" />
            </label>
          </div>

          <div v-if="server.transport === 'stdio'" class="ai-mcp-form-grid single-wide">
            <label>
              <span>启动命令</span>
              <input class="text-control" type="text" :value="server.command" placeholder="npx" :disabled="busy" @input="updateServer(index, { command: ($event.target as HTMLInputElement).value })" />
            </label>
            <label>
              <span>启动参数，每行一个</span>
              <textarea class="text-control ai-mcp-textarea" :value="formatList(server.args)" placeholder="-y&#10;@modelcontextprotocol/server-filesystem&#10;D:\\Docs" :disabled="busy" @input="updateServer(index, { args: parseList(($event.target as HTMLTextAreaElement).value) })"></textarea>
            </label>
            <label>
              <span>环境变量，KEY=value</span>
              <textarea class="text-control ai-mcp-textarea" :value="formatKeyValueBlock(server.env)" placeholder="API_KEY=******" :disabled="busy" @input="updateServer(index, { env: parseKeyValueBlock(($event.target as HTMLTextAreaElement).value) })"></textarea>
            </label>
          </div>

          <div v-else class="ai-mcp-form-grid single-wide">
            <label>
              <span>HTTP 地址</span>
              <input class="text-control" type="url" :value="server.url" placeholder="https://example.com/mcp" :disabled="busy" @input="updateServer(index, { url: ($event.target as HTMLInputElement).value })" />
            </label>
            <label>
              <span>请求头，KEY=value</span>
              <textarea class="text-control ai-mcp-textarea" :value="formatKeyValueBlock(server.headers)" placeholder="Authorization=Bearer ******" :disabled="busy" @input="updateServer(index, { headers: parseKeyValueBlock(($event.target as HTMLTextAreaElement).value) })"></textarea>
            </label>
          </div>

          <label class="ai-mcp-wide-field">
            <span>工具白名单，可留空；每行一个原始工具名</span>
            <textarea class="text-control ai-mcp-textarea small" :value="formatList(server.allowed_tools)" placeholder="search&#10;read_file" :disabled="busy" @input="updateServer(index, { allowed_tools: parseList(($event.target as HTMLTextAreaElement).value) })"></textarea>
          </label>

          <div v-if="statusFor(server.id)?.last_error" class="banner banner-danger ai-mcp-error"><strong>连接失败</strong><span>{{ statusFor(server.id)?.last_error }}</span></div>
          <div v-if="statusFor(server.id)" class="ai-mcp-live-row">
            <span>{{ sseStateLabel(statusFor(server.id)?.sse_state) }}</span>
            <span>最后刷新：{{ formatDateTime(statusFor(server.id)?.last_refresh_at) }}</span>
            <span v-if="statusFor(server.id)?.last_sse_error">监听错误：{{ statusFor(server.id)?.last_sse_error }}</span>
          </div>
          <div v-if="statusFor(server.id)?.tools?.length" class="ai-mcp-tool-preview">
            <span v-for="tool in statusFor(server.id)?.tools" :key="tool.name" class="ai-mcp-tool-chip" :title="tool.description || tool.original">{{ tool.original }}</span>
          </div>

          <div class="ai-mcp-card-footer">
            <span class="inline-note">工具名会自动转为 mcp_服务ID_工具名，避免和内置/插件工具冲突。</span>
            <button class="danger-btn ghost-danger-btn" type="button" :disabled="busy" @click="removeServer(index)">移除</button>
          </div>
        </article>
      </div>
    </section>

    <section class="subcard ai-mcp-section-card">
      <div class="ai-mcp-section-head compact">
        <div>
          <span class="eyebrow">Discovered Tools</span>
          <h4>已注入 AI 的 MCP 工具</h4>
          <p>这些工具已经完成 initialize、tools/list，并注册进当前 AI Tool Catalog。</p>
        </div>
      </div>
      <div v-if="!mcpSkills.length" class="empty-state compact">暂无已注入的 MCP 工具。请启用 MCP、配置服务并保存 AI 配置。</div>
      <div v-else class="ai-mcp-discovered-grid">
        <article v-for="skill in mcpSkills" :key="skill.provider_id" class="ai-mcp-discovered-card">
          <div>
            <h5>{{ skill.name }}</h5>
            <p>{{ skill.description || 'MCP 工具服务。' }}</p>
          </div>
          <span>{{ skill.tool_count }} 个工具</span>
        </article>
      </div>
    </section>
  </section>
</template>

<style scoped>
.ai-mcp-center {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.ai-mcp-hero-card,
.ai-mcp-section-head,
.ai-mcp-server-head,
.ai-mcp-card-footer,
.ai-mcp-server-actions,
.ai-mcp-discovered-card {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
}

.ai-mcp-hero-card {
  background:
    radial-gradient(circle at top right, color-mix(in srgb, var(--theme-primary) 16%, transparent), transparent 38%),
    color-mix(in srgb, var(--surface-card) 88%, var(--theme-primary) 5%);
}

.ai-mcp-hero-card h4,
.ai-mcp-section-head h4,
.ai-mcp-server-head h4,
.ai-mcp-discovered-card h5 {
  margin: 0;
}

.ai-mcp-hero-card p,
.ai-mcp-section-head p,
.ai-mcp-summary-card p,
.ai-mcp-server-head p,
.ai-mcp-discovered-card p {
  margin: 6px 0 0;
  color: var(--text-secondary);
  line-height: 1.6;
}

.ai-mcp-main-switch {
  margin-top: 4px;
}

.ai-mcp-summary-grid,
.ai-mcp-server-grid,
.ai-mcp-discovered-grid {
  display: grid;
  gap: 12px;
}

.ai-mcp-summary-grid {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.ai-mcp-summary-card {
  min-height: 118px;
  background: color-mix(in srgb, var(--surface-card) 84%, var(--theme-primary) 4%);
}

.ai-mcp-summary-card span {
  color: var(--text-muted);
  font-size: 12px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.ai-mcp-summary-card strong {
  display: block;
  margin-top: 10px;
  font-size: 26px;
  line-height: 1.1;
}

.ai-mcp-section-card,
.ai-mcp-server-card {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.ai-mcp-server-grid {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.ai-mcp-server-card {
  min-width: 0;
  background: color-mix(in srgb, var(--surface-card) 88%, var(--theme-primary) 3%);
}

.ai-mcp-server-actions {
  align-items: center;
  flex-shrink: 0;
}

.ai-mcp-state-chip,
.ai-mcp-tool-chip,
.ai-mcp-discovered-card > span {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: 28px;
  padding: 0 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 700;
  white-space: nowrap;
}

.ai-mcp-state-chip.state-ready {
  background: color-mix(in srgb, #22c55e 14%, var(--surface-card));
  color: #15803d;
}

.ai-mcp-state-chip.state-failed {
  background: color-mix(in srgb, #ef4444 12%, var(--surface-card));
  color: #b91c1c;
}

.ai-mcp-state-chip.state-waiting,
.ai-mcp-state-chip.state-muted {
  background: var(--chip-bg);
  color: var(--text-muted);
}

.ai-mcp-form-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.ai-mcp-form-grid.single-wide {
  grid-template-columns: 1fr;
}

.ai-mcp-form-grid label,
.ai-mcp-wide-field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  color: var(--text-secondary);
  font-size: 12px;
  font-weight: 700;
}

.ai-mcp-textarea {
  min-height: 88px;
  resize: vertical;
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, 'Liberation Mono', monospace;
}

.ai-mcp-textarea.small {
  min-height: 64px;
}

.ai-mcp-error {
  margin: 0;
}

.ai-mcp-tool-preview {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.ai-mcp-live-row {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  color: var(--text-muted);
  font-size: 12px;
}

.ai-mcp-live-row span {
  display: inline-flex;
  align-items: center;
  min-height: 26px;
  padding: 0 10px;
  border-radius: 999px;
  background: var(--chip-bg);
}

.ai-mcp-tool-chip {
  background: color-mix(in srgb, var(--theme-primary) 10%, var(--surface-card));
  color: var(--theme-primary-strong);
}

.ai-mcp-card-footer {
  align-items: center;
  border-top: 1px solid var(--soft-divider);
  padding-top: 12px;
}

.ai-mcp-discovered-grid {
  grid-template-columns: repeat(3, minmax(0, 1fr));
}

.ai-mcp-discovered-card {
  padding: 16px;
  border: 1px solid var(--soft-border);
  border-radius: 18px;
  background: var(--surface-soft-alt);
}

.ai-mcp-discovered-card > span {
  background: var(--chip-bg);
  color: var(--text-secondary);
}

@media (max-width: 1180px) {
  .ai-mcp-summary-grid,
  .ai-mcp-server-grid,
  .ai-mcp-discovered-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 720px) {
  .ai-mcp-hero-card,
  .ai-mcp-section-head,
  .ai-mcp-server-head,
  .ai-mcp-card-footer,
  .ai-mcp-discovered-card {
    flex-direction: column;
  }

  .ai-mcp-form-grid {
    grid-template-columns: 1fr;
  }
}
</style>
