<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import { APIError, requestJSON } from '../lib/http'
import { cloneRecord, getBoolean, setPath } from '../lib/object-path'
import type {
  AIInstalledSkillDetailView,
  AIInstalledSkillView,
  AISkillActionResult,
  AISkillInstallResult,
  AISkillToolView,
  AISkillView,
  AIView,
} from '../types/api'

type SkillFilter = 'all' | 'enabled' | 'disabled'
type InstallLogTone = 'info' | 'success' | 'error'
type InstallLogEntry = {
  at: string
  tone: InstallLogTone
  text: string
}

type ToolCard = {
  key: string
  skill: AISkillView
  tool: AISkillToolView
}

const props = defineProps<{
  view?: AIView | null
  config: Record<string, unknown>
  configLoaded: boolean
  busy: boolean
  apiBasePath: string
}>()

const emit = defineEmits<{
  unauthorized: []
  'replace-view': [value: AIView]
  'update:config': [value: Record<string, unknown>]
  notice: [payload: { kind: 'success' | 'error' | 'info'; title: string; text: string }]
}>()

const uploadInput = ref<HTMLInputElement | null>(null)
const installLogViewer = ref<HTMLDivElement | null>(null)
const installLogModalOpen = ref(false)
const installLogEntries = ref<InstallLogEntry[]>([])
const installLogTitle = ref('')
const installing = ref(false)
const importURL = ref('')
const selectedFilter = ref<SkillFilter>('all')
const search = ref('')
const actionBusyId = ref('')
const detailModalOpen = ref(false)
const detailLoading = ref(false)
const detailError = ref('')
const detail = ref<AIInstalledSkillDetailView | null>(null)

const builtinCLIEnabled = computed(() => getBoolean(props.config || {}, 'cli.enabled'))
const savedBuiltinCLIEnabled = computed(() => getBoolean((props.view?.config as Record<string, unknown>) || {}, 'cli.enabled'))
const builtinCLIDirty = computed(() => props.configLoaded && builtinCLIEnabled.value !== savedBuiltinCLIEnabled.value)

const toolSkills = computed<AISkillView[]>(() => props.view?.skills || [])
const installedSkills = computed<AIInstalledSkillView[]>(() => props.view?.installed_skills || [])
const enabledInstalledSkills = computed(() => installedSkills.value.filter((item) => item.enabled))
const totalToolCount = computed(() => toolSkills.value.reduce((sum, item) => sum + Number(item.tool_count || 0), 0))
const toolCards = computed<ToolCard[]>(() => {
  const cards: ToolCard[] = []
  for (const skill of toolSkills.value) {
    for (const tool of skill.tools || []) {
      cards.push({
        key: `${skill.provider_id}:${tool.name}`,
        skill,
        tool,
      })
    }
  }
  return cards
})
const filterChips = computed(() => [
  { value: 'all' as SkillFilter, label: '全部', count: installedSkills.value.length },
  { value: 'enabled' as SkillFilter, label: '已启用', count: enabledInstalledSkills.value.length },
  { value: 'disabled' as SkillFilter, label: '未启用', count: installedSkills.value.length - enabledInstalledSkills.value.length },
])

const filteredInstalledSkills = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  return installedSkills.value.filter((item) => {
    if (selectedFilter.value === 'enabled' && !item.enabled) return false
    if (selectedFilter.value === 'disabled' && item.enabled) return false
    if (!keyword) return true
    return [item.name, item.description, item.source_label, item.provider, item.source_url, item.id]
      .filter(Boolean)
      .some((value) => String(value).toLowerCase().includes(keyword))
  })
})

