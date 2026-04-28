export type SchemaFieldType = 'string' | 'number' | 'integer' | 'boolean' | 'enum' | 'json'

export interface SchemaField {
  key: string
  label: string
  description: string
  type: SchemaFieldType
  required: boolean
  present: boolean
  hasDefault: boolean
  enumValues: unknown[]
  minimum?: number
  maximum?: number
  value: any
  defaultValue?: any
}

export interface ExtraConfigRow {
  id: string
  path: string
  type: 'string' | 'number' | 'boolean' | 'json'
  value: string
}

export interface BuildConfigResult {
  value?: Record<string, unknown>
  error?: string
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

export function cloneValue<T>(value: T): T {
  if (value == null) return value
  return JSON.parse(JSON.stringify(value)) as T
}

export function formatJSON(value: unknown): string {
  return JSON.stringify(value ?? {}, null, 2)
}

export function getSchemaProperties(schema: unknown): Record<string, Record<string, unknown>> {
  if (!isPlainObject(schema)) return {}
  if (schema.type && schema.type !== 'object') return {}
  return isPlainObject(schema.properties) ? schema.properties as Record<string, Record<string, unknown>> : {}
}

export function getRequiredSet(schema: unknown): Set<string> {
  if (!isPlainObject(schema) || !Array.isArray(schema.required)) return new Set()
  return new Set(schema.required.map((item) => String(item)))
}

export function supportsSchema(schema: unknown): boolean {
  return Object.keys(getSchemaProperties(schema)).length > 0
}

function aliasLabel(key: string): string {
  const aliases: Record<string, string> = {
    api_url: 'API URL',
    api_base_url: 'API Base URL',
    api_key: 'API Key',
    model: 'Model',
    request_timeout_ms: 'Request Timeout',
    search_limit: 'Search Limit',
    search_expire_seconds: 'Search Cache TTL',
    bot_name: 'Bot Name',
    parser_api_base_url: 'Parser API Base URL',
    video_max_size_mb: 'Max Video Size',
    menu_text: 'Menu Text',
    auto_generate: 'Auto Generate',
    header_text: 'Header Text',
    song_level: 'Song Quality',
    recommend_api_url: 'Recommend API URL',
    recommend_api_key: 'Recommend API Key',
    recommend_model: 'Recommend Model',
  }
  return aliases[key] || ''
}

function aliasDescription(key: string): string {
  const aliases: Record<string, string> = {
    request_timeout_ms: 'Unit: milliseconds.',
    search_expire_seconds: 'Unit: seconds.',
    video_max_size_mb: 'Unit: MB.',
  }
  return aliases[key] || ''
}

function humanizeKey(key: string): string {
  return key
    .split(/[._-]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ') || 'Config Field'
}

function fieldType(schema: Record<string, unknown>): SchemaFieldType {
  if (Array.isArray(schema.enum) && schema.enum.length > 0) return 'enum'
  const declared = String(schema.type || '').toLowerCase()
  if (declared === 'integer') return 'integer'
  if (declared === 'number') return 'number'
  if (declared === 'boolean') return 'boolean'
  if (declared === 'array' || declared === 'object') return 'json'
  return 'string'
}

function initialValue(config: Record<string, unknown>, key: string, schema: Record<string, unknown>): { present: boolean; value: unknown } {
  if (Object.prototype.hasOwnProperty.call(config, key)) {
    return { present: true, value: cloneValue(config[key]) }
  }
  if (Object.prototype.hasOwnProperty.call(schema, 'default')) {
    return { present: true, value: cloneValue(schema.default) }
  }
  return { present: false, value: fieldType(schema) === 'boolean' ? false : '' }
}

export function buildSchemaFields(config: Record<string, unknown>, schema: unknown): SchemaField[] {
  const properties = getSchemaProperties(schema)
  const required = getRequiredSet(schema)
  return Object.keys(properties)
    .sort((left, right) => left.localeCompare(right))
    .map((key) => {
      const fieldSchema = properties[key]
      const valueState = initialValue(config || {}, key, fieldSchema)
      const title = String(fieldSchema.title || '').trim()
      const description = String(fieldSchema.description || '').trim()
      const isRequired = required.has(key)
      return {
        key,
        label: title || aliasLabel(key) || humanizeKey(key),
        description: [isRequired ? 'Required' : '', description || aliasDescription(key)].filter(Boolean).join(' · '),
        type: fieldType(fieldSchema),
        required: isRequired,
        present: valueState.present,
        hasDefault: Object.prototype.hasOwnProperty.call(fieldSchema, 'default'),
        enumValues: Array.isArray(fieldSchema.enum) ? fieldSchema.enum : [],
        minimum: typeof fieldSchema.minimum === 'number' ? fieldSchema.minimum : undefined,
        maximum: typeof fieldSchema.maximum === 'number' ? fieldSchema.maximum : undefined,
        value: valueState.value,
        defaultValue: fieldSchema.default,
      }
    })
}

function detectRowType(value: unknown): ExtraConfigRow['type'] {
  if (typeof value === 'number') return 'number'
  if (typeof value === 'boolean') return 'boolean'
  if (Array.isArray(value) || isPlainObject(value)) return 'json'
  return 'string'
}

function stringifyRowValue(value: unknown): string {
  if (Array.isArray(value) || isPlainObject(value)) return formatJSON(value)
  if (value == null) return ''
  return String(value)
}

function flattenConfig(value: unknown, prefix: string, rows: ExtraConfigRow[]) {
  if (Array.isArray(value)) {
    if (!value.length && prefix) {
      rows.push({ id: crypto.randomUUID(), path: prefix, type: 'json', value: '[]' })
    }
    value.forEach((item, index) => flattenConfig(item, prefix ? `${prefix}[${index}]` : `[${index}]`, rows))
    return
  }
  if (isPlainObject(value)) {
    const keys = Object.keys(value)
    if (!keys.length && prefix) {
      rows.push({ id: crypto.randomUUID(), path: prefix, type: 'json', value: '{}' })
    }
    keys.forEach((key) => flattenConfig(value[key], prefix ? `${prefix}.${key}` : key, rows))
    return
  }
  if (!prefix) return
  rows.push({ id: crypto.randomUUID(), path: prefix, type: detectRowType(value), value: stringifyRowValue(value) })
}

export function buildExtraRows(config: Record<string, unknown>, schema: unknown): ExtraConfigRow[] {
  const properties = getSchemaProperties(schema)
  const source: Record<string, unknown> = {}
  Object.keys(config || {}).forEach((key) => {
    if (!Object.prototype.hasOwnProperty.call(properties, key)) {
      source[key] = cloneValue(config[key])
    }
  })
  const rows: ExtraConfigRow[] = []
  flattenConfig(supportsSchema(schema) ? source : config || {}, '', rows)
  return rows
}

function parsePath(pathText: string): { tokens?: Array<string | number>; error?: string } {
  const input = String(pathText || '').trim()
  if (!input) return { error: 'Config key cannot be empty.' }
  const tokens: Array<string | number> = []
  const matcher = /([^.[\]]+)|\[(\d+)\]/g
  let match: RegExpExecArray | null
  let consumed = ''
  while ((match = matcher.exec(input)) !== null) {
    consumed += match[0]
    if (match[1]) tokens.push(match[1])
    if (match[2]) tokens.push(Number(match[2]))
  }
  if (!tokens.length || consumed.length !== input.length) return { error: `Invalid config key: ${input}` }
  if (typeof tokens[0] === 'number') return { error: `Config key cannot start with array index: ${input}` }
  return { tokens }
}

function parsePrimitive(type: ExtraConfigRow['type'], rawValue: string): { value?: unknown; error?: string } {
  const text = String(rawValue ?? '').trim()
  if (type === 'number') {
    if (!text) return { error: 'Number value cannot be empty.' }
    const value = Number(text)
    if (Number.isNaN(value)) return { error: `Invalid number: ${rawValue}` }
    return { value }
  }
  if (type === 'boolean') {
    const lowered = text.toLowerCase()
    if (['true', '1', 'yes', 'on'].includes(lowered)) return { value: true }
    if (['false', '0', 'no', 'off'].includes(lowered)) return { value: false }
    return { error: 'Boolean only supports true / false.' }
  }
  if (type === 'json') {
    if (!text) return { value: {} }
    try {
      return { value: JSON.parse(text) }
    } catch (error) {
      return { error: `Invalid JSON: ${error instanceof Error ? error.message : String(error)}` }
    }
  }
  return { value: String(rawValue ?? '') }
}

function assignValue(target: Record<string, unknown>, tokens: Array<string | number>, value: unknown) {
  let cursor: any = target
  tokens.forEach((token, index) => {
    const last = index === tokens.length - 1
    const nextToken = tokens[index + 1]
    if (last) {
      cursor[token] = value
      return
    }
    if (!cursor[token] || typeof cursor[token] !== 'object') {
      cursor[token] = typeof nextToken === 'number' ? [] : {}
    }
    cursor = cursor[token]
  })
}

function normalizeSchemaValue(field: SchemaField): { omit?: boolean; value?: unknown; error?: string } {
  if (field.type === 'boolean') {
    const value = !!field.value
    return { omit: !field.required && !field.present && !field.hasDefault && value === false, value }
  }

  const rawText = String(field.value ?? '').trim()
  if (field.type === 'integer' || field.type === 'number') {
    if (!rawText) {
      if (!field.required && !field.present && !field.hasDefault) return { omit: true }
      return { error: `${field.key}: number value cannot be empty.` }
    }
    const value = Number(rawText)
    if (Number.isNaN(value)) return { error: `${field.key}: invalid number.` }
    return { value: field.type === 'integer' ? Math.trunc(value) : value }
  }

  if (field.type === 'json') {
    if (!rawText) {
      if (!field.required && !field.present && !field.hasDefault) return { omit: true }
      return { error: `${field.key}: JSON value cannot be empty.` }
    }
    try {
      return { value: JSON.parse(rawText) }
    } catch (error) {
      return { error: `${field.key}: invalid JSON: ${error instanceof Error ? error.message : String(error)}` }
    }
  }

  if (field.type === 'enum') {
    if (!rawText && !field.required && !field.present && !field.hasDefault) return { omit: true }
    return { value: rawText }
  }

  if (!rawText && !field.required && !field.present && !field.hasDefault) return { omit: true }
  return { value: String(field.value ?? '') }
}

export function buildConfigFromEditors(fields: SchemaField[], rows: ExtraConfigRow[]): BuildConfigResult {
  const result: Record<string, unknown> = {}

  for (const field of fields) {
    const normalized = normalizeSchemaValue(field)
    if (normalized.error) return { error: normalized.error }
    if (!normalized.omit) result[field.key] = normalized.value
  }

  for (const row of rows) {
    const parsedPath = parsePath(row.path)
    if (parsedPath.error || !parsedPath.tokens) return { error: parsedPath.error }
    const parsedValue = parsePrimitive(row.type, row.value)
    if (parsedValue.error) return { error: `${row.path}: ${parsedValue.error}` }
    assignValue(result, parsedPath.tokens, parsedValue.value)
  }

  return { value: result }
}
