<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { APIError, requestJSON } from '../lib/http'
import { getBoolean, getNumber, getPath, getString, setPath } from '../lib/object-path'
import type { AIProviderModelsResult } from '../types/api'

type PrivatePersonaDraft = {
  id?: string
  name?: string
  description?: string
  bot_name?: string
  system_prompt?: string
  style_tags?: string[]
  enabled?: boolean
}

type GroupPolicyDraft = {
  group_id?: string
  name?: string
  reply_enabled?: boolean
  reply_on_at?: boolean
  reply_on_bot_name?: boolean
  reply_on_quote?: boolean
  cooldown_seconds?: number
  max_context_messages?: number
  max_output_tokens?: number
  vision_enabled?: boolean
  prompt_override?: string
}

type ModelProviderTarget = 'chat' | 'vision'

const props = defineProps<{
  config: Record<string, unknown>
  configLoaded: boolean
  busy: boolean
  apiBasePath?: string
  activeSection?: string
}>()

const emit = defineEmits<{
  'update:config': [value: Record<string, unknown>]
  unauthorized: []
  notice: [payload: { kind: 'success' | 'error' | 'info'; title: string; text: string }]
}>()

const fieldListIDPrefix = `ai-config-${Math.random().toString(36).slice(2)}`

const providerVendorOptions = ['custom', 'openai', 'deepseek', 'anthropic', 'google']
const thinkingModeOptions = ['auto', 'high', 'xhigh']
const thinkingFormatOptions = ['openai', 'anthropic']

const providerOfficialBaseURLs: Record<string, string> = {
  openai: 'https://api.openai.com/v1',
  deepseek: 'https://api.deepseek.com/v1',
  anthropic: 'https://api.anthropic.com/v1',
  google: 'https://generativelanguage.googleapis.com/v1beta/openai',
}

const draft = computed(() => props.config)
const fetchedChatModelOptions = ref<string[]>([])
const fetchedVisionModelOptions = ref<string[]>([])
const fetchedChatModelSignature = ref('')
const fetchedVisionModelSignature = ref('')
const modelFetchTarget = ref<ModelProviderTarget | ''>('')
const modelFetchFeedbackTarget = ref<ModelProviderTarget | ''>('')
const modelFetchError = ref('')
const modelFetchMessage = ref('')

function optionListID(name: string): string {
  return `${fieldListIDPrefix}-${name}`
}

function hasOption(options: string[], value: string): boolean {
  return options.includes(value)
}

function providerVendorLabel(value: string): string {
  switch (value) {
    case 'custom':
      return '自定义 OpenAI 兼容'
    case 'openai':
      return 'OpenAI'
    case 'deepseek':
      return 'DeepSeek'
    case 'anthropic':
      return 'Anthropic'
    case 'google':
      return 'Google'
    default:
      return value
  }
}

function thinkingModeLabel(value: string): string {
  switch (value) {
    case 'xhigh':
      return 'xhigh（最高强度）'
    case 'high':
      return 'high（高强度）'
    case 'auto':
      return 'auto（自动）'
    default:
      return value
  }
}

function thinkingFormatLabel(value: string): string {
  switch (value) {
    case 'anthropic':
      return 'Anthropic 格式'
    case 'openai':
      return 'OpenAI 兼容格式'
    default:
      return value
  }
}

function officialProviderBaseURL(vendor: string): string {
  return providerOfficialBaseURLs[vendor] || ''
}

function providerVendorValue(target: ModelProviderTarget): string {
  return text(`${providerPath(target)}.vendor`) || 'custom'
}

function providerBaseURLValue(target: ModelProviderTarget): string {
  const vendor = providerVendorValue(target)
  return text(`${providerPath(target)}.base_url`) || officialProviderBaseURL(vendor)
}

function setProviderVendor(target: ModelProviderTarget, vendor: string) {
  const prefix = providerPath(target)
  const nextDraft = { ...draft.value }
  setPath(nextDraft, `${prefix}.vendor`, vendor)
  const officialBaseURL = officialProviderBaseURL(vendor)
  if (officialBaseURL) {
    setPath(nextDraft, `${prefix}.base_url`, officialBaseURL)
  }
  emit('update:config', nextDraft)
}

function syncProviderBaseURL(target: ModelProviderTarget) {
  const vendor = providerVendorValue(target)
  const officialBaseURL = officialProviderBaseURL(vendor)
  if (!officialBaseURL) return
  const path = `${providerPath(target)}.base_url`
  if (!text(path).trim()) {
    setText(path, officialBaseURL)
  }
}