function apiURL(path: string): string {
  return props.apiBasePath + path
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
  if (Number.isNaN(date.getTime())) return '-'
  return date.toLocaleString('zh-CN', {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function sourceLabel(skill: AIInstalledSkillView): string {
  return String(skill.source_label || skill.provider || skill.source_type || '外部技能')
}

function toolSourceLabel(value: string): string {
  switch ((value || '').toLowerCase()) {
    case 'builtin':
      return '内置'
    case 'plugin':
      return '插件'
    case 'mcp':
      return 'MCP'
    default:
      return '扩展'
  }
}

function toolSourceClass(value: string): string {
  switch ((value || '').toLowerCase()) {
    case 'builtin':
      return 'source-builtin'
    case 'plugin':
      return 'source-plugin'
    case 'mcp':
      return 'source-mcp'
    default:
      return 'source-custom'
  }
}

function toolSkillSubtitle(skill: AISkillView): string {
  if (skill.source === 'plugin') {
    return skill.plugin_id ? `插件 ${skill.plugin_id}` : '插件扩展'
  }
  if (skill.source === 'mcp') {
    return skill.namespace ? `MCP 服务 ${skill.namespace}` : 'MCP 工具服务'
  }
  if (skill.source === 'builtin') {
    return '系统核心能力'
  }
  return '扩展提供者'
}

function toolProviderLabel(skill: AISkillView): string {
  if (skill.source === 'builtin') return '核心技能'
  if (skill.source === 'plugin') return skill.plugin_id ? `插件 ${skill.plugin_id}` : '插件技能'
  return skill.namespace || skill.provider_id || '扩展技能'
}

function toolDisplayName(tool: AISkillToolView): string {
  return String(tool.display_name || tool.name || '未命名工具')
}

function toolDisplayDescription(tool: AISkillToolView): string {
  return String(tool.display_description || tool.description || '当前工具未提供额外说明。')
}

function isBuiltinCLITool(skill: AISkillView, tool: AISkillToolView): boolean {
  return skill.provider_id === 'builtin.core' && skill.source === 'builtin' && tool.name === 'run_cli_command'
}

function toolAvailabilityLabel(skill: AISkillView, tool: AISkillToolView): string {
  if (isBuiltinCLITool(skill, tool)) {
    if (builtinCLIDirty.value) {
      return builtinCLIEnabled.value ? '将启用 · 待保存' : '将停用 · 待保存'
    }
    return builtinCLIEnabled.value ? '已启用' : '已停用'
  }
  return String(tool.availability || '')
}

function updateConfig(path: string, value: unknown) {
  const next = cloneRecord(props.config)
  setPath(next, path, value)
  emit('update:config', next)
}

function toggleBuiltinCLIEnabled(value: boolean) {
  updateConfig('cli.enabled', value)
}

function appendInstallLog(text: string, tone: InstallLogTone = 'info') {
  installLogEntries.value.push({
    at: new Date().toLocaleTimeString('zh-CN', { hour12: false }),
    tone,
    text,
  })
  void nextTick(() => {
    if (installLogViewer.value) {
      installLogViewer.value.scrollTop = installLogViewer.value.scrollHeight
    }
  })
}

function resetInstallLog(title: string) {
  installLogTitle.value = title
  installLogEntries.value = []
  installLogModalOpen.value = true
}

function closeInstallLogModal() {
  if (installing.value) return
  installLogModalOpen.value = false
}

async function refreshAIViewFallback() {
  try {
    const nextView = await requestJSON<AIView>(apiURL('/ai'))
    emit('replace-view', nextView)
  } catch (error) {
    emit('notice', {
      kind: 'info',
      title: '技能中心状态未刷新',
      text: formatError(error, '技能操作已完成，但技能中心状态刷新失败。'),
    })
  }
}

function applyResultView(view?: AIView | null) {
  if (view) {
    emit('replace-view', view)
    return
  }
  void refreshAIViewFallback()
}

function openSkillPackagePicker() {
  if (props.busy || installing.value) return
  uploadInput.value?.click()
}

async function onPickSkillPackage(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  input.value = ''
  await installSkillPackage(file)
}

async function installSkillPackage(file: File) {
  if (props.busy || installing.value) return
  resetInstallLog(file.name)
  appendInstallLog(`已选择技能包：${file.name}`)
  installing.value = true
  try {
    const formData = new FormData()
    formData.append('file', file, file.name)
    formData.append('overwrite', 'false')
    appendInstallLog('正在上传技能包并解析 SKILL.md。')
    const result = await requestJSON<AISkillInstallResult>(apiURL('/ai/skills/upload'), {
      method: 'POST',
      body: formData,
    })
    appendInstallLog(`技能 ID：${result.skill?.id || '-'}`, 'success')
    appendInstallLog(`技能名称：${result.skill?.name || '-'}`)
    appendInstallLog(`来源：${result.skill?.source_label || result.skill?.provider || '-'}`)
    if (result.installed_to) appendInstallLog(`安装目录：${result.installed_to}`)
    if (result.backup_path) appendInstallLog(`旧版本备份：${result.backup_path}`)
    appendInstallLog(result.message || '技能安装完成。', 'success')
    applyResultView(result.view)
    emit('notice', {
      kind: 'success',
      title: '技能安装完成',
      text: result.message || `已安装技能：${result.skill?.name || file.name}`,
    })
  } catch (error) {
    const message = formatError(error, '安装技能失败。')
    appendInstallLog(message, 'error')
    emit('notice', { kind: 'error', title: '技能安装失败', text: message })
  } finally {
    installing.value = false
  }
}

async function importSkillFromURL() {
  if (props.busy || installing.value) return
  const sourceURL = importURL.value.trim()
  if (!sourceURL) {
    emit('notice', { kind: 'info', title: '请输入技能来源', text: '支持 GitHub 仓库页、Gitee 仓库页、直链压缩包地址，或直接上传本地技能包。' })
    return
  }
  resetInstallLog('远程导入技能')
  appendInstallLog(`技能来源：${sourceURL}`)
  installing.value = true
  try {
    appendInstallLog('后台正在解析链接并尝试下载技能包。')
    const result = await requestJSON<AISkillInstallResult>(apiURL('/ai/skills/import'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ source_url: sourceURL, overwrite: false }),
    })
    appendInstallLog(`技能 ID：${result.skill?.id || '-'}`, 'success')
    appendInstallLog(`技能名称：${result.skill?.name || '-'}`)
    appendInstallLog(`来源：${result.skill?.source_label || result.skill?.provider || '-'}`)
    if (result.installed_to) appendInstallLog(`安装目录：${result.installed_to}`)
    if (result.backup_path) appendInstallLog(`旧版本备份：${result.backup_path}`)
    appendInstallLog(result.message || '技能导入完成。', 'success')
    importURL.value = ''
    applyResultView(result.view)
    emit('notice', {
      kind: 'success',
      title: '技能导入完成',
      text: result.message || `已导入技能：${result.skill?.name || sourceURL}`,
    })
  } catch (error) {
    const message = formatError(error, '导入技能失败。')
    appendInstallLog(message, 'error')
    emit('notice', { kind: 'error', title: '技能导入失败', text: message })
  } finally {
    installing.value = false
  }
}

async function toggleInstalledSkill(skill: AIInstalledSkillView) {
  if (!skill?.id || props.busy || actionBusyId.value) return
  const nextAction = skill.enabled ? 'disable' : 'enable'
  actionBusyId.value = `${skill.id}:${nextAction}`
  try {
    const result = await requestJSON<AISkillActionResult>(apiURL(`/ai/skills/${encodeURIComponent(skill.id)}/${nextAction}`), {
      method: 'POST',
    })
    applyResultView(result.view)
    if (detail.value?.id === skill.id && result.skill) {
      detail.value = { ...detail.value, ...result.skill }
    }
    emit('notice', {
      kind: 'success',
      title: skill.enabled ? '技能已停用' : '技能已启用',
      text: result.message || `${skill.name} 状态已更新。`,
    })
  } catch (error) {
    emit('notice', {
      kind: 'error',
      title: skill.enabled ? '停用技能失败' : '启用技能失败',
      text: formatError(error, '更新技能状态失败。'),
    })
  } finally {
    actionBusyId.value = ''
  }
}

async function removeInstalledSkill(skill: AIInstalledSkillView) {
  if (!skill?.id || props.busy || actionBusyId.value) return
  const confirmed = window.confirm(`确定卸载技能「${skill.name}」吗？\n\n卸载后会移除该技能的 SKILL.md 与附带文件。`)
  if (!confirmed) return
  actionBusyId.value = `${skill.id}:uninstall`
  try {
    const result = await requestJSON<AISkillActionResult>(apiURL(`/ai/skills/${encodeURIComponent(skill.id)}/uninstall`), {
      method: 'POST',
    })
    applyResultView(result.view)
    if (detail.value?.id === skill.id) {
      detail.value = null
      detailModalOpen.value = false
    }
    emit('notice', {
      kind: 'success',
      title: '技能已卸载',
      text: result.message || `已卸载技能：${skill.name}`,
    })
  } catch (error) {
    emit('notice', {
      kind: 'error',
      title: '卸载技能失败',
      text: formatError(error, '卸载技能失败。'),
    })
  } finally {
    actionBusyId.value = ''
  }
}

async function openSkillDetail(skill: AIInstalledSkillView) {
  if (!skill?.id || detailLoading.value) return
  detailModalOpen.value = true
  detailLoading.value = true
  detailError.value = ''
  try {
    detail.value = await requestJSON<AIInstalledSkillDetailView>(apiURL(`/ai/skills/${encodeURIComponent(skill.id)}`))
  } catch (error) {
    detail.value = null
    detailError.value = formatError(error, '加载技能说明失败。')
  } finally {
    detailLoading.value = false
  }
}

function closeDetailModal() {
  if (detailLoading.value) return
  detailModalOpen.value = false
}
</script>

<template>
  <section class="ai-skill-center">
    <div class="ai-skill-summary-grid">
      <article class="subcard ai-skill-summary-card">
        <span class="ai-skill-summary-label">工具技能组</span>
        <strong>{{ toolSkills.length }}</strong>
        <p>当前已注册到 AI 核心的工具型技能来源。</p>
      </article>
      <article class="subcard ai-skill-summary-card">
        <span class="ai-skill-summary-label">工具总数</span>
        <strong>{{ totalToolCount }}</strong>
        <p>这些工具会按上下文提供给模型调用。</p>
      </article>
      <article class="subcard ai-skill-summary-card">
        <span class="ai-skill-summary-label">外部技能</span>
        <strong>{{ installedSkills.length }}</strong>
        <p>支持安装通用 SKILL.md 仓库型技能。</p>
      </article>
      <article class="subcard ai-skill-summary-card">
        <span class="ai-skill-summary-label">已启用外部技能</span>
        <strong>{{ enabledInstalledSkills.length }}</strong>
        <p>启用后会作为提示词能力注入 AI 上下文。</p>
      </article>
    </div>

    <section class="subcard ai-skill-import-card">
      <div>
        <span class="eyebrow">技能中心</span>
        <h4>安装外部技能</h4>
        <p>支持上传 ZIP / TAR.GZ / TGZ，或输入 GitHub 仓库页、Gitee 仓库页、直链压缩包地址。</p>
      </div>
      <div class="ai-skill-import-actions">
        <input
          v-model="importURL"
          class="text-control"
          type="url"
          placeholder="粘贴 GitHub / Gitee 仓库页或直链压缩包地址"
          :disabled="busy || installing"
          @keyup.enter="importSkillFromURL"
        />
        <input
          ref="uploadInput"
          class="plugin-upload-input"
          type="file"
          accept=".zip,.tgz,.tar.gz,application/zip,application/gzip"
          @change="onPickSkillPackage"
        />
        <button class="secondary-btn" type="button" :disabled="busy || installing" @click="openSkillPackagePicker">
          {{ installing ? '处理中...' : '上传技能包' }}
        </button>
        <button class="primary-btn" type="button" :disabled="busy || installing" @click="importSkillFromURL">
          {{ installing ? '导入中...' : '从链接导入' }}
        </button>
      </div>
    </section>

    <section class="subcard ai-skill-section-card">
      <div class="ai-skill-section-head">
        <div>
          <span class="eyebrow">Prompt Skills</span>
          <h4>已安装外部技能</h4>
          <p>这类技能会以提示词方式注入 AI 核心，适合通用 SKILL.md / 开源 skill 仓库。</p>
        </div>
        <div class="ai-skill-filter-area">
          <div class="pill-group">
            <button
              v-for="chip in filterChips"
              :key="chip.value"
              class="pill-button"
              :class="{ active: selectedFilter === chip.value }"
              type="button"
              @click="selectedFilter = chip.value"
            >
              {{ chip.label }} · {{ chip.count }}
            </button>
          </div>
          <input v-model="search" class="search-input" type="search" placeholder="搜索技能名称 / 来源 / 说明" />
        </div>
      </div>

      <div v-if="!installedSkills.length" class="empty-state compact">
        还没有安装外部技能。你可以上传技能包，或直接粘贴 GitHub / Gitee 仓库页、直链压缩包地址导入。
      </div>
      <div v-else-if="!filteredInstalledSkills.length" class="empty-state compact">当前筛选条件下没有匹配的技能。</div>
      <div v-else class="ai-installed-skill-grid">
        <article v-for="skill in filteredInstalledSkills" :key="skill.id" class="subcard ai-installed-skill-card">
          <div class="ai-installed-skill-actions-top">
            <label class="switch ai-installed-skill-switch">
              <input
                type="checkbox"
                :checked="skill.enabled"
                :disabled="busy || !!actionBusyId"
                @change="toggleInstalledSkill(skill)"
              />
              <span class="slider"></span>
            </label>
            <div class="ai-installed-skill-icon-actions">
              <button class="secondary-btn compact-skill-btn" type="button" :disabled="busy || detailLoading" @click="openSkillDetail(skill)">说明</button>
              <button class="danger-btn ghost-danger-btn compact-skill-btn" type="button" :disabled="busy || !!actionBusyId" @click="removeInstalledSkill(skill)">卸载</button>
            </div>
          </div>

          <div class="ai-installed-skill-head">
            <div>
              <div class="ai-installed-skill-title-row">
                <h4>{{ skill.name }}</h4>
                <span class="ai-skill-source" :class="skill.enabled ? 'source-enabled' : 'source-disabled'">{{ sourceLabel(skill) }}</span>
              </div>
              <p class="ai-installed-skill-subtitle">{{ skill.description || '未提供技能简介。' }}</p>
            </div>
          </div>

          <p class="ai-installed-skill-preview">{{ skill.instruction_preview || '暂无说明预览。' }}</p>

          <dl class="detail-list ai-installed-skill-meta">
            <div>
              <dt>来源</dt>
              <dd>{{ sourceLabel(skill) }}</dd>
            </div>
            <div>
              <dt>格式</dt>
              <dd>{{ skill.format || '-' }}</dd>
            </div>
            <div>
              <dt>安装时间</dt>
              <dd>{{ formatDateTime(skill.installed_at) }}</dd>
            </div>
            <div>
              <dt>更新时间</dt>
              <dd>{{ formatDateTime(skill.updated_at) }}</dd>
            </div>
          </dl>
        </article>
      </div>
    </section>

    <section class="subcard ai-skill-section-card">
      <div class="ai-skill-section-head compact-head">
        <div>
          <span class="eyebrow">Tool Skills</span>
          <h4>已生效工具技能</h4>
          <p>这部分来自内置核心能力、插件注册工具与外部 provider。</p>
        </div>
      </div>
      <div v-if="!toolCards.length" class="empty-state compact">当前还没有可供 AI 调用的工具技能。</div>
      <div v-else class="ai-tool-card-grid">
        <article v-for="card in toolCards" :key="card.key" class="subcard ai-tool-card">
          <div class="ai-tool-card-actions">
            <span class="ai-skill-source" :class="toolSourceClass(card.skill.source)">{{ toolSourceLabel(card.skill.source) }}</span>
            <label v-if="isBuiltinCLITool(card.skill, card.tool)" class="switch ai-tool-inline-switch">
              <input
                type="checkbox"
                :checked="builtinCLIEnabled"
                :disabled="busy || !configLoaded"
                @change="toggleBuiltinCLIEnabled(($event.target as HTMLInputElement).checked)"
              />
              <span class="slider"></span>
            </label>
          </div>

          <div class="ai-tool-card-head">
            <div class="ai-tool-card-title">
              <h4>{{ toolDisplayName(card.tool) }}</h4>
              <code>{{ card.tool.name }}</code>
            </div>
            <span
              v-if="toolAvailabilityLabel(card.skill, card.tool)"
              class="ai-tool-availability"
              :class="{
                'is-active': isBuiltinCLITool(card.skill, card.tool) && builtinCLIEnabled,
                'is-inactive': isBuiltinCLITool(card.skill, card.tool) && !builtinCLIEnabled,
                'is-pending': isBuiltinCLITool(card.skill, card.tool) && builtinCLIDirty,
              }"
            >
              {{ toolAvailabilityLabel(card.skill, card.tool) }}
            </span>
          </div>

          <p class="ai-tool-card-description">{{ toolDisplayDescription(card.tool) }}</p>

          <div class="ai-tool-card-meta">
            <span>{{ toolProviderLabel(card.skill) }}</span>
            <span>{{ toolSkillSubtitle(card.skill) }}</span>
          </div>

          <p v-if="isBuiltinCLITool(card.skill, card.tool)" class="ai-tool-item-hint">
            开关控制 AI 是否能看到并调用该工具；白名单、超时和输出限制仍在「基础与回复」里配置。
          </p>
        </article>
      </div>
    </section>

    <div v-if="installLogModalOpen" class="plugin-modal-backdrop" role="presentation" @click.self="closeInstallLogModal">
      <section class="plugin-install-log-modal" role="dialog" aria-modal="true" :aria-label="installLogTitle || '技能安装日志'">
        <div class="plugin-modal-head">
          <div>
            <span class="eyebrow">安装日志</span>
            <h3>{{ installLogTitle || '技能安装 / 导入' }}</h3>
            <p>显示本次技能安装或链接导入的后台处理过程。</p>
          </div>
          <button class="plugin-modal-close" type="button" aria-label="关闭安装日志" title="关闭" :disabled="installing" @click="closeInstallLogModal">×</button>
        </div>
        <div ref="installLogViewer" class="plugin-install-log-viewer">
          <div v-for="entry in installLogEntries" :key="`${entry.at}-${entry.text}`" class="plugin-install-log-entry" :class="`tone-${entry.tone}`">
            <span class="plugin-install-log-time">{{ entry.at }}</span>
            <span class="plugin-install-log-text">{{ entry.text }}</span>
          </div>
        </div>
      </section>
    </div>

    <div v-if="detailModalOpen" class="plugin-modal-backdrop" role="presentation" @click.self="closeDetailModal">
      <section class="ai-skill-detail-modal" role="dialog" aria-modal="true" :aria-label="detail?.name || '技能说明'">
        <div class="plugin-modal-head">
          <div>
            <span class="eyebrow">技能说明</span>
            <h3>{{ detail?.name || '加载中...' }}</h3>
            <p>{{ detail?.description || '查看该技能安装后的完整 SKILL.md 内容。' }}</p>
          </div>
          <button class="plugin-modal-close" type="button" aria-label="关闭技能说明" title="关闭" :disabled="detailLoading" @click="closeDetailModal">×</button>
        </div>
        <div v-if="detailError" class="banner banner-danger"><strong>加载失败</strong><span>{{ detailError }}</span></div>
        <div v-else-if="detailLoading" class="empty-state compact">正在加载技能说明...</div>
        <div v-else-if="detail" class="ai-skill-detail-body">
          <dl class="detail-list ai-installed-skill-meta">
            <div>
              <dt>来源</dt>
              <dd>{{ sourceLabel(detail) }}</dd>
            </div>
            <div>
              <dt>入口文件</dt>
              <dd>{{ detail.entry_path || '-' }}</dd>
            </div>
            <div>
              <dt>原始链接</dt>
              <dd class="ai-detail-link">{{ detail.source_url || '-' }}</dd>
            </div>
          </dl>
          <pre class="ai-skill-detail-content">{{ detail.content || '该技能未提供可显示的 SKILL.md 内容。' }}</pre>
        </div>
      </section>
    </div>
  </section>
