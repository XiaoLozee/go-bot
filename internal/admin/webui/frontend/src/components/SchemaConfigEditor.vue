<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import {
  buildConfigFromEditors,
  buildExtraRows,
  buildSchemaFields,
  formatJSON,
  supportsSchema,
  type ExtraConfigRow,
  type SchemaField,
} from '../lib/schema-config'

const props = defineProps<{
  config: Record<string, unknown>
  schema?: Record<string, unknown>
  enabled: boolean
  busy: boolean
}>()

const emit = defineEmits<{
  save: [payload: { enabled: boolean; config: Record<string, unknown> }]
  reset: []
  'update:enabled': [enabled: boolean]
}>()

const fields = ref<SchemaField[]>([])
const rows = ref<ExtraConfigRow[]>([])
const fallbackText = ref('{}')
const localError = ref('')
const schemaMode = computed(() => supportsSchema(props.schema))

watch(
  () => [props.config, props.schema] as const,
  () => resetEditors(),
  { immediate: true, deep: true },
)

function resetEditors() {
  localError.value = ''
  const config = props.config || {}
  fields.value = buildSchemaFields(config, props.schema)
  rows.value = buildExtraRows(config, props.schema)
  fallbackText.value = formatJSON(config)
}

function addExtraRow() {
  rows.value.push({
    id: crypto.randomUUID(),
    path: '',
    type: 'string',
    value: '',
  })
}

function removeExtraRow(id: string) {
  rows.value = rows.value.filter((item) => item.id !== id)
}

function saveConfig() {
  localError.value = ''
  if (!schemaMode.value) {
    try {
      const parsed = JSON.parse(fallbackText.value || '{}')
      if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
        throw new Error('插件配置必须是 JSON 对象。')
      }
      emit('save', { enabled: props.enabled, config: parsed as Record<string, unknown> })
    } catch (error) {
      localError.value = error instanceof Error ? error.message : String(error)
    }
    return
  }

  const result = buildConfigFromEditors(fields.value, rows.value)
  if (result.error || !result.value) {
    localError.value = result.error || '构建插件配置失败。'
    return
  }
  emit('save', { enabled: props.enabled, config: result.value })
}

function restoreFromRuntime() {
  resetEditors()
  emit('reset')
}
</script>

<template>
  <article class="subcard editor-card">
    <div class="section-head compact">
      <div>
        <span class="eyebrow">配置</span>
        <h3>{{ schemaMode ? '结构表单' : 'JSON 编辑器' }}</h3>
      </div>
      <button class="secondary-btn" type="button" :disabled="busy" @click="restoreFromRuntime">
        Reset
      </button>
    </div>

    <label class="checkbox-row">
      <input :checked="enabled" type="checkbox" @change="emit('update:enabled', ($event.target as HTMLInputElement).checked)" />
      <span>保存后启用插件。</span>
    </label>

    <div v-if="localError" class="banner banner-danger">
      <strong>配置错误</strong>
      <span>{{ localError }}</span>
    </div>

    <template v-if="schemaMode">
      <div v-if="fields.length" class="schema-form-grid">
        <label v-for="field in fields" :key="field.key" class="schema-field">
          <span class="schema-label-row">
            <strong>{{ field.label }}</strong>
            <small>{{ field.key }}</small>
            <em v-if="field.required">必填</em>
          </span>
          <span v-if="field.description" class="schema-description">{{ field.description }}</span>

          <input v-if="field.type === 'boolean'" v-model="field.value" class="schema-checkbox" type="checkbox" />

          <select v-else-if="field.type === 'enum'" v-model="field.value" class="text-control">
            <option v-if="!field.required && !field.present && !field.hasDefault" value="">未设置</option>
            <option v-for="item in field.enumValues" :key="String(item)" :value="String(item)">
              {{ String(item) }}
            </option>
          </select>

          <input
            v-else-if="field.type === 'integer' || field.type === 'number'"
            v-model="field.value"
            class="text-control"
            type="number"
            :step="field.type === 'integer' ? '1' : 'any'"
            :min="field.minimum"
            :max="field.maximum"
            :placeholder="field.defaultValue == null ? '' : String(field.defaultValue)"
          />

          <textarea
            v-else-if="field.type === 'json'"
            v-model="field.value"
            class="json-editor compact-editor"
            spellcheck="false"
            :placeholder="field.defaultValue == null ? '' : formatJSON(field.defaultValue)"
          ></textarea>

          <input
            v-else
            v-model="field.value"
            class="text-control"
            type="text"
            :placeholder="field.defaultValue == null ? '' : String(field.defaultValue)"
          />
        </label>
      </div>

      <div class="section-head compact extra-head">
        <div>
          <span class="eyebrow">扩展</span>
          <h3>扩展配置</h3>
        </div>
        <button class="secondary-btn" type="button" :disabled="busy" @click="addExtraRow">新增字段</button>
      </div>

      <div v-if="!rows.length" class="empty-state compact">暂无扩展配置字段。</div>
      <div v-else class="extra-row-list">
        <div v-for="row in rows" :key="row.id" class="extra-row">
          <input v-model="row.path" class="text-control" type="text" placeholder="key 或 nested.path" />
          <select v-model="row.type" class="text-control small-control">
            <option value="string">字符串</option>
            <option value="number">数字</option>
            <option value="boolean">布尔值</option>
            <option value="json">JSON</option>
          </select>
          <textarea v-if="row.type === 'json'" v-model="row.value" class="json-editor mini-editor" spellcheck="false"></textarea>
          <input v-else v-model="row.value" class="text-control" type="text" placeholder="value" />
          <button class="danger-btn slim-btn" type="button" :disabled="busy" @click="removeExtraRow(row.id)">删除</button>
        </div>
      </div>
    </template>

    <template v-else>
      <textarea v-model="fallbackText" class="json-editor" spellcheck="false"></textarea>
      <p class="inline-note">该插件没有提供结构描述，当前改为 JSON 编辑方式。</p>
    </template>

    <div class="action-row compact">
      <button class="primary-btn" type="button" :disabled="busy" @click="saveConfig">
        {{ busy ? '保存中...' : '保存配置' }}
      </button>
    </div>
  </article>
</template>