function updateDraft(path: string, value: unknown) {
  const newDraft = { ...draft.value }
  setPath(newDraft, path, value)
  emit('update:config', newDraft)
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

function stringArrayValue(path: string): string[] {
  const value = getPath(draft.value, path, [])
  if (!Array.isArray(value)) return []
  return value
    .map((item) => String(item ?? '').trim())
    .filter(Boolean)
}

function stringArrayLines(path: string): string {
  return stringArrayValue(path).join('\n')
}

function setStringArray(path: string, value: string) {
  updateDraft(
    path,
    value
      .split(/\r?\n/)
      .map((item) => item.trim())
      .filter(Boolean),
  )
}

function setText(path: string, value: string) {
  updateDraft(path, value)
}

function setNumber(path: string, value: string | number) {
  const number = Number(value)
  updateDraft(path, Number.isFinite(number) ? number : 0)
}

function setBool(path: string, value: boolean) {
  updateDraft(path, value)
}

function thinkingEnabled(): boolean {
  const mode = text('reply.thinking_mode').trim().toLowerCase()
  return !(mode === 'disabled' || mode === 'disable' || mode === 'off' || mode === 'false')
}

function thinkingModeValue(): string {
  const mode = text('reply.thinking_mode').trim().toLowerCase()
  if (mode === 'xhigh' || mode === 'max') return 'xhigh'
  if (mode === 'high') return 'high'
  if (mode === 'enabled' || mode === 'enable' || mode === 'on' || mode === 'true') {
    const effort = text('reply.thinking_effort').trim().toLowerCase()
    return effort === 'max' || effort === 'xhigh' ? 'xhigh' : 'high'
  }
  return 'auto'
}

function setThinkingEnabled(value: boolean) {
  if (!value) {
    setText('reply.thinking_mode', 'disabled')
    return
  }
  setThinkingMode(thinkingModeValue())
}

function setThinkingMode(value: string) {
  const mode = value === 'xhigh' || value === 'high' ? value : 'auto'
  setText('reply.thinking_mode', mode)
  setText('reply.thinking_effort', mode === 'xhigh' ? 'max' : 'high')
}

function thinkingFormatValue(): string {
  return text('reply.thinking_format').trim().toLowerCase() === 'anthropic' ? 'anthropic' : 'openai'
}

function providerPath(target: ModelProviderTarget): string {
  return target === 'vision' ? 'vision.provider' : 'provider'
}

function providerPayload(target: ModelProviderTarget): Record<string, unknown> {
  const prefix = providerPath(target)
  return {
    kind: text(`${prefix}.kind`) || 'openai_compatible',
    vendor: text(`${prefix}.vendor`) || 'custom',
    base_url: providerBaseURLValue(target),
    api_key: text(`${prefix}.api_key`),
    model: text(`${prefix}.model`),
    timeout_ms: numberValue(`${prefix}.timeout_ms`) || 30000,
    temperature: numberValue(`${prefix}.temperature`) || 0.8,
  }
}

function providerSignature(target: ModelProviderTarget): string {
  const payload = providerPayload(target)
  return [
    String(payload.kind || ''),
    String(payload.vendor || ''),
    String(payload.base_url || ''),
    String(payload.api_key || ''),
  ].join('|')
}

function providerReady(target: ModelProviderTarget): boolean {
  const payload = providerPayload(target)
  return String(payload.kind || 'openai_compatible') === 'openai_compatible' && String(payload.base_url || '').trim() !== ''
}

function modelOptions(target: ModelProviderTarget): string[] {
  const signature = providerSignature(target)
  if (target === 'vision') {
    return fetchedVisionModelSignature.value === signature ? fetchedVisionModelOptions.value : []
  }
  return fetchedChatModelSignature.value === signature ? fetchedChatModelOptions.value : []
}

async function discoverModels(target: ModelProviderTarget) {
  if (props.activeSection !== 'model') return
  if (modelFetchTarget.value || props.busy) return
  const apiBasePath = props.apiBasePath || ''
  if (!apiBasePath) return
  if (!providerReady(target)) {
    modelFetchFeedbackTarget.value = target
    modelFetchError.value = '请先填写 Base URL，再获取模型。'
    return
  }

  modelFetchTarget.value = target
  modelFetchFeedbackTarget.value = target
  modelFetchError.value = ''
  modelFetchMessage.value = ''
  try {
    const result = await requestJSON<AIProviderModelsResult>(apiBasePath + '/ai/models', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(providerPayload(target)),
    })
    const models = (result.models || []).map((item) => item.id).filter(Boolean)
    const signature = providerSignature(target)
    if (target === 'vision') {
      fetchedVisionModelOptions.value = models
      fetchedVisionModelSignature.value = signature
    } else {
      fetchedChatModelOptions.value = models
      fetchedChatModelSignature.value = signature
    }
    modelFetchMessage.value = result.message || `已获取 ${models.length} 个可用模型。`
    emit('notice', { kind: 'success', title: '模型列表已获取', text: modelFetchMessage.value })
  } catch (error) {
    if (error instanceof APIError && error.status === 401) {
      emit('unauthorized')
      return
    }
    const message = formatModelFetchError(error)
    modelFetchError.value = message
    emit('notice', { kind: 'error', title: '模型列表获取失败', text: message })
  } finally {
    modelFetchTarget.value = ''
  }
}

function formatModelFetchError(error: unknown): string {
  if (error instanceof APIError) {
    const detail = error.payload && typeof error.payload === 'object' && 'detail' in error.payload
      ? String((error.payload as Record<string, unknown>).detail || '')
      : ''
    return detail || error.message || '模型列表获取失败。'
  }
  if (error instanceof Error) return error.message
  return '模型列表获取失败。'
}

function arrayDraft<T extends Record<string, unknown>>(path: string): T[] {
  const value = draft.value[path]
  return Array.isArray(value) ? (value as T[]) : []
}

function writeArrayDraft<T extends Record<string, unknown>>(path: string, value: T[]) {
  updateDraft(path, value)
}

const privatePersonas = computed<PrivatePersonaDraft[]>(() => arrayDraft<PrivatePersonaDraft>('private_personas'))
const groupPolicies = computed<GroupPolicyDraft[]>(() => arrayDraft<GroupPolicyDraft>('group_policies'))

function splitTags(value: string): string[] {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
}

function joinTags(value: string[] | undefined): string {
  return Array.isArray(value) ? value.join(', ') : ''
}

function addPrivatePersona() {
  writeArrayDraft('private_personas', [
    ...privatePersonas.value,
    {
      id: `persona_${Date.now()}`,
      name: '新人格',
      description: '',
      bot_name: '',
      system_prompt: '',
      style_tags: [],
      enabled: true,
    },
  ])
}

function updatePrivatePersona(index: number, key: keyof PrivatePersonaDraft, value: string | boolean | string[]) {
  const next = privatePersonas.value.map((item, itemIndex) => (itemIndex === index ? { ...item, [key]: value } : item))
  writeArrayDraft('private_personas', next)
}

function removePrivatePersona(index: number) {
  writeArrayDraft('private_personas', privatePersonas.value.filter((_, itemIndex) => itemIndex !== index))
}

function addGroupPolicy() {
  writeArrayDraft('group_policies', [
    ...groupPolicies.value,
    {
      group_id: '',
      name: '新群策略',
      reply_enabled: true,
      reply_on_at: boolValue('reply.reply_on_at'),
      reply_on_bot_name: boolValue('reply.reply_on_bot_name'),
      reply_on_quote: boolValue('reply.reply_on_quote'),
      cooldown_seconds: numberValue('reply.cooldown_seconds') || 20,
      max_context_messages: numberValue('reply.max_context_messages') || 16,
      max_output_tokens: numberValue('reply.max_output_tokens') || 160,
      vision_enabled: boolValue('vision.enabled'),
      prompt_override: '',
    },
  ])
}

function updateGroupPolicy(index: number, key: keyof GroupPolicyDraft, value: string | boolean | number) {
  const next = groupPolicies.value.map((item, itemIndex) => (itemIndex === index ? { ...item, [key]: value } : item))
  writeArrayDraft('group_policies', next)
}

function removeGroupPolicy(index: number) {
  writeArrayDraft('group_policies', groupPolicies.value.filter((_, itemIndex) => itemIndex !== index))
}