</template>

<style scoped>
.ai-skill-center {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.ai-skill-summary-grid {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 12px;
}

.ai-skill-summary-card {
  display: grid;
  grid-template-columns: auto minmax(0, 1fr);
  grid-template-areas:
    'value label'
    'value copy';
  align-items: center;
  column-gap: 12px;
  row-gap: 2px;
  min-height: 86px;
  background: color-mix(in srgb, var(--surface-card) 82%, var(--theme-primary) 4%);
}

.ai-skill-summary-card strong {
  grid-area: value;
  font-size: 30px;
  line-height: 1;
}

.ai-skill-summary-card p {
  grid-area: copy;
  line-height: 1.5;
}

.ai-skill-summary-card .ai-skill-summary-label {
  grid-area: label;
}

.ai-skill-summary-label {
  font-size: 12px;
  letter-spacing: 0.08em;
  text-transform: uppercase;
  color: var(--text-muted);
}

.ai-skill-summary-card p,
.ai-installed-skill-subtitle,
.ai-installed-skill-preview {
  margin: 0;
  color: var(--text-secondary);
}

.ai-skill-import-card,
.ai-skill-section-card {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.ai-skill-import-card h4,
.ai-skill-section-head h4,
.ai-installed-skill-title-row h4 {
  margin: 0;
}

.ai-skill-import-card p,
.ai-skill-section-head p {
  margin: 6px 0 0;
  color: var(--text-secondary);
}

.ai-skill-import-actions {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto auto;
  gap: 12px;
  align-items: center;
}

.plugin-upload-input {
  display: none;
}

.ai-skill-section-head {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: flex-start;
}

.ai-skill-filter-area {
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: min(420px, 100%);
}

.pill-group {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.pill-button {
  border: 1px solid var(--border-subtle);
  background: var(--surface-soft);
  color: var(--text-secondary);
  border-radius: 999px;
  padding: 8px 14px;
  font-size: 13px;
  cursor: pointer;
}

.pill-button.active {
  color: var(--theme-primary-strong);
  border-color: color-mix(in srgb, var(--theme-primary) 32%, transparent);
  background: color-mix(in srgb, var(--theme-primary) 14%, var(--surface-soft));
}

.ai-installed-skill-grid,
.ai-tool-card-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 14px;
  max-height: min(86vh, 1040px);
  overflow: auto;
  padding: 2px 6px 2px 2px;
}

.ai-installed-skill-card,
.ai-tool-card {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 218px;
  background: var(--card-bg);
  cursor: default;
  transition: border-color 0.18s ease, box-shadow 0.18s ease, transform 0.18s ease;
}

.ai-installed-skill-card:hover,
.ai-tool-card:hover {
  border-color: color-mix(in srgb, var(--theme-primary) 26%, var(--border-subtle));
  box-shadow: 0 18px 42px rgba(15, 23, 42, 0.08);
  transform: translateY(-1px);
}

.ai-installed-skill-head {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: flex-start;
  min-height: 52px;
}

.ai-installed-skill-actions-top,
.ai-tool-card-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.ai-installed-skill-icon-actions {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.compact-skill-btn {
  min-height: 32px;
  padding: 6px 10px;
  border-radius: 10px;
  font-size: 12px;
}

.ai-installed-skill-title-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.ai-installed-skill-head > div,
.ai-tool-card-title {
  min-width: 0;
}

.ai-installed-skill-title-row h4,
.ai-installed-skill-subtitle,
.ai-tool-card-title h4,
.ai-tool-card-title code {
  overflow: hidden;
  text-overflow: ellipsis;
}

.ai-installed-skill-title-row h4,
.ai-tool-card-title h4,
.ai-tool-card-title code {
  max-width: 100%;
  white-space: nowrap;
}

.ai-installed-skill-meta {
  margin: auto 0 0;
}

.ai-installed-skill-preview,
.ai-tool-card-description {
  min-height: 62px;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}

.ai-skill-source {
  display: inline-flex;
  align-items: center;
  padding: 4px 10px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 700;
}

.ai-skill-source.source-enabled,
.ai-skill-source.source-builtin {
  background: color-mix(in srgb, var(--theme-primary) 16%, transparent);
  color: var(--theme-primary-strong);
}

.ai-skill-source.source-plugin {
  background: color-mix(in srgb, var(--theme-accent) 18%, transparent);
  color: var(--theme-accent-strong);
}

.ai-skill-source.source-mcp {
  background: color-mix(in srgb, var(--theme-primary) 10%, var(--theme-accent) 8%);
  color: var(--theme-primary-strong);
}

.ai-skill-source.source-disabled,
.ai-skill-source.source-custom {
  background: color-mix(in srgb, var(--theme-warning) 16%, transparent);
  color: var(--theme-warning-strong);
}

.ai-tool-inline-switch {
  flex-shrink: 0;
}

.ai-tool-card-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 12px;
}

.ai-tool-card-title {
  display: flex;
  flex-direction: column;
  gap: 5px;
}

.ai-tool-card-title h4 {
  margin: 0;
  font-size: 15px;
}

.ai-tool-card-title code {
  color: var(--text-muted);
  font-size: 12px;
}

.ai-tool-card-description {
  margin: 0;
  color: var(--text-secondary);
  font-size: 13px;
  line-height: 1.6;
}

.ai-tool-card-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: auto;
  color: var(--text-muted);
  font-size: 12px;
}

.ai-tool-item-hint {
  margin: 0;
  font-size: 12px;
  line-height: 1.6;
  color: var(--text-muted);
}

.ai-tool-availability {
  display: flex;
  align-items: center;
  padding: 6px 10px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--theme-primary) 10%, white 90%);
  border: 1px solid var(--border-subtle);
  font-size: 12px;
  color: var(--text-secondary);
  white-space: nowrap;
}

