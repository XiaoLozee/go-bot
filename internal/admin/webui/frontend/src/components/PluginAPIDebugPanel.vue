<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { APIError, requestJSON } from '../lib/http'
import { buildPythonSample, debugMethodCatalog, type DebugMethodDefinition } from '../lib/plugin-api-debug'
import { formatJSON } from '../lib/schema-config'
import type { RuntimeConfig, WebUIBootstrap } from '../types/api'

const props = defineProps<{
  runtimeConfig: RuntimeConfig
  bootstrap: WebUIBootstrap | null
  busy: boolean
}>()

const emit = defineEmits<{
  busy: [busy: boolean]
  unauthorized: []
}>()

const activeMethod = ref(debugMethodCatalog[0]?.method || '')
const payloadText = ref('{}')
const responseText = ref('等待调试结果。')
const responseKind = ref<'neutral' | 'success' | 'error'>('neutral')
const summaryText = ref('选择一个 SDK 方法并执行框架级调试调用。')

const defaultConnectionID = computed(() => props.bootstrap?.connections?.[0]?.id || '')
const groupedMethods = computed(() => {
  const groups = new Map<string, DebugMethodDefinition[]>()
  debugMethodCatalog.forEach((item) => {
    const list = groups.get(item.category) || []
    list.push(item)
    groups.set(item.category, list)
  })
  return Array.from(groups.entries()).map(([category, items]) => ({ category, items }))
})
const currentDefinition = computed(() => debugMethodCatalog.find((item) => item.method === activeMethod.value) || debugMethodCatalog[0])
const payloadObject = computed(() => {
  try {
    const parsed = JSON.parse(payloadText.value || '{}')
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed as Record<string, unknown> : {}
  } catch {
    return {}
  }
})
const pythonSample = computed(() => currentDefinition.value ? buildPythonSample(currentDefinition.value, payloadObject.value) : '')
const previewResponse = computed(() => {
  if (responseKind.value !== 'neutral' && responseText.value && responseText.value !== '等待调试结果。') return responseText.value
  return formatJSON(currentDefinition.value?.exampleResponse || {})
})

watch(
  () => [activeMethod.value, defaultConnectionID.value] as const,
  () => resetPayloadForMethod(),
  { immediate: true },
)

function apiURL(path: string): string {
  return props.runtimeConfig.apiBasePath + path
}

function resetPayloadForMethod() {
  if (!currentDefinition.value) return
  payloadText.value = formatJSON(currentDefinition.value.template(defaultConnectionID.value))
  responseText.value = '等待调试结果。'
  responseKind.value = 'neutral'
  summaryText.value = `准备调用 ${currentDefinition.value.method}。`
}

function selectMethod(method: string) {
  activeMethod.value = method
}

function formatError(error: unknown): string {
  if (error instanceof APIError) {
    if (error.status === 401) emit('unauthorized')
    return error.message
  }
  if (error instanceof Error) return error.message
  return String(error)
}

async function executeDebugCall() {
  if (!currentDefinition.value || props.busy) return
  let payload: Record<string, unknown>
  try {
    const parsed = JSON.parse(payloadText.value || '{}')
    if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
      throw new Error('请求参数必须是 JSON 对象。')
    }
    payload = parsed as Record<string, unknown>
  } catch (error) {
    responseKind.value = 'error'
    responseText.value = formatError(error)
    summaryText.value = '请求参数 JSON 解析失败。'
    return
  }

  emit('busy', true)
  responseKind.value = 'neutral'
  responseText.value = '请求执行中...'
  summaryText.value = `正在调用 ${currentDefinition.value.method}...`
  try {
    const result = await requestJSON<Record<string, unknown>>(apiURL('/plugin-api/debug'), {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ method: currentDefinition.value.method, payload }),
    })
    payloadText.value = formatJSON(payload)
    responseText.value = formatJSON(result)
    responseKind.value = result.error ? 'error' : 'success'
    summaryText.value = String(result.error || result.message || '调试调用已完成。')
  } catch (error) {
    responseKind.value = 'error'
    responseText.value = formatError(error)
    summaryText.value = '调试调用失败。'
  } finally {
    emit('busy', false)
  }
}
</script>

<template>
  <section class="card api-debug-card">
    <div class="section-head">
      <div>
        <span class="eyebrow">框架 SDK</span>
        <h2>插件 API 调试</h2>
      </div>
      <span class="muted">默认连接：{{ defaultConnectionID || '无' }}</span>
    </div>

    <div class="api-debug-layout">
      <aside class="api-debug-nav">
        <section v-for="group in groupedMethods" :key="group.category" class="api-debug-group">
          <h3>{{ group.category }}</h3>
          <button
            v-for="method in group.items"
            :key="method.method"
            class="api-debug-method"
            :class="{ active: activeMethod === method.method }"
            type="button"
            @click="selectMethod(method.method)"
          >
            <strong>{{ method.label }}</strong>
            <span>{{ method.method }}</span>
          </button>
        </section>
      </aside>

      <main v-if="currentDefinition" class="api-debug-main">
        <section class="subcard">
          <span class="eyebrow">当前方法</span>
          <h3>{{ currentDefinition.label }}</h3>
          <p class="inline-note">{{ currentDefinition.hint }} {{ currentDefinition.summary }}</p>
          <div class="action-row compact">
            <button class="primary-btn" type="button" :disabled="busy" @click="executeDebugCall">
              {{ busy ? '执行中...' : '执行调试调用' }}
            </button>
            <button class="secondary-btn" type="button" :disabled="busy" @click="resetPayloadForMethod">重置参数</button>
          </div>
        </section>

        <section class="subcard">
          <h3>请求参数</h3>
          <div class="param-list">
            <div v-for="param in currentDefinition.params" :key="param.key" class="param-item">
              <strong>{{ param.label }}</strong>
              <code>{{ param.key }}</code>
              <span>{{ param.type }} · {{ param.required ? '必填' : '可选' }}</span>
              <p>{{ param.description }}</p>
            </div>
          </div>
          <textarea v-model="payloadText" class="json-editor debug-editor" spellcheck="false"></textarea>
        </section>

        <section class="subcard">
          <h3>Python 示例</h3>
          <pre class="code-block dark-code"><code>{{ pythonSample }}</code></pre>
        </section>

        <section class="subcard">
          <div class="banner" :class="`banner-${responseKind === 'neutral' ? 'info' : responseKind}`">
            <strong>{{ summaryText }}</strong>
          </div>
          <h3>响应结果</h3>
          <pre class="code-block dark-code"><code>{{ previewResponse }}</code></pre>
        </section>
      </main>
    </div>
  </section>
</template>