watch(
  () => [props.configLoaded, providerVendorValue('chat')],
  () => {
    if (props.configLoaded) {
      syncProviderBaseURL('chat')
    }
  },
)

watch(
  () => [props.configLoaded, text('vision.mode'), providerVendorValue('vision')],
  () => {
    if (props.configLoaded && text('vision.mode') === 'independent') {
      syncProviderBaseURL('vision')
    }
  },
)


</script>

<template>
  <div class="ai-config-wrapper">
    <section v-show="!props.activeSection || props.activeSection === 'base'" class="ai-base-stack">
      <div v-if="!configLoaded" class="card ai-config-card">
        <div class="section-head compact">
          <div>
            <span class="eyebrow">配置</span>
            <h3>基础与回复</h3>
          </div>
        </div>
        <div class="empty-state compact">AI 配置仍在加载中。</div>
      </div>

      <template v-else>
        <div class="ai-provider-layout">
          <article class="card ai-config-card ai-provider-card">
            <div class="section-head compact ai-provider-head">
              <div>
                <span class="eyebrow">基础</span>
                <h3>运行状态</h3>
              </div>
              <span class="ai-provider-caption">Runtime</span>
            </div>
            <div class="ai-toggle-list">
              <label class="checkbox-row ai-toggle-item">
                <input type="checkbox" :checked="boolValue('enabled')" @change="setBool('enabled', ($event.target as HTMLInputElement).checked)" />
                <span>启用 AI 核心</span>
              </label>
              <div class="ai-runtime-summary">
                <div class="ai-runtime-badge" :class="{ active: boolValue('enabled') }">
                  {{ boolValue('enabled') ? '当前已启用' : '当前未启用' }}
                </div>
                <p class="ai-runtime-copy">关闭后将停止 AI 回复与图片识别处理。</p>
                <div class="ai-runtime-meta">
                  <span>群聊：{{ boolValue('reply.enabled_in_group') ? '开' : '关' }}</span>
                  <span>私聊：{{ boolValue('reply.enabled_in_private') ? '开' : '关' }}</span>
                  <span>识图：{{ boolValue('vision.enabled') ? '开' : '关' }}</span>
                  <span>思考：{{ thinkingEnabled() ? thinkingModeValue() : '关' }}</span>
                </div>
              </div>
            </div>
          </article>

          <article class="card ai-config-card ai-provider-card">
            <div class="section-head compact ai-provider-head">
              <div>
                <span class="eyebrow">回复</span>
                <h3>回复开关</h3>
              </div>
              <span class="ai-provider-caption">Reply Scope</span>
            </div>
            <div class="ai-toggle-list">
              <label class="checkbox-row ai-toggle-item">
                <input type="checkbox" :checked="boolValue('reply.enabled_in_group')" @change="setBool('reply.enabled_in_group', ($event.target as HTMLInputElement).checked)" />
                <span>群聊回复</span>
              </label>
              <label class="checkbox-row ai-toggle-item">
                <input type="checkbox" :checked="boolValue('reply.enabled_in_private')" @change="setBool('reply.enabled_in_private', ($event.target as HTMLInputElement).checked)" />
                <span>私聊回复</span>
              </label>
              <label class="checkbox-row ai-toggle-item">
                <input type="checkbox" :checked="boolValue('reply.reply_on_at')" @change="setBool('reply.reply_on_at', ($event.target as HTMLInputElement).checked)" />
                <span>被 @ 时回复</span>
              </label>
              <label class="checkbox-row ai-toggle-item">
                <input type="checkbox" :checked="boolValue('reply.reply_on_bot_name')" @change="setBool('reply.reply_on_bot_name', ($event.target as HTMLInputElement).checked)" />
                <span>提到机器人昵称时回复</span>
              </label>
              <label class="checkbox-row ai-toggle-item">
                <input type="checkbox" :checked="boolValue('reply.reply_on_quote')" @change="setBool('reply.reply_on_quote', ($event.target as HTMLInputElement).checked)" />
                <span>引用机器人消息时回复</span>
              </label>
            </div>
          </article>
        </div>

        <article class="card ai-config-card ai-base-parameter-card">
          <div class="section-head compact ai-provider-head">
            <div>
              <span class="eyebrow">参数</span>
              <h3>回复参数</h3>
            </div>
            <span class="ai-provider-caption">Reply Limits</span>
          </div>
          <div class="ai-provider-form ai-parameter-grid">
            <label class="schema-field">
              <span class="schema-label-row"><strong>冷却秒数</strong><small>reply.cooldown_seconds</small></span>
              <input class="text-control" type="number" :value="numberValue('reply.cooldown_seconds')" @input="setNumber('reply.cooldown_seconds', ($event.target as HTMLInputElement).value)" />
            </label>
            <label class="schema-field">
              <span class="schema-label-row"><strong>上下文消息数</strong><small>reply.max_context_messages</small></span>
              <input class="text-control" type="number" :value="numberValue('reply.max_context_messages')" @input="setNumber('reply.max_context_messages', ($event.target as HTMLInputElement).value)" />
            </label>
            <label class="schema-field">
              <span class="schema-label-row"><strong>最大输出 Token</strong><small>reply.max_output_tokens</small></span>
              <input class="text-control" type="number" :value="numberValue('reply.max_output_tokens')" @input="setNumber('reply.max_output_tokens', ($event.target as HTMLInputElement).value)" />
            </label>
            <label class="checkbox-row ai-toggle-item ai-thinking-toggle ai-provider-span-two">
              <input type="checkbox" :checked="thinkingEnabled()" @change="setThinkingEnabled(($event.target as HTMLInputElement).checked)" />
              <span>
                <strong>思考模式</strong>
                <small>关闭后会发送 thinking.disabled；开启后可在下方选择 auto / high / xhigh。</small>
              </span>
            </label>
            <label class="schema-field">
              <span class="schema-label-row"><strong>思考档位</strong><small>reply.thinking_mode</small></span>
              <select class="text-control" :value="thinkingModeValue()" :disabled="!thinkingEnabled()" @change="setThinkingMode(($event.target as HTMLSelectElement).value)">
                <option v-for="item in thinkingModeOptions" :key="item" :value="item">{{ thinkingModeLabel(item) }}</option>
              </select>
              <small class="field-hint">auto 交给模型或服务商默认；high / xhigh 会显式开启思考并设置强度。</small>
            </label>
            <label class="schema-field">
              <span class="schema-label-row"><strong>控制参数格式</strong><small>reply.thinking_format</small></span>
              <select class="text-control" :value="thinkingFormatValue()" @change="setText('reply.thinking_format', ($event.target as HTMLSelectElement).value)">
                <option v-for="item in thinkingFormatOptions" :key="item" :value="item">{{ thinkingFormatLabel(item) }}</option>
              </select>
            </label>
            <div class="ai-provider-span-two inline-note ai-thinking-note">
              auto 不主动下发思考控制；high 会发送 enabled + high；xhigh 会发送 enabled + max。OpenAI 兼容格式使用 reasoning_effort，Anthropic 格式使用 output_config.effort。
            </div>
          </div>
        </article>

        <article class="card ai-config-card ai-base-parameter-card ai-social-card">
          <div class="section-head compact ai-provider-head">
            <div>
              <span class="eyebrow">群友感</span>
              <h3>自然聊天</h3>
            </div>
            <span class="ai-provider-caption">Social Chat</span>
          </div>
          <div class="ai-social-grid">
            <section class="ai-social-panel">
              <label class="checkbox-row ai-toggle-item ai-social-toggle">
                <input type="checkbox" :checked="boolValue('proactive.enabled')" @change="setBool('proactive.enabled', ($event.target as HTMLInputElement).checked)" />
                <span>
                  <strong>群聊主动参与</strong>
                  <small>群里活跃时低频自然接一句，默认避开问句、冷却和安静时段。</small>
                </span>
              </label>
              <div class="ai-provider-form ai-social-form">
                <label class="schema-field">
                  <span class="schema-label-row"><strong>触发概率</strong><small>proactive.probability</small></span>
                  <input class="text-control" type="number" min="0.01" max="1" step="0.01" :value="numberValue('proactive.probability')" @input="setNumber('proactive.probability', ($event.target as HTMLInputElement).value)" />
                </label>
                <label class="schema-field">
                  <span class="schema-label-row"><strong>最小间隔（秒）</strong><small>proactive.min_interval_seconds</small></span>
                  <input class="text-control" type="number" min="30" :value="numberValue('proactive.min_interval_seconds')" @input="setNumber('proactive.min_interval_seconds', ($event.target as HTMLInputElement).value)" />
                </label>
                <label class="schema-field">
                  <span class="schema-label-row"><strong>每群每日上限</strong><small>proactive.daily_limit_per_group</small></span>
                  <input class="text-control" type="number" min="1" :value="numberValue('proactive.daily_limit_per_group')" @input="setNumber('proactive.daily_limit_per_group', ($event.target as HTMLInputElement).value)" />
                </label>
                <label class="schema-field">
                  <span class="schema-label-row"><strong>窗口消息数</strong><small>proactive.min_recent_messages</small></span>
                  <input class="text-control" type="number" min="1" :value="numberValue('proactive.min_recent_messages')" @input="setNumber('proactive.min_recent_messages', ($event.target as HTMLInputElement).value)" />
                </label>
                <label class="schema-field">
                  <span class="schema-label-row"><strong>活跃窗口（秒）</strong><small>proactive.recent_window_seconds</small></span>
                  <input class="text-control" type="number" min="60" :value="numberValue('proactive.recent_window_seconds')" @input="setNumber('proactive.recent_window_seconds', ($event.target as HTMLInputElement).value)" />
                </label>
                <label class="schema-field ai-provider-span-two">
                  <span class="schema-label-row"><strong>安静时段</strong><small>proactive.quiet_hours</small></span>
                  <textarea class="text-control" rows="3" :value="stringArrayLines('proactive.quiet_hours')" placeholder="每行一个，例如：