.ai-tool-availability.is-active {
  background: color-mix(in srgb, var(--theme-success) 14%, white 86%);
  color: var(--theme-success-strong);
}

.ai-tool-availability.is-inactive {
  background: color-mix(in srgb, var(--theme-warning) 14%, white 86%);
  color: var(--theme-warning-strong);
}

.ai-tool-availability.is-pending {
  border-color: color-mix(in srgb, var(--theme-primary) 28%, transparent);
}

.plugin-modal-backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.28);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  z-index: 40;
}

.plugin-install-log-modal,
.ai-skill-detail-modal {
  width: min(880px, 100%);
  max-height: min(80vh, 900px);
  overflow: hidden;
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: 22px;
  border-radius: 24px;
  background: var(--surface-card);
  border: 1px solid var(--border-subtle);
  box-shadow: 0 28px 70px rgba(15, 23, 42, 0.18);
}

.plugin-modal-head {
  display: flex;
  justify-content: space-between;
  gap: 16px;
  align-items: flex-start;
}

.plugin-modal-head h3,
.plugin-modal-head p {
  margin: 0;
}

.plugin-modal-head p {
  margin-top: 6px;
  color: var(--text-secondary);
}

.plugin-modal-close {
  border: none;
  width: 38px;
  height: 38px;
  border-radius: 50%;
  background: var(--surface-soft);
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 22px;
}