00:00-08:00" @input="setStringArray('proactive.quiet_hours', ($event.target as HTMLTextAreaElement).value)" />
                </label>
              </div>
            </section>

            <section class="ai-social-panel">
              <label class="checkbox-row ai-toggle-item ai-social-toggle">
                <input type="checkbox" :checked="boolValue('reply.split.enabled')" @change="setBool('reply.split.enabled', ($event.target as HTMLInputElement).checked)" />
                <span>
                  <strong>闲聊拆句</strong>
                  <small>闲聊和主动插话过长时拆成多条发送，问答和工具结果保持完整。</small>
                </span>
              </label>
              <div class="ai-provider-form ai-social-form">
                <label class="checkbox-row ai-toggle-item ai-social-toggle ai-provider-span-two">
                  <input type="checkbox" :checked="boolValue('reply.split.only_casual')" @change="setBool('reply.split.only_casual', ($event.target as HTMLInputElement).checked)" />
                  <span>
                    <strong>只拆闲聊消息</strong>
                    <small>关闭后会对所有较长回复生效，不建议在工具结果里开启。</small>
                  </span>
                </label>
                <label class="schema-field">
                  <span class="schema-label-row"><strong>单句目标字数</strong><small>reply.split.max_chars</small></span>
                  <input class="text-control" type="number" min="20" :value="numberValue('reply.split.max_chars')" @input="setNumber('reply.split.max_chars', ($event.target as HTMLInputElement).value)" />
                </label>
                <label class="schema-field">
                  <span class="schema-label-row"><strong>最多拆成几条</strong><small>reply.split.max_parts</small></span>
                  <input class="text-control" type="number" min="1" max="6" :value="numberValue('reply.split.max_parts')" @input="setNumber('reply.split.max_parts', ($event.target as HTMLInputElement).value)" />
                </label>
                <label class="schema-field">
                  <span class="schema-label-row"><strong>发送间隔（毫秒）</strong><small>reply.split.delay_ms</small></span>
                  <input class="text-control" type="number" min="0" step="50" :value="numberValue('reply.split.delay_ms')" @input="setNumber('reply.split.delay_ms', ($event.target as HTMLInputElement).value)" />
                </label>
              </div>
            </section>
          </div>
        </article>

        <article class="card ai-config-card ai-base-parameter-card">
          <div class="section-head compact ai-provider-head">
            <div>
              <span class="eyebrow">核心技能</span>
              <h3>CLI 调用能力</h3>
            </div>
            <span class="ai-provider-caption">CLI Tool</span>
          </div>
          <div class="ai-provider-form ai-parameter-grid">
            <div class="ai-provider-span-two inline-note">
              启用开关已移到「技能」里的“执行白名单 CLI”卡片；这里保留白名单、超时和输出限制配置。
            </div>
            <label class="schema-field">
              <span class="schema-label-row"><strong>超时（秒）</strong><small>cli.timeout_seconds</small></span>
              <input class="text-control" type="number" :value="numberValue('cli.timeout_seconds')" @input="setNumber('cli.timeout_seconds', ($event.target as HTMLInputElement).value)" />
            </label>
            <label class="schema-field">
              <span class="schema-label-row"><strong>单路最大输出字节</strong><small>cli.max_output_bytes</small></span>
              <input class="text-control" type="number" :value="numberValue('cli.max_output_bytes')" @input="setNumber('cli.max_output_bytes', ($event.target as HTMLInputElement).value)" />
            </label>
            <label class="schema-field ai-provider-span-two">
              <span class="schema-label-row"><strong>允许调用的命令白名单</strong><small>cli.allowed_commands</small></span>
              <textarea class="text-control" rows="5" :value="stringArrayLines('cli.allowed_commands')" placeholder="每行一个，例如：
git
go
python3" @input="setStringArray('cli.allowed_commands', ($event.target as HTMLTextAreaElement).value)" />
            </label>
          </div>
        </article>
      </template>
    </section>

    <section v-show="!props.activeSection || props.activeSection === 'model'" class="ai-model-stack">
      <datalist :id="optionListID('chat-model')">
        <option v-for="item in modelOptions('chat')" :key="item" :value="item"></option>
      </datalist>
      <datalist :id="optionListID('vision-model')">
        <option v-for="item in modelOptions('vision')" :key="item" :value="item"></option>
      </datalist>

      <div v-if="!configLoaded" class="card ai-config-card">
        <div class="section-head compact">
          <div>
            <span class="eyebrow">模型服务</span>
            <h3>模型与视觉</h3>
          </div>
        </div>
        <div class="empty-state compact">AI 配置仍在加载中。</div>
      </div>

      <template v-else>
        <div class="ai-provider-layout">
          <article class="card ai-config-card ai-provider-card">
            <div class="section-head compact ai-provider-head">
              <div>
                <span class="eyebrow">模型服务</span>
                <h3>文本 AI 服务商</h3>
              </div>
              <span class="ai-provider-caption">Chat Provider</span>
            </div>
            <div class="ai-provider-form ai-provider-form-single">
              <label class="schema-field">
                <span class="schema-label-row"><strong>AI 服务商</strong><small>provider.vendor</small></span>
                <select class="text-control" :value="providerVendorValue('chat')" @change="setProviderVendor('chat', ($event.target as HTMLSelectElement).value)">
                  <option v-if="text('provider.vendor') && !hasOption(providerVendorOptions, text('provider.vendor'))" :value="text('provider.vendor')">{{ text('provider.vendor') }}</option>
                  <option v-for="item in providerVendorOptions" :key="item" :value="item">{{ providerVendorLabel(item) }}</option>
                </select>
              </label>
              <label class="schema-field ai-provider-span-two">
                <span class="schema-label-row"><strong>Base URL</strong><small>provider.base_url</small></span>
                <input class="text-control ai-baseurl-input" type="text" :value="providerBaseURLValue('chat')" placeholder="https://api.openai.com/v1" @input="setText('provider.base_url', ($event.target as HTMLInputElement).value)" />
              </label>
              <label class="schema-field ai-provider-span-two">
                <span class="schema-label-row"><strong>API Key</strong><small>provider.api_key</small></span>
                <input class="text-control" type="password" :value="text('provider.api_key')" autocomplete="new-password" @input="setText('provider.api_key', ($event.target as HTMLInputElement).value)" />
              </label>
              <p v-if="modelFetchFeedbackTarget === 'chat' && modelFetchError" class="error-copy ai-provider-span-two">{{ modelFetchError }}</p>
              <p v-else-if="modelFetchFeedbackTarget === 'chat' && modelFetchMessage" class="inline-note ai-provider-span-two">{{ modelFetchMessage }}</p>
            </div>
          </article>

          <article class="card ai-config-card ai-provider-card">
            <div class="section-head compact ai-provider-head">
              <div>
                <span class="eyebrow">参数</span>
                <h3>模型参数</h3>
              </div>
              <span class="ai-provider-caption">请求控制</span>
            </div>
            <div class="ai-provider-form ai-parameter-grid">
              <label class="schema-field ai-provider-span-two">
                <span class="schema-label-row"><strong>模型</strong><small>provider.model</small></span>
                <div class="model-field-row">
                  <input class="text-control" type="text" :list="optionListID('chat-model')" :value="text('provider.model')" placeholder="可选择已获取模型，也可自定义输入" @input="setText('provider.model', ($event.target as HTMLInputElement).value)" />
                  <button class="secondary-btn model-fetch-btn" type="button" :disabled="props.busy || !!modelFetchTarget" @click="discoverModels('chat')">{{ modelFetchTarget === 'chat' ? '获取中...' : '获取模型' }}</button>
                </div>
              </label>
              <label class="schema-field">
                <span class="schema-label-row"><strong>请求超时（毫秒）</strong><small>provider.timeout_ms</small></span>
                <input class="text-control" type="number" :value="numberValue('provider.timeout_ms')" @input="setNumber('provider.timeout_ms', ($event.target as HTMLInputElement).value)" />
              </label>
              <label class="schema-field">
                <span class="schema-label-row"><strong>温度</strong><small>provider.temperature</small></span>
                <input class="text-control" type="number" step="0.1" :value="numberValue('provider.temperature')" @input="setNumber('provider.temperature', ($event.target as HTMLInputElement).value)" />
              </label>
            </div>
          </article>
        </div>

        <article class="card ai-config-card ai-vision-card">
          <div class="section-head compact ai-provider-head">
            <div>
              <span class="eyebrow">视觉</span>
              <h3>图片识别服务商</h3>
            </div>
            <span class="ai-provider-caption">Vision Provider</span>
          </div>
          <div class="ai-vision-layout">
            <div class="ai-vision-topbar">
              <div class="ai-vision-switch-panel">
                <div class="ai-vision-switch-copy">
                  <span class="ai-vision-switch-kicker">视觉开关</span>
                  <strong>启用图片识别</strong>
                  <small>关闭后不会处理图片消息。</small>
                </div>
                <label class="checkbox-row ai-vision-switch-action">
                  <input type="checkbox" :checked="boolValue('vision.enabled')" @change="setBool('vision.enabled', ($event.target as HTMLInputElement).checked)" />
                  <span>{{ boolValue('vision.enabled') ? '已启用' : '未启用' }}</span>
                </label>
              </div>
              <label class="schema-field ai-vision-mode-panel">
                <span class="schema-label-row"><strong>图片识别模式</strong><small>vision.mode</small></span>
                <select class="text-control" :value="text('vision.mode') || 'same_as_chat'" :disabled="!boolValue('vision.enabled')" @change="setText('vision.mode', ($event.target as HTMLSelectElement).value)">
                  <option value="same_as_chat">与 AI 服务商一致</option>
                  <option value="independent">独立配置图片识别服务商</option>
                </select>
              </label>
            </div>

            <div v-if="!boolValue('vision.enabled')" class="inline-note ai-vision-follow-note">图片识别已关闭，开启后可选择跟随文本模型或独立配置。</div>

            <div v-else-if="text('vision.mode') !== 'independent'" class="inline-note ai-vision-follow-note">图片识别将复用上方文本 AI 服务商与模型参数。</div>

            <template v-else>
              <div class="ai-provider-form ai-parameter-grid ai-vision-provider-form">
                <label class="schema-field">
                  <span class="schema-label-row"><strong>图片识别服务商</strong><small>vision.provider.vendor</small></span>
                  <select class="text-control" :value="providerVendorValue('vision')" @change="setProviderVendor('vision', ($event.target as HTMLSelectElement).value)">
                    <option v-if="text('vision.provider.vendor') && !hasOption(providerVendorOptions, text('vision.provider.vendor'))" :value="text('vision.provider.vendor')">{{ text('vision.provider.vendor') }}</option>
                    <option v-for="item in providerVendorOptions" :key="item" :value="item">{{ providerVendorLabel(item) }}</option>
                  </select>
                </label>
                <label class="schema-field ai-provider-span-two">
                  <span class="schema-label-row"><strong>Base URL</strong><small>vision.provider.base_url</small></span>
                  <input class="text-control ai-baseurl-input" type="text" :value="providerBaseURLValue('vision')" placeholder="https://api.openai.com/v1" @input="setText('vision.provider.base_url', ($event.target as HTMLInputElement).value)" />
                </label>
                <label class="schema-field ai-provider-span-two">
                  <span class="schema-label-row"><strong>API Key</strong><small>vision.provider.api_key</small></span>
                  <input class="text-control" type="password" :value="text('vision.provider.api_key')" autocomplete="new-password" @input="setText('vision.provider.api_key', ($event.target as HTMLInputElement).value)" />
                </label>
                <label class="schema-field ai-provider-span-two">
                  <span class="schema-label-row"><strong>图片识别模型</strong><small>vision.provider.model</small></span>
                  <div class="model-field-row">
                    <input class="text-control" type="text" :list="optionListID('vision-model')" :value="text('vision.provider.model')" placeholder="可选择已获取模型，也可自定义输入" @input="setText('vision.provider.model', ($event.target as HTMLInputElement).value)" />
                    <button class="secondary-btn model-fetch-btn" type="button" :disabled="props.busy || !!modelFetchTarget" @click="discoverModels('vision')">{{ modelFetchTarget === 'vision' ? '获取中...' : '获取模型' }}</button>
                  </div>
                </label>
                <label class="schema-field">
                  <span class="schema-label-row"><strong>请求超时（毫秒）</strong><small>vision.provider.timeout_ms</small></span>
                  <input class="text-control" type="number" :value="numberValue('vision.provider.timeout_ms')" @input="setNumber('vision.provider.timeout_ms', ($event.target as HTMLInputElement).value)" />
                </label>
                <label class="schema-field">
                  <span class="schema-label-row"><strong>温度（可选）</strong><small>vision.provider.temperature</small></span>
                  <input class="text-control" type="number" step="0.1" :value="numberValue('vision.provider.temperature')" @input="setNumber('vision.provider.temperature', ($event.target as HTMLInputElement).value)" />
                </label>
                <p v-if="modelFetchFeedbackTarget === 'vision' && modelFetchError" class="error-copy ai-provider-span-two">{{ modelFetchError }}</p>
                <p v-else-if="modelFetchFeedbackTarget === 'vision' && modelFetchMessage" class="inline-note ai-provider-span-two">{{ modelFetchMessage }}</p>
              </div>
            </template>
          </div>
        </article>
      </template>
    </section>

    <section v-show="!props.activeSection || props.activeSection === 'persona'" class="card ai-config-card ai-persona-card">
      <div class="section-head compact">
        <div>
          <span class="eyebrow">人格</span>
          <h3>私聊人格</h3>
        </div>
        <button class="secondary-btn" type="button" :disabled="!configLoaded || busy" @click="addPrivatePersona">新增人格</button>
      </div>
      <label class="schema-field">
        <span class="schema-label-row"><strong>当前激活人格 ID</strong><small>private_active_persona_id</small></span>
        <select class="text-control" :value="text('private_active_persona_id')" @change="setText('private_active_persona_id', ($event.target as HTMLSelectElement).value)">
          <option value="">未设置</option>
          <option v-for="persona in privatePersonas" :key="persona.id || persona.name" :value="persona.id || ''">
            {{ persona.name || persona.id || '未命名人格' }}
          </option>
        </select>
      </label>
      <div v-if="!privatePersonas.length" class="empty-state compact">当前没有配置私聊人格。</div>
      <div v-else class="ai-draft-list">
        <article v-for="(persona, index) in privatePersonas" :key="persona.id || index" class="ai-draft-card">
          <div class="section-head compact">
            <div>
              <span class="eyebrow">人格 {{ index + 1 }}</span>
              <h3>{{ persona.name || persona.id || '未命名人格' }}</h3>
            </div>
            <button class="danger-btn slim-btn" type="button" :disabled="!configLoaded || busy" @click="removePrivatePersona(index)">删除</button>
          </div>
          <div class="config-grid ai-config-grid">
            <label class="checkbox-row">
              <input type="checkbox" :checked="!!persona.enabled" @change="updatePrivatePersona(index, 'enabled', ($event.target as HTMLInputElement).checked)" />
              <span>启用</span>
            </label>
            <label class="schema-field">
              <span class="schema-label-row"><strong>ID</strong><small>id</small></span>
              <input class="text-control" type="text" :value="persona.id || ''" @input="updatePrivatePersona(index, 'id', ($event.target as HTMLInputElement).value)" />
            </label>
            <label class="schema-field">
              <span class="schema-label-row"><strong>名称</strong><small>name</small></span>
              <input class="text-control" type="text" :value="persona.name || ''" @input="updatePrivatePersona(index, 'name', ($event.target as HTMLInputElement).value)" />
            </label>
            <label class="schema-field">
              <span class="schema-label-row"><strong>机器人名</strong><small>bot_name</small></span>
              <input class="text-control" type="text" :value="persona.bot_name || ''" @input="updatePrivatePersona(index, 'bot_name', ($event.target as HTMLInputElement).value)" />
            </label>
            <label class="schema-field">
              <span class="schema-label-row"><strong>描述</strong><small>description</small></span>
              <input class="text-control" type="text" :value="persona.description || ''" @input="updatePrivatePersona(index, 'description', ($event.target as HTMLInputElement).value)" />
            </label>
            <label class="schema-field">
              <span class="schema-label-row"><strong>风格标签</strong><small>style_tags</small></span>
              <input class="text-control" type="text" :value="joinTags(persona.style_tags)" placeholder="温柔、简洁、活泼" @input="updatePrivatePersona(index, 'style_tags', splitTags(($event.target as HTMLInputElement).value))" />
            </label>
            <label class="schema-field ai-wide-field">
              <span class="schema-label-row"><strong>系统提示词</strong><small>system_prompt</small></span>
              <textarea class="json-editor compact-editor" :value="persona.system_prompt || ''" @input="updatePrivatePersona(index, 'system_prompt', ($event.target as HTMLTextAreaElement).value)"></textarea>
            </label>
          </div>
        </article>
      </div>
    </section>

    <section v-show="!props.activeSection || props.activeSection === 'group'" class="card ai-config-card ai-policy-card">
      <div class="section-head compact">
        <div>
          <span class="eyebrow">群策略</span>
          <h3>群策略</h3>
        </div>
        <button class="secondary-btn" type="button" :disabled="!configLoaded || busy" @click="addGroupPolicy">新增策略</button>
      </div>
      <div v-if="!groupPolicies.length" class="empty-state compact">当前没有配置群策略。</div>
      <div v-else class="ai-draft-list">
        <article v-for="(policy, index) in groupPolicies" :key="policy.group_id || index" class="ai-draft-card">
          <div class="section-head compact">
            <div>
              <span class="eyebrow">策略 {{ index + 1 }}</span>
              <h3>{{ policy.name || policy.group_id || '未命名策略' }}</h3>
            </div>
            <button class="danger-btn slim-btn" type="button" :disabled="!configLoaded || busy" @click="removeGroupPolicy(index)">删除</button>
          </div>
          <div class="config-grid ai-config-grid">
            <label class="schema-field">
              <span class="schema-label-row"><strong>群 ID</strong><small>group_id</small></span>
              <input class="text-control" type="text" :value="policy.group_id || ''" @input="updateGroupPolicy(index, 'group_id', ($event.target as HTMLInputElement).value)" />
            </label>
            <label class="schema-field">
              <span class="schema-label-row"><strong>名称</strong><small>name</small></span>
              <input class="text-control" type="text" :value="policy.name || ''" @input="updateGroupPolicy(index, 'name', ($event.target as HTMLInputElement).value)" />
            </label>
            <label class="checkbox-row">
              <input type="checkbox" :checked="!!policy.reply_enabled" @change="updateGroupPolicy(index, 'reply_enabled', ($event.target as HTMLInputElement).checked)" />
              <span>启用回复</span>
            </label>
            <label class="checkbox-row">
              <input type="checkbox" :checked="!!policy.reply_on_at" @change="updateGroupPolicy(index, 'reply_on_at', ($event.target as HTMLInputElement).checked)" />
              <span>被 @ 时回复</span>
            </label>
            <label class="checkbox-row">
              <input type="checkbox" :checked="!!policy.reply_on_bot_name" @change="updateGroupPolicy(index, 'reply_on_bot_name', ($event.target as HTMLInputElement).checked)" />
              <span>提到机器人昵称时回复</span>
            </label>
            <label class="checkbox-row">
              <input type="checkbox" :checked="!!policy.reply_on_quote" @change="updateGroupPolicy(index, 'reply_on_quote', ($event.target as HTMLInputElement).checked)" />
              <span>引用机器人消息时回复</span>
            </label>
            <label class="checkbox-row">
              <input type="checkbox" :checked="!!policy.vision_enabled" @change="updateGroupPolicy(index, 'vision_enabled', ($event.target as HTMLInputElement).checked)" />
              <span>启用视觉</span>
            </label>
            <label class="schema-field">
              <span class="schema-label-row"><strong>冷却秒数</strong><small>cooldown_seconds</small></span>
              <input class="text-control" type="number" :value="policy.cooldown_seconds || 0" @input="updateGroupPolicy(index, 'cooldown_seconds', Number(($event.target as HTMLInputElement).value) || 0)" />
            </label>
            <label class="schema-field">
              <span class="schema-label-row"><strong>上下文消息数</strong><small>max_context_messages</small></span>
              <input class="text-control" type="number" :value="policy.max_context_messages || 0" @input="updateGroupPolicy(index, 'max_context_messages', Number(($event.target as HTMLInputElement).value) || 0)" />
            </label>
            <label class="schema-field">
              <span class="schema-label-row"><strong>输出 Token</strong><small>max_output_tokens</small></span>
              <input class="text-control" type="number" :value="policy.max_output_tokens || 0" @input="updateGroupPolicy(index, 'max_output_tokens', Number(($event.target as HTMLInputElement).value) || 0)" />
            </label>
            <label class="schema-field ai-wide-field">
              <span class="schema-label-row"><strong>覆盖提示词</strong><small>prompt_override</small></span>
              <textarea class="json-editor compact-editor" :value="policy.prompt_override || ''" @input="updateGroupPolicy(index, 'prompt_override', ($event.target as HTMLTextAreaElement).value)"></textarea>
            </label>
          </div>
        </article>
      </div>
    </section>
  </div>