.plugin-install-log-viewer {
  flex: 1;
  min-height: 240px;
  overflow: auto;
  border-radius: 16px;
  border: 1px solid var(--border-subtle);
  background: var(--surface-soft);
  padding: 14px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.plugin-install-log-entry {
  display: grid;
  grid-template-columns: 90px minmax(0, 1fr);
  gap: 12px;
  align-items: start;
  font-size: 13px;
}

.plugin-install-log-entry.tone-success .plugin-install-log-text {
  color: var(--theme-success-strong);
}

.plugin-install-log-entry.tone-error .plugin-install-log-text {
  color: var(--theme-danger-strong);
}

.plugin-install-log-time {
  color: var(--text-muted);
  font-variant-numeric: tabular-nums;
}

.plugin-install-log-text {
  color: var(--text-primary);
  line-height: 1.6;
  word-break: break-word;
}

.ai-skill-detail-body {
  display: flex;
  flex-direction: column;
  gap: 14px;
  min-height: 0;
}

.ai-skill-detail-body .ai-installed-skill-meta {
  margin: 0;
}

.ai-skill-detail-content {
  margin: 0;
  padding: 16px;
  border-radius: 18px;
  background: var(--surface-soft);
  border: 1px solid var(--border-subtle);
  white-space: pre-wrap;
  word-break: break-word;
  line-height: 1.7;
  overflow: auto;
  min-height: 280px;
  max-height: 52vh;
}

.ai-detail-link {
  word-break: break-all;
}

.ghost-danger-btn {
  background: color-mix(in srgb, var(--theme-danger) 12%, transparent);
  border-color: color-mix(in srgb, var(--theme-danger) 28%, transparent);
  color: var(--theme-danger-strong);
}

@media (max-width: 1220px) {
  .ai-skill-summary-grid,
  .ai-installed-skill-grid,
  .ai-tool-card-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 900px) {
  .ai-skill-import-actions {
    grid-template-columns: 1fr;
  }

  .ai-skill-section-head {
    flex-direction: column;
  }

  .ai-skill-filter-area {
    min-width: 0;
    width: 100%;
  }
}

@media (max-width: 760px) {
  .ai-skill-summary-grid,
  .ai-installed-skill-grid,
  .ai-tool-card-grid {
    grid-template-columns: 1fr;
  }

  .ai-installed-skill-grid,
  .ai-tool-card-grid {
    max-height: none;
  }
}
</style>