</template>

<style scoped>
.ai-base-stack :deep(.text-control),
.ai-model-stack :deep(.text-control) {
  border-radius: 14px;
  background: var(--surface-soft-deep);
}

.ai-base-stack :deep(.schema-field),
.ai-model-stack :deep(.schema-field) {
  padding: 0;
  border: none;
  border-radius: 0;
  background: transparent;
  gap: 8px;
}

.ai-base-stack :deep(.schema-label-row small),
.ai-model-stack :deep(.schema-label-row small) {
  font-size: 11px;
  color: var(--text-muted);
}

.ai-config-wrapper {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.ai-base-stack,
.ai-model-stack {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.ai-config-grid {
  grid-template-columns: repeat(auto-fit, minmax(280px, 1fr));
  align-items: start;
}

.ai-provider-layout {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 18px;
}

.ai-provider-card,
.ai-base-parameter-card,
.ai-vision-card {
  min-width: 0;
}

.ai-provider-head {
  align-items: flex-start;
  justify-content: space-between;
  gap: 14px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--soft-divider);
}

.ai-provider-caption {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  align-self: flex-start;
  padding: 0;
  border: none;
  background: transparent;
  color: var(--text-muted);
  font-size: 12px;
  font-weight: 600;
  letter-spacing: 0.06em;
  white-space: nowrap;
}

.ai-provider-form {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px 16px;
  align-items: start;
}

.ai-provider-form-single {
  grid-template-columns: 1fr;
}

.ai-parameter-grid {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.ai-provider-span-two,
.ai-wide-field {
  grid-column: 1 / -1;
}

.ai-toggle-list {
  display: grid;
  gap: 12px;
}

.ai-toggle-item {
  min-height: 48px;
  padding: 14px 16px;
  border: 1px solid var(--soft-border);
  border-radius: 16px;
  background: var(--surface-soft);
}

.ai-thinking-toggle {
  align-items: flex-start;
}

.ai-thinking-toggle span {
  display: grid;
  gap: 4px;
}

.field-hint,
.ai-thinking-note {
  color: var(--text-soft);
  line-height: 1.6;
}

.ai-runtime-summary {
  display: grid;
  gap: 10px;
  padding: 16px;
  border: 1px dashed var(--soft-border);
  border-radius: 16px;
  background: var(--surface-soft-alt);
}

.ai-runtime-badge {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: fit-content;
  min-height: 30px;
  padding: 0 12px;
  border-radius: 999px;
  background: var(--danger-bg-soft);
  color: var(--danger-text);
  font-size: 12px;
  font-weight: 700;
}

.ai-runtime-badge.active {
  background: var(--success-bg-soft);
  color: var(--success-text);
}

.ai-runtime-copy {
  margin: 0;
  color: var(--text-soft);
  line-height: 1.6;
}

.ai-runtime-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.ai-runtime-meta span {
  display: inline-flex;
  align-items: center;
  min-height: 28px;
  padding: 0 10px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.78);
  color: var(--text-secondary);
  font-size: 12px;
}

.ai-base-parameter-card {
  width: 100%;
}

.ai-social-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.ai-social-panel {
  display: flex;
  flex-direction: column;
  gap: 14px;
  min-width: 0;
  padding: 16px;
  border: 1px solid var(--soft-border);
  border-radius: 18px;
  background: linear-gradient(180deg, var(--surface-soft) 0%, var(--surface-soft-alt) 100%);
}

.ai-social-toggle {
  align-items: flex-start;
}

.ai-social-toggle span {
  display: grid;
  gap: 4px;
  min-width: 0;
}

.ai-social-toggle strong {
  color: var(--text-primary);
  font-size: 14px;
}

.ai-social-toggle small {
  color: var(--text-soft);
  line-height: 1.5;
}

.ai-social-form {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.ai-vision-layout {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.ai-vision-topbar {
  display: grid;
  grid-template-columns: minmax(280px, 340px) minmax(0, 1fr);
  gap: 16px;
  align-items: stretch;
}

.ai-vision-switch-panel,
.ai-vision-mode-panel {
  min-height: 100%;
  min-width: 0;
  box-sizing: border-box;
  padding: 16px;
  border: 1px solid var(--soft-border);
  border-radius: 18px;
  background: linear-gradient(180deg, var(--surface-soft) 0%, var(--surface-soft-alt) 100%);
}

.ai-vision-switch-panel {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-items: start;
  gap: 16px;
}

.ai-vision-switch-copy {
  display: grid;
  gap: 4px;
  min-width: 0;
  align-content: start;
}

.ai-vision-switch-kicker {
  font-size: 11px;
  color: var(--text-muted);
  letter-spacing: 0.06em;
}

.ai-vision-switch-copy strong {
  font-size: 15px;
  line-height: 1.35;
  color: var(--text-primary);
}

.ai-vision-switch-copy small {
  color: var(--text-soft);
  line-height: 1.5;
}

.ai-vision-switch-action {
  flex-shrink: 0;
  align-self: start;
  margin-top: 2px;
  padding: 8px 12px;
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.72);
}

.ai-vision-mode-panel {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  justify-content: flex-start;
  gap: 8px;
}

.ai-vision-provider-form {
  padding-top: 0;
}

.ai-vision-follow-note {
  margin: 0;
  padding: 12px 14px;
  border: 1px solid var(--soft-border);
  border-radius: 14px;
  background: var(--surface-soft-alt);
  color: var(--text-soft);
}

.ai-draft-list {
  display: flex;
  flex-direction: column;
  gap: 16px;
  margin-top: 12px;
}

.ai-draft-card {
  border: 1px solid var(--soft-border);
  border-radius: 16px;
  padding: 16px;
  background: var(--surface-soft);
}

.model-field-row {
  display: grid;
  grid-template-columns: 1fr;
  gap: 8px;
  align-items: start;
}

.model-fetch-btn {
  min-height: 36px;
  padding: 0 14px;
  justify-self: end;
  white-space: nowrap;
}

.compact-editor {
  min-height: 120px;
}

@media (max-width: 960px) {
  .ai-provider-layout,
  .ai-vision-topbar,
  .ai-social-grid {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 640px) {
  .ai-vision-switch-panel {
    grid-template-columns: 1fr;
  }

  .ai-vision-switch-action {
    width: 100%;
    justify-content: center;
  }

  .ai-provider-form,
  .ai-parameter-grid,
  .ai-config-grid,
  .model-field-row {
    grid-template-columns: 1fr;
  }

  .ai-provider-span-two,
  .ai-wide-field {
    grid-column: auto;
  }

  .ai-provider-caption {
    width: auto;
  }

  .model-fetch-btn {
    justify-self: stretch;
  }
}
</style>
