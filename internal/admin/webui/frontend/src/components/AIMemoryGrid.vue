<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { APIError, requestJSON } from '../lib/http'
import type { AIRelationAnalysisResult, AIRelationAnalysisTaskView } from '../types/api'

type MemoryRecord = Record<string, unknown>

const props = defineProps<{
  debugSessions: MemoryRecord[]
  candidateMemories: MemoryRecord[]
  longTermMemories: MemoryRecord[]
  groupProfiles: MemoryRecord[]
  groupObservations: MemoryRecord[]
  userProfiles: MemoryRecord[]
  relationEdges: MemoryRecord[]
  reflectionStats?: MemoryRecord
  lastReflectionAt?: string
  lastReflectionError?: string
  apiBasePath: string
  busy: boolean
  memoryActionBusy: string
  reflectionBusy: boolean
}>()

const emit = defineEmits<{
  runReflection: []
  runCandidateAction: [id: string, action: 'promote' | 'delete']
  deleteLongTermMemory: [id: string]
}>()

const selectedGroupID = ref('all')
const selectedRelationNodeID = ref('')
const selectedRelationType = ref('all')
const personalityReportStatus = ref('')
const relationAnalysisBusy = ref(false)
const relationAnalysisTaskID = ref('')
const relationAnalysisTaskStatus = ref('')
const llmPersonalityReportMarkdown = ref('')
const llmPersonalityReportMeta = ref('')
let relationAnalysisPollTimer = 0
let relationAnalysisRequestSerial = 0

const groupOptions = computed(() => {
  const ids = new Set<string>()
  for (const collection of [
    props.debugSessions,
    props.candidateMemories,
    props.longTermMemories,
    props.groupProfiles,
    props.groupObservations,
    props.userProfiles,
    props.relationEdges,
  ]) {
    for (const item of collection) {
      const groupID = groupIDFromRecord(item)
      if (groupID) ids.add(groupID)
    }
  }
  return ['all', ...Array.from(ids).sort((a, b) => a.localeCompare(b, 'zh-Hans-CN'))]
})

watch(groupOptions, (options) => {
  if (!options.includes(selectedGroupID.value)) selectedGroupID.value = options[0] || 'all'
})

watch(selectedGroupID, () => {
  clearRelationAnalysisPolling()
  relationAnalysisBusy.value = false
  relationAnalysisTaskID.value = ''
  relationAnalysisTaskStatus.value = ''
  selectedRelationNodeID.value = ''
  selectedRelationType.value = 'all'
  llmPersonalityReportMarkdown.value = ''
  llmPersonalityReportMeta.value = ''
  personalityReportStatus.value = ''
})

onBeforeUnmount(() => {
  clearRelationAnalysisPolling()
})

const selectedGroupLabel = computed(() => groupDisplayLabel(selectedGroupID.value))
const filteredDebugSessions = computed(() => groupScoped(props.debugSessions))
const filteredCandidateMemories = computed(() => groupScoped(props.candidateMemories))
const filteredLongTermMemories = computed(() => groupScoped(props.longTermMemories))
const filteredGroupProfiles = computed(() => groupScoped(props.groupProfiles))
const filteredGroupObservations = computed(() => groupScoped(props.groupObservations))
const filteredUserProfiles = computed(() => groupScoped(props.userProfiles))
const filteredRelationEdges = computed(() => groupScoped(props.relationEdges))
const relationAnalysisDisabled = computed(() => relationAnalysisBusy.value || props.busy || (!filteredUserProfiles.value.length && !filteredRelationEdges.value.length))
const relationAnalysisPrimaryLabel = computed(() => {
  if (relationAnalysisBusy.value) return '后台分析中…'
  return llmPersonalityReportMarkdown.value ? '使用缓存分析' : '调用 LLM 分析'
})
const relationAnalysisTaskMeta = computed(() => {
  if (!relationAnalysisTaskID.value) return ''
  return `任务 ${relationAnalysisTaskID.value} · ${relationAnalysisStatusLabel(relationAnalysisTaskStatus.value)}`
})
const visibleRelationEdges = computed(() => {
  if (selectedRelationType.value === 'all') return filteredRelationEdges.value
  return filteredRelationEdges.value.filter((item) => stringField(item, 'relation_type') === selectedRelationType.value)
})
const relationTypeOptions = computed(() => {
  const counts = new Map<string, number>()
  for (const item of filteredRelationEdges.value) {
    const type = stringField(item, 'relation_type') || 'unknown'
    counts.set(type, (counts.get(type) || 0) + 1)
  }
  const preferredOrder = ['conversation', 'mention', 'reply', 'co_topic', 'shared_preference', 'banter']
  const types = Array.from(counts.keys()).sort((a, b) => {
    const left = preferredOrder.indexOf(a)
    const right = preferredOrder.indexOf(b)
    if (left !== -1 || right !== -1) {
      if (left === -1) return 1
      if (right === -1) return -1
      return left - right
    }
    return relationTypeLabel(a).localeCompare(relationTypeLabel(b), 'zh-Hans-CN')
  })
  return [
    { value: 'all', label: '全部关系', count: filteredRelationEdges.value.length },
    ...types.map((type) => ({ value: type, label: relationTypeLabel(type), count: counts.get(type) || 0 })),
  ]
})
const relationTypeBreakdown = computed(() => relationTypeOptions.value.filter((item) => item.value !== 'all' && item.count > 0))
const strongestRelationEdges = computed(() =>
  [...visibleRelationEdges.value]
    .sort((a, b) => {
      const strengthGap = normalizedRatio(b.strength) - normalizedRatio(a.strength)
      if (strengthGap !== 0) return strengthGap
      return numberField(b, 'evidence_count') - numberField(a, 'evidence_count')
    })
    .slice(0, 5),
)
const personalityReportMarkdown = computed(() => buildPersonalityReportMarkdown())
const effectivePersonalityReportMarkdown = computed(() => llmPersonalityReportMarkdown.value || personalityReportMarkdown.value)

watch(relationTypeOptions, (options) => {
  if (!options.some((option) => option.value === selectedRelationType.value)) {
    selectedRelationType.value = 'all'
  }
})

watch(selectedRelationType, () => {
  selectedRelationNodeID.value = ''
})

const strategyCards = computed(() => [
  {
    key: 'sessions',
    label: '会话上下文',
    value: filteredDebugSessions.value.length,
    hint: '短期上下文入口',
    tone: 'blue',
  },
  {
    key: 'candidates',
    label: '候选记忆',
    value: filteredCandidateMemories.value.length,
    hint: '等待筛选与提升',
    tone: 'amber',
  },
  {
    key: 'long-term',
    label: '长期记忆',
    value: filteredLongTermMemories.value.length,
    hint: '已沉淀的稳定信息',
    tone: 'green',
  },
  {
    key: 'profiles',
    label: '群画像',
    value: filteredGroupProfiles.value.length,
    hint: '群氛围与策略偏好',
    tone: 'violet',
  },
  {
    key: 'relations',
    label: '关系边',
    value: filteredRelationEdges.value.length,
    hint: '用户与主题关系',
    tone: 'rose',
  },
])

const reflectionStatCards = computed(() => [
  {
    key: 'promoted_count',
    label: '沉淀',
    value: statNumber('promoted_count'),
    hint: '候选转长期',
  },
  {
    key: 'adjusted_candidate_count',
    label: '校准',
    value: statNumber('adjusted_candidate_count'),
    hint: '可信度调整',
  },
  {
    key: 'conflict_candidate_count',
    label: '冲突',
    value: statNumber('conflict_candidate_count'),
    hint: '待确认记忆',
  },
  {
    key: 'deleted_candidate_count',
    label: '清理候选',
    value: statNumber('deleted_candidate_count'),
    hint: '过期或低质',
  },
  {
    key: 'deleted_long_term_count',
    label: '清理长期',
    value: statNumber('deleted_long_term_count'),
    hint: '长期记忆淘汰',
  },
  {
    key: 'updated_group_count',
    label: '更新群画像',
    value: statNumber('updated_group_count'),
    hint: '画像刷新',
  },
])

const hasReflectionStats = computed(() => reflectionStatCards.value.some((item) => item.value > 0))
const lastReflectionLabel = computed(() => formatDateTime(props.lastReflectionAt))
const reflectionError = computed(() => String(props.lastReflectionError || ''))

function statNumber(key: string): number {
  return numberField(props.reflectionStats, key)
}

function stringField(value: MemoryRecord | undefined, key: string): string {
  if (!value) return ''
  const raw = value[key]
  return raw == null ? '' : String(raw)
}

function numberField(value: MemoryRecord | undefined, key: string): number {
  if (!value) return 0
  const raw = Number(value[key])
  return Number.isFinite(raw) ? raw : 0
}

function arrayField(value: MemoryRecord | undefined, key: string): string[] {
  if (!value) return []
  const raw = value[key]
  return Array.isArray(raw) ? raw.map((item) => String(item)).filter(Boolean) : []
}

function memoryPreview(value: MemoryRecord): string {
  return (
    stringField(value, 'content') ||
    stringField(value, 'reflection_summary') ||
    stringField(value, 'topic_summary') ||
    stringField(value, 'summary') ||
    '暂无可展示的摘要。'
  )
}

function normalizedRatio(value: unknown): number {
  const number = Number(value)
  if (!Number.isFinite(number)) return 0
  return Math.min(1, Math.max(0, number))
}

function progressStyle(value: unknown): Record<string, string> {
  return { width: `${Math.round(normalizedRatio(value) * 100)}%` }
}

function formatPercent(value: unknown): string {
  const number = Number(value)
  if (!Number.isFinite(number)) return '-'
  return `${Math.round(Math.min(1, Math.max(0, number)) * 100)}%`
}

function formatDateTime(value?: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function memoryTypeLabel(value: string): string {
  const labels: Record<string, string> = {
    fact: '事实',
    preference: '偏好',
    profile: '画像',
    relationship: '关系',
    rule: '规则',
    topic: '话题',
  }
  return labels[value] || value || '记忆'
}

function scopeLabel(value: string): string {
  const labels: Record<string, string> = {
    global: '全局',
    group: '群聊',
    user: '用户',
    private: '私聊',
  }
  return labels[value] || value || '未分域'
}

function subjectLabel(value: MemoryRecord): string {
  return stringField(value, 'group_id') || stringField(value, 'subject_id') || stringField(value, 'id') || '-'
}

function confidenceClass(value: unknown): string {
  const ratio = normalizedRatio(value)
  if (ratio >= 0.75) return 'is-strong'
  if (ratio >= 0.45) return 'is-medium'
  return 'is-weak'
}

function tagLimit(value: MemoryRecord, key: string, limit = 8): string[] {
  return arrayField(value, key).slice(0, limit)
}

function hasAnyTags(value: MemoryRecord, keys: string[]): boolean {
  return keys.some((key) => arrayField(value, key).length > 0)
}

function groupMetricRows(value: MemoryRecord) {
  return [
    { key: 'humor_density', label: '幽默密度', value: normalizedRatio(value.humor_density) },
    { key: 'emoji_rate', label: '表情倾向', value: normalizedRatio(value.emoji_rate) },
    { key: 'formality', label: '正式程度', value: normalizedRatio(value.formality) },
  ]
}

function sessionTitle(value: MemoryRecord): string {
  const scope = scopeLabel(stringField(value, 'scope'))
  const groupID = stringField(value, 'group_id')
  return groupID ? `${scope} · ${groupID}` : scope
}

function relationTitle(value: MemoryRecord): string {
  return relationTypeLabel(stringField(value, 'relation_type'))
}

function groupIDFromRecord(value: MemoryRecord): string {
  const direct = stringField(value, 'group_id')
  if (direct) return direct
  const scope = stringField(value, 'scope')
  const match = scope.match(/(?:^|:)group:?(\d+)$/i) || scope.match(/^group:(\d+)$/i)
  return match?.[1] || ''
}

function groupDisplayLabel(groupID: string): string {
  if (groupID === 'all') return '全部群聊'
  return `群聊(${groupID})`
}

function groupScoped(items: MemoryRecord[]): MemoryRecord[] {
  if (selectedGroupID.value === 'all') return items
  return items.filter((item) => groupIDFromRecord(item) === selectedGroupID.value)
}

function numericID(value: string): string {
  return /^\d+$/.test(value) ? value : ''
}

function buildQQAvatarURL(value: string): string {
  const userID = numericID(value.trim())
  if (!userID) return ''
  return `https://q1.qlogo.cn/g?b=qq&nk=${encodeURIComponent(userID)}&s=140`
}

function relationTypeLabel(value: string): string {
  const labels: Record<string, string> = {
    all: '全部关系',
    mention: '提到',
    conversation: '常聊',
    reply: '回复',
    co_topic: '共同话题',
    shared_preference: '共同偏好',
    banter: '玩笑互动',
    topic: '话题',
    preference: '偏好',
  }
  return labels[value] || value || '关联'
}

function relationTypeDescription(value: string): string {
  const descriptions: Record<string, string> = {
    all: '展示当前群聊范围内的全部关系边。',
    mention: '消息中直接 @ 或明确提到对方形成的关系。',
    conversation: '短时间窗口内连续参与同一段对话形成的关系。',
    reply: '通过回复某条历史消息形成的显式互动关系。',
    co_topic: '近期消息中出现相同话题信号形成的关系。',
    shared_preference: '用户画像中的稳定偏好重叠形成的关系。',
    banter: '双方在近窗口内都有玩梗、调侃或笑点信号形成的关系。',
    topic: '围绕同一话题形成的关系。',
    preference: '围绕相似偏好形成的关系。',
  }
  return descriptions[value] || '自定义关系类型。'
}

function relationTypeColor(value: string): string {
  const colors: Record<string, string> = {
    all: '#64748b',
    conversation: '#2563eb',
    mention: '#8b5cf6',
    reply: '#f97316',
    co_topic: '#14b8a6',
    shared_preference: '#22c55e',
    banter: '#ec4899',
    topic: '#0ea5e9',
    preference: '#16a34a',
  }
  return colors[value] || '#64748b'
}

function relationEvidenceHint(item: MemoryRecord): string {
  return relationTypeDescription(stringField(item, 'relation_type'))
}

function relationNodeLabel(nodeID: string, groupID = ''): string {
  const id = nodeID.trim()
  if (!id) return '-'
  const profile = filteredUserProfiles.value.find((item) => {
    const sameUser = stringField(item, 'user_id') === id
    const sameGroup = !groupID || stringField(item, 'group_id') === groupID
    return sameUser && sameGroup
  })
  const name = profile ? stringField(profile, 'display_name') : ''
  return name && name !== id ? `${name}(${id})` : id
}

function relationNodeShortLabel(label: string): string {
  const name = label.replace(/\(\d+\)$/, '').trim()
  if (name.length <= 6) return name
  return `${name.slice(0, 6)}…`
}

function relationNodeInitial(label: string): string {
  const text = label.replace(/\(\d+\)$/, '').trim()
  return text ? text.slice(0, 1).toUpperCase() : '?'
}

function relationNodeStyle(node: { id: string; x: number; y: number; size: number }): Record<string, string> {
  const selected = selectedRelationNodeID.value === node.id
  const position = relationNodeDisplayPosition(node)
  const size = selected ? Math.max(88, node.size + 18) : node.size
  return {
    left: `${position.x}%`,
    top: `${position.y}%`,
    width: `${size}px`,
    height: `${size}px`,
  }
}

const relationGraph = computed(() => {
  const sourceEdges = visibleRelationEdges.value.filter((item) => {
    const left = stringField(item, 'node_a')
    const right = stringField(item, 'node_b')
    return left && right
  })
  const nodeWeights = new Map<string, number>()
  const nodeGroups = new Map<string, string>()
  for (const item of sourceEdges) {
    const groupID = stringField(item, 'group_id')
    const strength = Math.max(0.08, normalizedRatio(item.strength))
    for (const node of [stringField(item, 'node_a'), stringField(item, 'node_b')]) {
      if (!node) continue
      nodeWeights.set(node, (nodeWeights.get(node) || 0) + strength + numberField(item, 'evidence_count') * 0.03)
      if (groupID && !nodeGroups.has(node)) nodeGroups.set(node, groupID)
    }
  }
  const ids = Array.from(nodeWeights.keys()).sort((a, b) => (nodeWeights.get(b) || 0) - (nodeWeights.get(a) || 0))
  const count = Math.max(ids.length, 1)
  const nodes = ids.map((id, index) => {
    const orbitIndex = Math.max(0, index - 1)
    const orbitCount = Math.max(1, count - 1)
    const angle = -Math.PI / 2 + (Math.PI * 2 * orbitIndex) / orbitCount
    const radiusX = index === 0 ? 0 : index <= 8 ? 34 : 42
    const radiusY = index === 0 ? 0 : index <= 8 ? 28 : 34
    const weight = nodeWeights.get(id) || 0
    const label = relationNodeLabel(id, nodeGroups.get(id) || '')
    return {
      id,
      label,
      shortLabel: relationNodeShortLabel(label),
      initial: relationNodeInitial(label),
      avatarURL: buildQQAvatarURL(id),
      x: 50 + Math.cos(angle) * radiusX,
      y: 50 + Math.sin(angle) * radiusY,
      size: Math.min(76, 46 + weight * 8),
    }
  })
  const nodeMap = new Map(nodes.map((node) => [node.id, node]))
  const edges = sourceEdges
    .map((item, index) => {
      const source = nodeMap.get(stringField(item, 'node_a'))
      const target = nodeMap.get(stringField(item, 'node_b'))
      if (!source || !target) return null
      const strength = normalizedRatio(item.strength)
      return {
        id: stringField(item, 'id') || `edge-${index}`,
        source,
        target,
        type: stringField(item, 'relation_type'),
        label: relationTitle(item),
        strength,
        evidence: numberField(item, 'evidence_count'),
      }
    })
    .filter((item): item is NonNullable<typeof item> => !!item)
  return { nodes, edges }
})

const focusedRelationNodePositions = computed(() => {
  const positions = new Map<string, { x: number; y: number }>()
  const selectedID = selectedRelationNodeID.value
  if (!selectedID) return positions

  positions.set(selectedID, { x: 50, y: 50 })

  const connectedNodes = relationGraph.value.nodes.filter((node) => node.id !== selectedID && relationNodeIsConnected(node.id))
  const count = connectedNodes.length
  if (!count) return positions

  const radiusX = count <= 6 ? 34 : 40
  const radiusY = count <= 6 ? 28 : 33
  connectedNodes.forEach((node, index) => {
    const angle = -Math.PI / 2 + (Math.PI * 2 * index) / count
    positions.set(node.id, {
      x: 50 + Math.cos(angle) * radiusX,
      y: 50 + Math.sin(angle) * radiusY,
    })
  })

  return positions
})

function relationNodeDisplayPosition(node: { id: string; x: number; y: number }): { x: number; y: number } {
  return focusedRelationNodePositions.value.get(node.id) || { x: node.x, y: node.y }
}

const relationGraphDisplayEdges = computed(() => {
  const nodeID = selectedRelationNodeID.value
  return relationGraph.value.edges.map((edge) => {
    const sourcePosition = relationNodeDisplayPosition(edge.source)
    const targetPosition = relationNodeDisplayPosition(edge.target)
    const sourceSelected = !!nodeID && edge.source.id === nodeID
    const targetSelected = !!nodeID && edge.target.id === nodeID
    if (sourceSelected || targetSelected) {
      const outerPosition = sourceSelected ? targetPosition : sourcePosition
      return {
        ...edge,
        x1: 50,
        y1: 50,
        x2: outerPosition.x,
        y2: outerPosition.y,
      }
    }
    return {
      ...edge,
      x1: sourcePosition.x,
      y1: sourcePosition.y,
      x2: targetPosition.x,
      y2: targetPosition.y,
    }
  })
})

function relationGraphEdgeStyle(edge: { strength: number; type: string }): Record<string, string> {
  return {
    '--ray-delay': `${Math.round(edge.strength * 120)}ms`,
    '--relation-color': relationTypeColor(edge.type),
  }
}

watch(relationGraph, (graph) => {
  if (selectedRelationNodeID.value && !graph.nodes.some((node) => node.id === selectedRelationNodeID.value)) {
    selectedRelationNodeID.value = ''
  }
})

const selectedRelationNodeLabel = computed(() => {
  const nodeID = selectedRelationNodeID.value
  if (!nodeID) return ''
  const node = relationGraph.value.nodes.find((item) => item.id === nodeID)
  return node?.label || nodeID
})

const focusedRelationEdges = computed(() => {
  const nodeID = selectedRelationNodeID.value
  if (!nodeID) return visibleRelationEdges.value
  return visibleRelationEdges.value.filter((item) => relationRecordTouchesNode(item, nodeID))
})

function toggleRelationNode(nodeID: string) {
  selectedRelationNodeID.value = selectedRelationNodeID.value === nodeID ? '' : nodeID
}

function clearRelationFocus() {
  selectedRelationNodeID.value = ''
}

function relationRecordTouchesNode(item: MemoryRecord, nodeID: string): boolean {
  return stringField(item, 'node_a') === nodeID || stringField(item, 'node_b') === nodeID
}

function relationGraphEdgeClass(edge: { source: { id: string }; target: { id: string } }): Record<string, boolean> {
  const nodeID = selectedRelationNodeID.value
  const touches = !!nodeID && (edge.source.id === nodeID || edge.target.id === nodeID)
  return {
    'is-focused': touches,
    'is-radial': touches,
    'is-muted': !!nodeID && !touches,
  }
}

function relationRecordClass(item: MemoryRecord): Record<string, boolean> {
  const nodeID = selectedRelationNodeID.value
  const touches = !!nodeID && relationRecordTouchesNode(item, nodeID)
  return {
    'is-focused': touches,
    'is-muted': !!nodeID && !touches,
  }
}

function relationRecordStyle(item: MemoryRecord): Record<string, string> {
  return {
    '--relation-color': relationTypeColor(stringField(item, 'relation_type')),
  }
}

function userProfileRelations(userID: string, groupID: string): MemoryRecord[] {
  return filteredRelationEdges.value.filter((item) => {
    const sameGroup = !groupID || stringField(item, 'group_id') === groupID
    return sameGroup && relationRecordTouchesNode(item, userID)
  })
}

function userPersonalitySummary(profile: MemoryRecord): string {
  const styleTags = arrayField(profile, 'style_tags')
  const preferences = arrayField(profile, 'topic_preferences')
  const teasing = normalizedRatio(profile.teasing_tolerance)
  const trust = normalizedRatio(profile.trust_score)
  const interactionLevel = numberField(profile, 'interaction_level_with_bot')
  const parts: string[] = []
  if (styleTags.length) parts.push(`聊天风格偏 ${styleTags.slice(0, 3).join('、')}`)
  if (preferences.length) parts.push(`稳定兴趣集中在 ${preferences.slice(0, 4).join('、')}`)
  if (teasing >= 0.68) parts.push('对玩笑和轻度调侃接受度较高')
  else if (teasing <= 0.32) parts.push('更适合克制、直接的互动方式')
  if (trust >= 0.72) parts.push('与机器人互动信任度较高')
  else if (interactionLevel > 0) parts.push(`与机器人已有 ${interactionLevel} 级互动熟悉度`)
  return parts.join('；') || '画像证据仍偏少，建议继续观察其发言风格、偏好与互动对象。'
}

function userRelationSummary(profile: MemoryRecord): string {
  const userID = stringField(profile, 'user_id')
  const groupID = stringField(profile, 'group_id')
  const relations = userProfileRelations(userID, groupID)
  if (!relations.length) return '暂无稳定关系边。'
  return relations
    .slice()
    .sort((a, b) => normalizedRatio(b.strength) - normalizedRatio(a.strength))
    .slice(0, 4)
    .map((item) => {
      const left = stringField(item, 'node_a')
      const right = stringField(item, 'node_b')
      const peerID = left === userID ? right : left
      return `${relationNodeLabel(peerID, groupID)}：${relationTitle(item)} ${formatPercent(item.strength)}`
    })
    .join('；')
}

function buildPersonalityReportMarkdown(): string {
  const title = selectedGroupID.value === 'all' ? 'AI Memory Personality Report' : `AI Memory Personality Report - Group ${selectedGroupID.value}`
  const lines: string[] = [
    `# ${title}`,
    '',
    `- Scope: ${selectedGroupLabel.value}`,
    `- Generated At: ${new Date().toLocaleString()}`,
    `- User Profiles: ${filteredUserProfiles.value.length}`,
    `- Relation Edges: ${filteredRelationEdges.value.length}`,
    '',
    '## Relation Overview',
    '',
  ]
  if (relationTypeBreakdown.value.length) {
    for (const item of relationTypeBreakdown.value) {
      lines.push(`- ${item.label}: ${item.count}`)
    }
  } else {
    lines.push('- No relation edges yet.')
  }
  lines.push('', '## Member Personality Notes', '')
  if (!filteredUserProfiles.value.length) {
    lines.push('No user profiles yet.')
  } else {
    const profiles = [...filteredUserProfiles.value].sort((a, b) => {
      const leftGroup = stringField(a, 'group_id')
      const rightGroup = stringField(b, 'group_id')
      if (leftGroup !== rightGroup) return leftGroup.localeCompare(rightGroup)
      return relationNodeLabel(stringField(a, 'user_id'), leftGroup).localeCompare(relationNodeLabel(stringField(b, 'user_id'), rightGroup), 'zh-Hans-CN')
    })
    for (const profile of profiles) {
      const groupID = stringField(profile, 'group_id')
      const userID = stringField(profile, 'user_id')
      const displayName = relationNodeLabel(userID, groupID)
      const preferences = arrayField(profile, 'topic_preferences')
      const styleTags = arrayField(profile, 'style_tags')
      const taboos = arrayField(profile, 'taboo_topics')
      lines.push(`### ${displayName}`)
      lines.push('')
      lines.push(`- Group: ${groupID || '-'}`)
      lines.push(`- Personality: ${userPersonalitySummary(profile)}`)
      lines.push(`- Interaction Relations: ${userRelationSummary(profile)}`)
      lines.push(`- Topic Preferences: ${preferences.length ? preferences.join(', ') : '-'}`)
      lines.push(`- Style Tags: ${styleTags.length ? styleTags.join(', ') : '-'}`)
      lines.push(`- Taboo Topics: ${taboos.length ? taboos.join(', ') : '-'}`)
      lines.push(`- Trust Score: ${formatPercent(profile.trust_score)}`)
      lines.push(`- Teasing Tolerance: ${formatPercent(profile.teasing_tolerance)}`)
      lines.push('')
    }
  }
  return `${lines.join('\n').trim()}\n`
}

function personalityReportFileName(): string {
  const scope = selectedGroupID.value === 'all' ? 'all-groups' : `group-${selectedGroupID.value}`
  return `ai-memory-personality-${scope}.md`
}

async function copyPersonalityReport() {
  personalityReportStatus.value = ''
  try {
    await navigator.clipboard.writeText(effectivePersonalityReportMarkdown.value)
    personalityReportStatus.value = '已复制 Markdown。'
  } catch {
    personalityReportStatus.value = '复制失败，请使用下载。'
  }
}

function downloadPersonalityReport() {
  personalityReportStatus.value = ''
  const blob = new Blob([effectivePersonalityReportMarkdown.value], { type: 'text/markdown;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = personalityReportFileName()
  document.body.appendChild(anchor)
  anchor.click()
  anchor.remove()
  URL.revokeObjectURL(url)
  personalityReportStatus.value = '已生成 Markdown 文件。'
}

function clearRelationAnalysisPolling() {
  if (relationAnalysisPollTimer) {
    window.clearTimeout(relationAnalysisPollTimer)
    relationAnalysisPollTimer = 0
  }
}

function relationAnalysisStatusLabel(status: string): string {
  switch (status) {
    case 'queued':
      return '排队中'
    case 'running':
      return '执行中'
    case 'succeeded':
      return '已完成'
    case 'failed':
      return '失败'
    case 'canceled':
      return '已取消'
    default:
      return '处理中'
  }
}

function applyRelationAnalysisResult(result: AIRelationAnalysisResult) {
  llmPersonalityReportMarkdown.value = String(result.markdown || '').trim() ? `${String(result.markdown).trim()}\n` : ''
  const cacheLabel = result.cache_hit ? '缓存命中' : '新生成'
  const expiresLabel = result.expires_at ? ` · 过期 ${formatDateTime(result.expires_at)}` : ''
  llmPersonalityReportMeta.value = `LLM 分析 · ${cacheLabel} · 用户 ${result.user_count || 0} · 关系 ${result.edge_count || 0} · 记忆 ${result.memory_count || 0}${expiresLabel}`
  personalityReportStatus.value = result.message || 'AI 关系分析已生成。'
}

function scheduleRelationAnalysisPoll(taskID: string, requestSerial: number) {
  clearRelationAnalysisPolling()
  relationAnalysisPollTimer = window.setTimeout(() => {
    void pollRelationAnalysisTask(taskID, requestSerial)
  }, 1400)
}

function handleRelationAnalysisTask(task: AIRelationAnalysisTaskView, requestSerial: number) {
  if (requestSerial !== relationAnalysisRequestSerial) return
  relationAnalysisTaskID.value = String(task.task_id || '')
  relationAnalysisTaskStatus.value = String(task.status || '')
  const status = relationAnalysisTaskStatus.value

  if (status === 'queued' || status === 'running') {
    relationAnalysisBusy.value = true
    personalityReportStatus.value = task.message || (status === 'queued' ? 'AI 关系分析任务排队中…' : 'AI 正在后台分析…')
    if (relationAnalysisTaskID.value) {
      scheduleRelationAnalysisPoll(relationAnalysisTaskID.value, requestSerial)
    }
    return
  }

  clearRelationAnalysisPolling()
  relationAnalysisBusy.value = false
  if (status === 'succeeded' && task.result) {
    applyRelationAnalysisResult(task.result)
    return
  }
  personalityReportStatus.value = String(task.error || task.message || 'AI 关系分析失败。')
}

async function pollRelationAnalysisTask(taskID: string, requestSerial: number) {
  try {
    const task = await requestJSON<AIRelationAnalysisTaskView>(`${props.apiBasePath}/ai/relations/analyze/${encodeURIComponent(taskID)}`)
    handleRelationAnalysisTask(task, requestSerial)
  } catch (err) {
    if (requestSerial !== relationAnalysisRequestSerial) return
    clearRelationAnalysisPolling()
    relationAnalysisBusy.value = false
    relationAnalysisTaskStatus.value = 'failed'
    personalityReportStatus.value = err instanceof APIError ? err.message : '查询 AI 关系分析任务状态失败。'
  }
}

async function runRelationAnalysis(force = false) {
  if (relationAnalysisDisabled.value) return
  relationAnalysisRequestSerial += 1
  const requestSerial = relationAnalysisRequestSerial
  clearRelationAnalysisPolling()
  relationAnalysisBusy.value = true
  relationAnalysisTaskID.value = ''
  relationAnalysisTaskStatus.value = 'queued'
  personalityReportStatus.value = ''
  try {
    const task = await requestJSON<AIRelationAnalysisTaskView>(`${props.apiBasePath}/ai/relations/analyze`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        group_id: selectedGroupID.value === 'all' ? '' : selectedGroupID.value,
        force,
      }),
    })
    handleRelationAnalysisTask(task, requestSerial)
  } catch (err) {
    if (requestSerial !== relationAnalysisRequestSerial) return
    clearRelationAnalysisPolling()
    relationAnalysisTaskID.value = ''
    relationAnalysisTaskStatus.value = 'failed'
    relationAnalysisBusy.value = false
    personalityReportStatus.value = err instanceof APIError ? err.message : 'AI 关系分析失败。'
  }
}

function relationNodeIsConnected(nodeID: string): boolean {
  const selectedID = selectedRelationNodeID.value
  return !!selectedID && relationGraph.value.edges.some((edge) => {
    const left = edge.source.id
    const right = edge.target.id
    return (left === selectedID && right === nodeID) || (right === selectedID && left === nodeID)
  })
}

function relationNodeIsHidden(nodeID: string): boolean {
  const selectedID = selectedRelationNodeID.value
  return !!selectedID && selectedID !== nodeID && !relationNodeIsConnected(nodeID)
}

function relationNodeClass(nodeID: string): Record<string, boolean> {
  const selectedID = selectedRelationNodeID.value
  const selected = selectedID === nodeID
  const connected = relationNodeIsConnected(nodeID)
  const hidden = relationNodeIsHidden(nodeID)
  return {
    'is-selected': selected,
    'is-connected': connected,
    'is-hidden': hidden,
    'is-muted': hidden,
  }
}
</script>

<template>
  <section class="card ai-memory-card">
    <div class="ai-memory-command-grid">
      <div class="ai-memory-hero">
        <div class="ai-memory-hero-copy">
          <span class="eyebrow">AI 记忆策略</span>
          <h3>记忆流转与反思治理</h3>
          <p>从最近会话提取候选记忆，经反思筛选后沉淀为长期记忆，并同步更新群画像与关系图。</p>
        </div>
      </div>

      <div class="ai-memory-command-panel">
        <div class="memory-command-top">
          <div>
            <span class="eyebrow">整理动作</span>
            <h4>反思 Worker</h4>
            <p>上次反思：{{ lastReflectionLabel }}</p>
          </div>
          <button class="primary-btn" type="button" :disabled="reflectionBusy || busy" @click="emit('runReflection')">
            {{ reflectionBusy ? '反思执行中...' : '立即执行反思' }}
          </button>
        </div>

        <section class="memory-group-switcher">
          <div>
            <span class="eyebrow">群维度</span>
            <h4>{{ selectedGroupLabel }}</h4>
          </div>
          <div class="memory-group-chip-list" role="tablist" aria-label="切换记忆群聊">
            <button
              v-for="groupID in groupOptions"
              :key="groupID"
              class="memory-group-chip"
              :class="{ active: selectedGroupID === groupID }"
              type="button"
              role="tab"
              :aria-selected="selectedGroupID === groupID"
              @click="selectedGroupID = groupID"
            >
              {{ groupDisplayLabel(groupID) }}
            </button>
          </div>
        </section>
      </div>
    </div>

    <section v-if="reflectionError" class="banner banner-danger ai-memory-alert">
      <strong>反思诊断</strong>
      <span>{{ reflectionError }}</span>
    </section>

    <div class="ai-memory-strategy-strip" aria-label="记忆策略流转">
      <article v-for="item in strategyCards" :key="item.key" class="memory-step-card" :class="`tone-${item.tone}`">
        <span class="memory-step-dot"></span>
        <div>
          <strong>{{ item.label }}</strong>
          <p>{{ item.hint }}</p>
        </div>
        <em>{{ item.value }}</em>
      </article>
    </div>

    <section class="ai-reflection-stats" :class="{ muted: !hasReflectionStats }">
      <div class="ai-reflection-stats-head">
        <div>
          <span class="eyebrow">最近反思结果</span>
          <h4>{{ hasReflectionStats ? '本轮策略调整' : '等待反思数据' }}</h4>
        </div>
        <p>{{ hasReflectionStats ? '展示最近一次反思写入的关键变更。' : '执行反思后，这里会展示沉淀、校准、清理等结果。' }}</p>
      </div>
      <div class="ai-reflection-stat-grid">
        <article v-for="item in reflectionStatCards" :key="item.key" class="memory-mini-stat">
          <span>{{ item.label }}</span>
          <strong>{{ item.value }}</strong>
          <p>{{ item.hint }}</p>
        </article>
      </div>
    </section>

    <div class="ai-memory-board">
      <article class="subcard ai-memory-column ai-memory-column-primary ai-memory-column-candidates">
        <div class="memory-column-head">
          <div>
            <span class="eyebrow">候选池</span>
            <h3>候选记忆</h3>
            <p>建议优先处理高可信度、证据数量充足的候选。</p>
          </div>
          <strong>{{ filteredCandidateMemories.length }}</strong>
        </div>
        <div v-if="!filteredCandidateMemories.length" class="empty-state compact">当前群暂无候选记忆。</div>
        <article v-for="(item, index) in filteredCandidateMemories" v-else :key="stringField(item, 'id') || `candidate-${index}`" class="memory-item-card">
          <div class="memory-item-topline">
            <span class="memory-kind">{{ memoryTypeLabel(stringField(item, 'memory_type')) }}</span>
            <span class="memory-scope">{{ scopeLabel(stringField(item, 'scope')) }}</span>
          </div>
          <p class="memory-preview">{{ memoryPreview(item) }}</p>
          <div class="memory-confidence" :class="confidenceClass(item.confidence)">
            <span>可信度 {{ formatPercent(item.confidence) }}</span>
            <i><b :style="progressStyle(item.confidence)"></b></i>
          </div>
          <div class="memory-card-meta">
            <span>{{ subjectLabel(item) }}</span>
            <span>证据 {{ numberField(item, 'evidence_count') }}</span>
            <span v-if="numberField(item, 'ttl_days')">TTL {{ numberField(item, 'ttl_days') }} 天</span>
          </div>
          <div class="memory-actions">
            <button class="secondary-btn slim-btn" type="button" :disabled="!!memoryActionBusy || busy" @click="emit('runCandidateAction', stringField(item, 'id'), 'promote')">提升</button>
            <button class="danger-btn slim-btn" type="button" :disabled="!!memoryActionBusy || busy" @click="emit('runCandidateAction', stringField(item, 'id'), 'delete')">删除</button>
          </div>
        </article>
      </article>

      <article class="subcard ai-memory-column ai-memory-column-primary ai-memory-column-long-term">
        <div class="memory-column-head">
          <div>
            <span class="eyebrow">沉淀层</span>
            <h3>长期记忆</h3>
            <p>已被反思确认，可直接参与后续回复决策。</p>
          </div>
          <strong>{{ filteredLongTermMemories.length }}</strong>
        </div>
        <div v-if="!filteredLongTermMemories.length" class="empty-state compact">当前群暂无长期记忆。</div>
        <article v-for="(item, index) in filteredLongTermMemories" v-else :key="stringField(item, 'id') || `long-term-${index}`" class="memory-item-card">
          <div class="memory-item-topline">
            <span class="memory-kind">{{ memoryTypeLabel(stringField(item, 'memory_type')) }}</span>
            <span class="memory-scope">{{ scopeLabel(stringField(item, 'scope')) }}</span>
          </div>
          <p class="memory-preview">{{ memoryPreview(item) }}</p>
          <div class="memory-confidence" :class="confidenceClass(item.confidence)">
            <span>稳定度 {{ formatPercent(item.confidence) }}</span>
            <i><b :style="progressStyle(item.confidence)"></b></i>
          </div>
          <div class="memory-card-meta">
            <span>{{ subjectLabel(item) }}</span>
            <span>证据 {{ numberField(item, 'evidence_count') }}</span>
            <span>{{ formatDateTime(stringField(item, 'updated_at')) }}</span>
          </div>
          <div class="memory-actions align-right">
            <button class="danger-btn slim-btn" type="button" :disabled="!!memoryActionBusy || busy" @click="emit('deleteLongTermMemory', stringField(item, 'id'))">删除</button>
          </div>
        </article>
      </article>

      <article class="subcard ai-memory-column ai-memory-column-profile">
        <div class="memory-column-head">
          <div>
            <span class="eyebrow">群策略</span>
            <h3>群画像</h3>
            <p>用于控制群聊回复风格、话题关注和软规则。</p>
          </div>
          <strong>{{ filteredGroupProfiles.length }}</strong>
        </div>
        <div v-if="!filteredGroupProfiles.length" class="empty-state compact">当前群暂无群画像。</div>
        <article v-for="(item, index) in filteredGroupProfiles" v-else :key="stringField(item, 'group_id') || `profile-${index}`" class="memory-item-card profile-card">
          <div class="memory-item-topline">
            <span class="memory-kind">{{ stringField(item, 'group_id') || '群聊' }}</span>
            <span class="memory-scope">{{ formatDateTime(stringField(item, 'updated_at')) }}</span>
          </div>
          <p class="memory-preview">{{ memoryPreview(item) }}</p>
          <div class="memory-metric-list">
            <div v-for="metric in groupMetricRows(item)" :key="metric.key" class="memory-metric-row">
              <span>{{ metric.label }}</span>
              <i><b :style="progressStyle(metric.value)"></b></i>
              <em>{{ formatPercent(metric.value) }}</em>
            </div>
          </div>
          <div v-if="hasAnyTags(item, ['style_tags', 'topic_focus', 'active_memes', 'soft_rules', 'hard_rules'])" class="memory-tag-block">
            <span v-for="tag in tagLimit(item, 'style_tags')" :key="`style-${tag}`" class="chip">{{ tag }}</span>
            <span v-for="tag in tagLimit(item, 'topic_focus')" :key="`topic-${tag}`" class="chip subtle-chip">{{ tag }}</span>
            <span v-for="tag in tagLimit(item, 'active_memes')" :key="`meme-${tag}`" class="chip subtle-chip">{{ tag }}</span>
            <span v-for="tag in tagLimit(item, 'soft_rules', 4)" :key="`soft-${tag}`" class="chip rule-chip">{{ tag }}</span>
            <span v-for="tag in tagLimit(item, 'hard_rules', 4)" :key="`hard-${tag}`" class="chip danger-chip">{{ tag }}</span>
          </div>
        </article>
      </article>

      <article class="subcard ai-memory-column ai-memory-column-sessions">
        <div class="memory-column-head">
          <div>
            <span class="eyebrow">短期上下文</span>
            <h3>最近会话</h3>
            <p>反思提取候选记忆时使用的近期上下文。</p>
          </div>
          <strong>{{ filteredDebugSessions.length }}</strong>
        </div>
        <div v-if="!filteredDebugSessions.length" class="empty-state compact">当前群暂无会话上下文。</div>
        <article v-for="(item, index) in filteredDebugSessions" v-else :key="`${stringField(item, 'scope')}-${stringField(item, 'group_id')}-${index}`" class="memory-item-card session-card">
          <div class="memory-item-topline">
            <span class="memory-kind">{{ sessionTitle(item) }}</span>
            <span class="memory-scope">{{ numberField(item, 'recent_count') }} 条消息</span>
          </div>
          <p class="memory-preview">{{ memoryPreview(item) }}</p>
          <div class="memory-card-meta">
            <span>活跃用户 {{ arrayField(item, 'active_users').length }}</span>
            <span>{{ formatDateTime(stringField(item, 'updated_at')) }}</span>
          </div>
          <div v-if="arrayField(item, 'active_users').length" class="memory-tag-block compact-tags">
            <span v-for="user in tagLimit(item, 'active_users', 10)" :key="user" class="chip subtle-chip">{{ user }}</span>
          </div>
        </article>
      </article>

      <article class="subcard ai-memory-column ai-memory-column-observations">
        <div class="memory-column-head">
          <div>
            <span class="eyebrow">观察层</span>
            <h3>群观察</h3>
            <p>展示本轮群聊观察摘要、风险标记和候选亮点。</p>
          </div>
          <strong>{{ filteredGroupObservations.length }}</strong>
        </div>
        <div v-if="!filteredGroupObservations.length" class="empty-state compact">当前群暂无群观察。</div>
        <article v-for="(item, index) in filteredGroupObservations" v-else :key="`${stringField(item, 'group_id')}-${stringField(item, 'updated_at')}-${index}`" class="memory-item-card observation-card">
          <div class="memory-item-topline">
            <span class="memory-kind">{{ stringField(item, 'group_id') || '群聊' }}</span>
            <span class="memory-scope">{{ formatDateTime(stringField(item, 'updated_at')) }}</span>
          </div>
          <p class="memory-preview">{{ memoryPreview(item) }}</p>
          <div class="memory-card-meta">
            <span>会话 {{ numberField(item, 'session_message_count') }} 条</span>
            <span>候选 {{ arrayField(item, 'candidate_highlights').length }}</span>
            <span>长期 {{ arrayField(item, 'long_term_highlights').length }}</span>
          </div>
          <div v-if="hasAnyTags(item, ['style_tags', 'topic_focus', 'active_memes', 'active_users', 'risk_flags'])" class="memory-tag-block compact-tags">
            <span v-for="tag in tagLimit(item, 'style_tags')" :key="`obs-style-${tag}`" class="chip">{{ tag }}</span>
            <span v-for="tag in tagLimit(item, 'topic_focus')" :key="`obs-topic-${tag}`" class="chip subtle-chip">{{ tag }}</span>
            <span v-for="tag in tagLimit(item, 'active_memes')" :key="`obs-meme-${tag}`" class="chip subtle-chip">{{ tag }}</span>
            <span v-for="tag in tagLimit(item, 'active_users', 8)" :key="`obs-user-${tag}`" class="chip subtle-chip">{{ tag }}</span>
            <span v-for="tag in tagLimit(item, 'risk_flags')" :key="`obs-risk-${tag}`" class="chip danger-chip">{{ tag }}</span>
          </div>
        </article>
      </article>

      <article class="subcard ai-memory-column ai-personality-report-card">
        <div class="memory-column-head">
          <div>
            <span class="eyebrow">群友分析</span>
            <h3>性格分析 Markdown</h3>
            <p>基于用户画像、互动关系与偏好标签生成可复制、可下载的群友分析文档。</p>
          </div>
          <strong>{{ filteredUserProfiles.length }}</strong>
        </div>
        <div class="personality-report-actions">
          <button
            class="primary-btn slim-btn"
            type="button"
            :disabled="relationAnalysisDisabled"
            @click="runRelationAnalysis(false)"
          >
            {{ relationAnalysisPrimaryLabel }}
          </button>
          <button
            class="secondary-btn slim-btn"
            type="button"
            :disabled="relationAnalysisDisabled"
            @click="runRelationAnalysis(true)"
          >
            强制刷新
          </button>
          <button class="secondary-btn slim-btn" type="button" :disabled="!effectivePersonalityReportMarkdown" @click="downloadPersonalityReport">下载 MD</button>
          <button class="secondary-btn slim-btn" type="button" :disabled="!effectivePersonalityReportMarkdown" @click="copyPersonalityReport">复制 MD</button>
          <span v-if="relationAnalysisTaskMeta" class="personality-report-task" :class="{ busy: relationAnalysisBusy }">{{ relationAnalysisTaskMeta }}</span>
          <span v-if="llmPersonalityReportMeta" class="personality-report-source">{{ llmPersonalityReportMeta }}</span>
          <span v-if="personalityReportStatus">{{ personalityReportStatus }}</span>
        </div>
        <div v-if="!filteredUserProfiles.length" class="empty-state compact">当前群暂无可分析的用户画像。</div>
        <div v-else class="personality-report-preview">
          <div class="relation-insight-row">
            <span>关系类型</span>
            <p v-if="relationTypeBreakdown.length">
              <em v-for="item in relationTypeBreakdown" :key="item.value" :style="{ '--relation-color': relationTypeColor(item.value) }">
                {{ item.label }} {{ item.count }}
              </em>
            </p>
            <p v-else>暂无关系边。</p>
          </div>
          <div class="relation-insight-row">
            <span>最强关系</span>
            <p v-if="strongestRelationEdges.length">
              <em
                v-for="item in strongestRelationEdges"
                :key="stringField(item, 'id') || `${stringField(item, 'node_a')}-${stringField(item, 'node_b')}-${stringField(item, 'relation_type')}`"
                :style="{ '--relation-color': relationTypeColor(stringField(item, 'relation_type')) }"
              >
                {{ relationNodeLabel(stringField(item, 'node_a'), stringField(item, 'group_id')) }} ↔
                {{ relationNodeLabel(stringField(item, 'node_b'), stringField(item, 'group_id')) }} ·
                {{ relationTitle(item) }}
              </em>
            </p>
            <p v-else>暂无可排序关系。</p>
          </div>
          <pre>{{ effectivePersonalityReportMarkdown }}</pre>
        </div>
      </article>

      <article class="subcard ai-memory-column ai-memory-column-wide">
        <div class="memory-column-head">
          <div>
            <span class="eyebrow">关系网络</span>
            <h3>关系边</h3>
            <p>把用户、群聊与主题之间的关系强度集中展示，方便判断记忆是否形成稳定关系。</p>
          </div>
          <strong>{{ visibleRelationEdges.length }}</strong>
        </div>
        <div v-if="!filteredRelationEdges.length" class="empty-state compact">当前群暂无关系边。</div>
        <div v-else class="ai-relation-graph-panel">
          <div class="relation-graph-stage">
            <div class="relation-focus-bar">
              <div>
                <span class="eyebrow">节点聚焦</span>
                <p>{{ selectedRelationNodeID ? `正在查看 ${selectedRelationNodeLabel} 的中心放射关系线` : '点击头像后移至中心，并放射展示该成员关系线。' }}</p>
              </div>
              <button v-if="selectedRelationNodeID" class="secondary-btn slim-btn" type="button" @click="clearRelationFocus">显示全部</button>
            </div>
            <div class="relation-type-filter" role="tablist" aria-label="筛选关系类型">
              <button
                v-for="option in relationTypeOptions"
                :key="option.value"
                class="relation-type-chip"
                :class="{ active: selectedRelationType === option.value }"
                :style="{ '--relation-color': relationTypeColor(option.value) }"
                type="button"
                role="tab"
                :aria-selected="selectedRelationType === option.value"
                @click="selectedRelationType = option.value"
              >
                <span>{{ option.label }}</span>
                <b>{{ option.count }}</b>
              </button>
            </div>
            <div class="relation-graph-canvas" aria-label="关系知识图谱">
              <svg class="relation-graph-lines" viewBox="0 0 100 100" role="img">
                <line
                  v-for="edge in relationGraphDisplayEdges"
                  :key="`${edge.id}-${selectedRelationNodeID || 'all'}`"
                  class="relation-graph-link"
                  :class="relationGraphEdgeClass(edge)"
                  :style="relationGraphEdgeStyle(edge)"
                  :x1="edge.x1"
                  :y1="edge.y1"
                  :x2="edge.x2"
                  :y2="edge.y2"
                  :stroke-width="0.55 + edge.strength * 1.05"
                />
              </svg>
              <div class="relation-graph-nodes">
                <button
                  v-for="node in relationGraph.nodes"
                  :key="node.id"
                  class="relation-graph-node"
                  :class="relationNodeClass(node.id)"
                  :style="relationNodeStyle(node)"
                  :title="node.label"
                  type="button"
                  :tabindex="relationNodeIsHidden(node.id) ? -1 : 0"
                  :aria-hidden="relationNodeIsHidden(node.id) ? 'true' : undefined"
                  :aria-pressed="selectedRelationNodeID === node.id"
                  :aria-label="selectedRelationNodeID === node.id ? `取消聚焦 ${node.label}` : `聚焦 ${node.label} 的关系线`"
                  @click="toggleRelationNode(node.id)"
                >
                  <img v-if="node.avatarURL" :src="node.avatarURL" :alt="node.label" loading="lazy" />
                  <span v-else>{{ node.initial }}</span>
                  <em>{{ node.shortLabel }}</em>
                </button>
              </div>
            </div>
          </div>
          <div class="ai-relation-list">
            <div class="relation-list-head">
              <div>
              <span class="eyebrow">关系列表</span>
                <h4>{{ selectedRelationNodeID ? selectedRelationNodeLabel : relationTypeLabel(selectedRelationType) }}</h4>
              </div>
              <strong>{{ focusedRelationEdges.length }}</strong>
            </div>
            <article
              v-for="(item, index) in focusedRelationEdges"
              :key="stringField(item, 'id') || `${stringField(item, 'node_a')}-${stringField(item, 'node_b')}-${index}`"
              class="ai-relation-edge"
              :class="relationRecordClass(item)"
              :style="relationRecordStyle(item)"
            >
              <span class="relation-node">{{ relationNodeLabel(stringField(item, 'node_a'), stringField(item, 'group_id')) }}</span>
              <div class="relation-bridge">
                <strong>{{ relationTitle(item) }}</strong>
                <i><b :style="progressStyle(item.strength)"></b></i>
                <em>{{ formatPercent(item.strength) }} · 证据 {{ numberField(item, 'evidence_count') }}</em>
                <small>{{ relationEvidenceHint(item) }}</small>
              </div>
              <span class="relation-node align-end">{{ relationNodeLabel(stringField(item, 'node_b'), stringField(item, 'group_id')) }}</span>
            </article>
          </div>
        </div>
      </article>
    </div>
  </section>
</template>

<style scoped>
.ai-memory-card {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.ai-memory-command-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.35fr) minmax(320px, 0.65fr);
  gap: 16px;
  align-items: stretch;
}

.ai-memory-hero {
  display: flex;
  align-items: center;
  min-height: 100%;
  padding: 22px;
  border: 1px solid var(--accent-border);
  border-radius: 22px;
  background:
    radial-gradient(circle at 8% 0%, var(--accent-ring), transparent 34%),
    linear-gradient(135deg, var(--surface-soft-alt), var(--card-bg));
}

.ai-memory-hero-copy h3,
.ai-memory-hero-copy p {
  margin: 0;
}

.ai-memory-hero-copy h3 {
  color: var(--text-primary);
  font-size: 22px;
}

.ai-memory-hero-copy p {
  margin-top: 8px;
  color: var(--text-secondary);
  line-height: 1.7;
  max-width: 760px;
}

.ai-memory-command-panel {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-width: 0;
  padding: 16px;
  border: 1px solid var(--soft-border);
  border-radius: 22px;
  background: var(--surface-soft);
}

.memory-command-top {
  display: flex;
  justify-content: space-between;
  gap: 14px;
  align-items: flex-start;
}

.memory-command-top h4,
.memory-command-top p {
  margin: 0;
}

.memory-command-top h4 {
  color: var(--text-primary);
}

.memory-command-top p {
  margin-top: 4px;
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.5;
}

.ai-memory-alert {
  margin: 0;
}

.memory-group-switcher {
  display: grid;
  grid-template-columns: 1fr;
  gap: 10px;
  align-items: start;
  padding: 12px;
  border: 1px solid var(--soft-border);
  border-radius: 18px;
  background: var(--card-bg);
}

.memory-group-switcher h4 {
  margin: 2px 0 0;
  color: var(--text-primary);
}

.memory-group-chip-list {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-start;
  gap: 8px;
  max-height: 108px;
  overflow-y: auto;
}

.memory-group-chip {
  min-height: 34px;
  padding: 0 12px;
  border: 1px solid var(--soft-border);
  border-radius: 999px;
  background: var(--card-bg);
  color: var(--text-secondary);
  font: inherit;
  font-size: 12px;
  font-weight: 700;
  cursor: pointer;
  transition: background 160ms ease, border-color 160ms ease, color 160ms ease, box-shadow 160ms ease;
}

.memory-group-chip:hover,
.memory-group-chip.active {
  border-color: var(--accent-border);
  background: var(--selection-bg);
  color: var(--accent-strong);
  box-shadow: 0 8px 18px var(--selection-shadow);
}

.ai-memory-strategy-strip {
  display: grid;
  grid-template-columns: repeat(5, minmax(0, 1fr));
  gap: 12px;
}

.memory-step-card {
  position: relative;
  min-height: 118px;
  padding: 16px;
  border: 1px solid var(--soft-border);
  border-radius: 18px;
  background: var(--surface-soft-alt);
  overflow: hidden;
}

.memory-step-card::after {
  content: '';
  position: absolute;
  inset: auto -28px -36px auto;
  width: 90px;
  height: 90px;
  border-radius: 50%;
  background: var(--accent-ring);
}

.memory-step-dot {
  display: inline-flex;
  width: 10px;
  height: 10px;
  border-radius: 999px;
  background: var(--accent);
  box-shadow: 0 0 0 6px var(--accent-ring);
}

.memory-step-card strong,
.memory-step-card p,
.memory-step-card em {
  position: relative;
  z-index: 1;
}

.memory-step-card strong {
  display: block;
  margin-top: 14px;
  color: var(--text-primary);
  font-size: 14px;
}

.memory-step-card p {
  margin: 4px 0 0;
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.5;
}

.memory-step-card em {
  position: absolute;
  right: 16px;
  bottom: 12px;
  color: var(--accent-strong);
  font-size: 28px;
  font-style: normal;
  font-weight: 800;
}

.memory-step-card.tone-amber .memory-step-dot {
  background: #f59e0b;
}

.memory-step-card.tone-amber em {
  color: #b45309;
}

.memory-step-card.tone-green .memory-step-dot {
  background: #10b981;
}

.memory-step-card.tone-green em {
  color: #047857;
}

.memory-step-card.tone-violet .memory-step-dot {
  background: #8b5cf6;
}

.memory-step-card.tone-violet em {
  color: #6d28d9;
}

.memory-step-card.tone-rose .memory-step-dot {
  background: #f0a7bf;
}

.memory-step-card.tone-rose em {
  color: #be466e;
}

.ai-reflection-stats {
  display: grid;
  grid-template-columns: minmax(220px, 0.8fr) minmax(0, 2fr);
  gap: 16px;
  align-items: stretch;
  padding: 16px;
  border: 1px solid var(--soft-border);
  border-radius: 20px;
  background: var(--surface-soft);
}

.ai-reflection-stats.muted {
  opacity: 0.86;
}

.ai-reflection-stats-head {
  display: flex;
  flex-direction: column;
  justify-content: center;
  gap: 8px;
}

.ai-reflection-stats-head h4,
.ai-reflection-stats-head p,
.memory-mini-stat p {
  margin: 0;
}

.ai-reflection-stats-head h4 {
  color: var(--text-primary);
}

.ai-reflection-stats-head p,
.memory-mini-stat p {
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.5;
}

.ai-reflection-stat-grid {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: 10px;
}

.memory-mini-stat {
  padding: 12px;
  border: 1px solid var(--soft-border);
  border-radius: 16px;
  background: var(--card-bg);
}

.memory-mini-stat span {
  color: var(--text-muted);
  font-size: 12px;
}

.memory-mini-stat strong {
  display: block;
  margin: 4px 0;
  color: var(--accent-strong);
  font-size: 24px;
}

.ai-memory-board {
  display: grid;
  grid-template-columns: minmax(0, 1.05fr) minmax(0, 1.05fr) minmax(300px, 0.9fr);
  grid-template-areas:
    "candidates longterm profile"
    "candidates longterm sessions"
    "observations observations observations"
    "personality personality personality"
    "relations relations relations";
  gap: 16px;
  align-items: start;
}

.ai-memory-column {
  display: flex;
  flex-direction: column;
  gap: 12px;
  min-height: 0;
  max-height: 620px;
  overflow-y: auto;
}

.ai-memory-column-candidates {
  grid-area: candidates;
}

.ai-memory-column-long-term {
  grid-area: longterm;
}

.ai-memory-column-profile {
  grid-area: profile;
}

.ai-memory-column-sessions {
  grid-area: sessions;
}

.ai-memory-column-observations {
  grid-area: observations;
  max-height: 420px;
}

.ai-memory-column-primary {
  max-height: 860px;
}

.ai-memory-column-wide {
  grid-area: relations;
  grid-column: 1 / -1;
  max-height: none;
}

.memory-column-head {
  position: sticky;
  top: -22px;
  z-index: 2;
  display: flex;
  justify-content: space-between;
  gap: 16px;
  padding-bottom: 12px;
  border-bottom: 1px solid var(--soft-divider);
  background: linear-gradient(180deg, var(--card-bg) 0%, var(--card-bg) 72%, transparent 100%);
}

.memory-column-head h3,
.memory-column-head p {
  margin: 0;
}

.memory-column-head h3 {
  color: var(--text-primary);
}

.memory-column-head p {
  margin-top: 6px;
  color: var(--text-muted);
  font-size: 12px;
  line-height: 1.55;
}

.memory-column-head > strong {
  flex: 0 0 auto;
  color: var(--accent-strong);
  font-size: 28px;
  line-height: 1;
}

.memory-item-card {
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 14px;
  border: 1px solid var(--soft-border);
  border-radius: 18px;
  background: var(--surface-soft-alt);
  transition: border-color 180ms ease, box-shadow 180ms ease, background 180ms ease;
}

.memory-item-card:hover {
  border-color: var(--accent-border);
  box-shadow: 0 10px 28px var(--selection-shadow);
}

.memory-item-topline,
.memory-card-meta,
.memory-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}

.memory-item-topline,
.memory-card-meta {
  justify-content: space-between;
}

.memory-kind,
.memory-scope,
.memory-card-meta span {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.memory-kind {
  color: var(--accent-strong);
  font-size: 13px;
  font-weight: 800;
}

.memory-scope,
.memory-card-meta span {
  color: var(--text-muted);
  font-size: 12px;
}

.memory-preview {
  margin: 0;
  color: var(--text-primary);
  font-size: 13px;
  line-height: 1.65;
  white-space: pre-wrap;
  word-break: break-word;
}

.memory-confidence {
  display: grid;
  grid-template-columns: auto minmax(88px, 1fr);
  gap: 10px;
  align-items: center;
  color: var(--text-muted);
  font-size: 12px;
}

.memory-confidence i,
.memory-metric-row i,
.relation-bridge i {
  display: block;
  height: 7px;
  border-radius: 999px;
  background: var(--soft-divider);
  overflow: hidden;
}

.memory-confidence b,
.memory-metric-row b,
.relation-bridge b {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--accent);
}

.memory-confidence.is-strong b {
  background: #10b981;
}

.memory-confidence.is-medium b {
  background: #f59e0b;
}

.memory-confidence.is-weak b {
  background: #f0a7bf;
}

.memory-actions {
  justify-content: flex-start;
  flex-wrap: wrap;
}

.memory-actions.align-right {
  justify-content: flex-end;
}

.memory-metric-list {
  display: grid;
  gap: 8px;
}

.memory-metric-row {
  display: grid;
  grid-template-columns: 72px minmax(0, 1fr) 42px;
  gap: 8px;
  align-items: center;
  color: var(--text-muted);
  font-size: 12px;
}

.memory-metric-row em,
.relation-bridge em {
  color: var(--text-muted);
  font-style: normal;
  font-size: 12px;
}

.memory-tag-block {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}

.memory-tag-block .chip {
  min-height: 30px;
  padding: 0 10px;
  font-size: 12px;
}

.subtle-chip {
  background: var(--surface-soft-deep);
}

.rule-chip {
  border-color: var(--accent-border);
  background: var(--selection-bg);
  color: var(--accent-strong);
}

.danger-chip {
  border-color: var(--danger-border);
  background: var(--danger-bg);
  color: var(--danger-text);
}

.ai-personality-report-card {
  grid-area: personality;
  grid-column: 1 / -1;
}

.personality-report-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  align-items: center;
}

.personality-report-actions span {
  color: var(--text-secondary);
  font-size: 13px;
}

.personality-report-actions .personality-report-source {
  padding: 5px 9px;
  border: 1px solid var(--accent-border);
  border-radius: 999px;
  background: var(--selection-bg);
  color: var(--accent-strong);
  font-weight: 800;
}

.personality-report-actions .personality-report-task {
  padding: 5px 9px;
  border: 1px solid color-mix(in srgb, var(--border-color) 72%, var(--accent-color) 28%);
  border-radius: 999px;
  background: color-mix(in srgb, var(--surface-elevated) 78%, var(--accent-soft) 22%);
  color: var(--text-primary);
  font-weight: 700;
}

.personality-report-actions .personality-report-task.busy {
  border-color: var(--accent-border);
  color: var(--accent-strong);
  animation: relation-analysis-pulse 1.4s ease-in-out infinite;
}

@keyframes relation-analysis-pulse {
  0%,
  100% {
    box-shadow: 0 0 0 0 color-mix(in srgb, var(--accent-color) 0%, transparent 100%);
  }
  50% {
    box-shadow: 0 0 0 7px color-mix(in srgb, var(--accent-color) 16%, transparent 84%);
  }
}

@media (prefers-reduced-motion: reduce) {
  .personality-report-actions .personality-report-task.busy {
    animation: none;
  }
}

.personality-report-preview {
  display: grid;
  grid-template-columns: minmax(280px, 0.58fr) minmax(320px, 1fr);
  gap: 12px;
  align-items: stretch;
}

.relation-insight-row {
  display: grid;
  gap: 8px;
  align-content: start;
  min-height: 108px;
  padding: 14px;
  border: 1px solid var(--soft-border);
  border-radius: 18px;
  background: var(--surface-soft-alt);
}

.relation-insight-row span {
  color: var(--text-muted);
  font-size: 12px;
  font-weight: 800;
}

.relation-insight-row p {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  margin: 0;
  color: var(--text-secondary);
}

.relation-insight-row em {
  display: inline-flex;
  align-items: center;
  min-height: 28px;
  padding: 0 9px;
  border: 1px solid color-mix(in srgb, var(--relation-color, var(--accent)) 30%, var(--soft-border));
  border-radius: 999px;
  background: color-mix(in srgb, var(--relation-color, var(--accent)) 8%, var(--card-bg));
  color: color-mix(in srgb, var(--relation-color, var(--accent)) 72%, var(--text-primary));
  font-size: 12px;
  font-style: normal;
  font-weight: 800;
}

.personality-report-preview pre {
  grid-column: 2;
  grid-row: 1 / span 2;
  min-height: 228px;
  max-height: 360px;
  margin: 0;
  padding: 16px;
  border: 1px solid var(--soft-border);
  border-radius: 18px;
  background: color-mix(in srgb, var(--surface-soft-deep) 72%, var(--card-bg));
  color: var(--text-secondary);
  font-size: 12px;
  line-height: 1.6;
  overflow: auto;
  white-space: pre-wrap;
}

.ai-relation-graph-panel {
  display: grid;
  grid-template-columns: minmax(520px, 1.45fr) minmax(320px, 0.75fr);
  gap: 18px;
  align-items: stretch;
}

.relation-graph-stage {
  display: grid;
  grid-template-rows: auto auto minmax(520px, 1fr);
  gap: 12px;
  min-width: 0;
}

.relation-focus-bar,
.relation-list-head {
  display: flex;
  justify-content: space-between;
  gap: 12px;
  align-items: center;
  padding: 12px 14px;
  border: 1px solid var(--soft-border);
  border-radius: 18px;
  background: var(--surface-soft);
}

.relation-focus-bar p,
.relation-list-head h4 {
  margin: 2px 0 0;
  color: var(--text-secondary);
  font-size: 13px;
  line-height: 1.5;
}

.relation-list-head h4 {
  color: var(--text-primary);
  font-size: 15px;
}

.relation-list-head strong {
  color: var(--accent-strong);
  font-size: 24px;
  line-height: 1;
}

.relation-type-filter {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  align-items: center;
  padding: 10px 12px;
  border: 1px solid var(--soft-border);
  border-radius: 18px;
  background: var(--surface-soft-alt);
}

.relation-type-chip {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-height: 34px;
  padding: 0 11px;
  border: 1px solid color-mix(in srgb, var(--relation-color, var(--accent)) 28%, var(--soft-border));
  border-radius: 999px;
  background: color-mix(in srgb, var(--relation-color, var(--accent)) 8%, var(--card-bg));
  color: var(--text-secondary);
  cursor: pointer;
  transition: border-color 180ms ease, background 180ms ease, color 180ms ease, box-shadow 180ms ease;
}

.relation-type-chip:hover,
.relation-type-chip:focus-visible,
.relation-type-chip.active {
  border-color: color-mix(in srgb, var(--relation-color, var(--accent)) 70%, var(--accent-border));
  background: color-mix(in srgb, var(--relation-color, var(--accent)) 14%, var(--card-bg));
  color: var(--text-primary);
  box-shadow: 0 8px 18px color-mix(in srgb, var(--relation-color, var(--accent)) 18%, transparent);
  outline: none;
}

.relation-type-chip b {
  display: inline-grid;
  place-items: center;
  min-width: 22px;
  height: 22px;
  padding: 0 6px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--relation-color, var(--accent)) 18%, transparent);
  color: color-mix(in srgb, var(--relation-color, var(--accent)) 78%, var(--text-primary));
  font-size: 12px;
}

.relation-graph-canvas {
  position: relative;
  min-height: 560px;
  border: 1px solid var(--soft-border);
  border-radius: 26px;
  background:
    radial-gradient(circle at 50% 46%, var(--accent-ring), transparent 36%),
    radial-gradient(circle at 78% 18%, color-mix(in srgb, var(--accent) 18%, transparent), transparent 24%),
    linear-gradient(135deg, var(--surface-soft-alt), var(--card-bg));
  overflow: hidden;
}

.relation-graph-canvas::before {
  content: '';
  position: absolute;
  inset: 22px;
  border: 1px dashed var(--soft-border);
  border-radius: 50%;
  opacity: 0.55;
  pointer-events: none;
  z-index: 0;
}

.relation-graph-lines {
  position: absolute;
  inset: 0;
  display: block;
  width: 100%;
  height: 100%;
  pointer-events: none;
  z-index: 1;
}

.relation-graph-link {
  stroke: var(--relation-color, var(--accent));
  stroke-linecap: round;
  opacity: 0.34;
  transition: opacity 180ms ease, stroke 180ms ease, stroke-width 180ms ease, filter 180ms ease;
}

.relation-graph-link.is-focused {
  stroke: var(--relation-color, #f97316);
  opacity: 0.86;
  filter: drop-shadow(0 0 4px color-mix(in srgb, var(--relation-color, #f97316) 62%, transparent));
}

.relation-graph-link.is-radial {
  stroke-dasharray: 96;
  stroke-dashoffset: 96;
  animation: relation-ray-draw 520ms cubic-bezier(0.2, 0.8, 0.2, 1) var(--ray-delay, 0ms) forwards;
}

.relation-graph-link.is-muted {
  opacity: 0;
}

.relation-graph-nodes {
  position: absolute;
  inset: 0;
  z-index: 3;
}

.relation-graph-node {
  position: absolute;
  transform: translate(-50%, -50%);
  display: grid;
  place-items: center;
  padding: 0;
  border: 3px solid var(--card-bg);
  border-radius: 50%;
  background: linear-gradient(135deg, var(--accent), var(--accent-strong));
  box-shadow: 0 12px 26px var(--selection-shadow);
  cursor: pointer;
  transition:
    left 420ms cubic-bezier(0.2, 0.8, 0.2, 1),
    top 420ms cubic-bezier(0.2, 0.8, 0.2, 1),
    width 360ms cubic-bezier(0.2, 0.8, 0.2, 1),
    height 360ms cubic-bezier(0.2, 0.8, 0.2, 1),
    transform 220ms ease,
    opacity 220ms ease,
    box-shadow 220ms ease,
    border-color 220ms ease,
    filter 220ms ease;
}

.relation-graph-node:hover,
.relation-graph-node:focus-visible {
  transform: translate(-50%, -50%) translateY(-2px);
  border-color: #f97316;
  box-shadow: 0 18px 36px var(--selection-shadow), 0 0 0 6px color-mix(in srgb, #f97316 18%, transparent);
  outline: none;
}

.relation-graph-node.is-selected {
  z-index: 5;
  border-color: #f97316;
  box-shadow:
    0 22px 48px var(--selection-shadow),
    0 0 0 8px color-mix(in srgb, #f97316 22%, transparent),
    0 0 34px color-mix(in srgb, #f97316 36%, transparent);
  filter: saturate(1.12);
}

.relation-graph-node.is-connected {
  z-index: 3;
  border-color: var(--accent);
  box-shadow: 0 16px 32px var(--selection-shadow), 0 0 0 6px var(--accent-ring);
}

.relation-graph-node.is-muted {
  opacity: 0.34;
  filter: grayscale(0.65);
}

.relation-graph-node.is-hidden {
  opacity: 0;
  pointer-events: none;
  transform: translate(-50%, -50%) scale(0.34);
  filter: blur(2px) grayscale(0.8);
}

.relation-graph-node img,
.relation-graph-node > span {
  width: 100%;
  height: 100%;
  border-radius: inherit;
}

.relation-graph-node img {
  display: block;
  object-fit: cover;
}

.relation-graph-node > span {
  display: grid;
  place-items: center;
  color: white;
  font-weight: 900;
}

.relation-graph-node em {
  position: absolute;
  left: 50%;
  top: calc(100% + 5px);
  transform: translateX(-50%);
  max-width: 92px;
  padding: 3px 7px;
  border: 1px solid var(--soft-border);
  border-radius: 999px;
  background: color-mix(in srgb, var(--card-bg) 90%, transparent);
  color: var(--text-secondary);
  font-size: 11px;
  font-style: normal;
  font-weight: 800;
  line-height: 1.2;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ai-relation-list {
  display: grid;
  grid-template-columns: 1fr;
  gap: 10px;
  align-content: start;
  max-height: 638px;
  overflow-y: auto;
}

.ai-relation-edge {
  position: relative;
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(150px, 0.9fr) minmax(0, 1fr);
  gap: 12px;
  align-items: center;
  padding: 12px 14px;
  border: 1px solid var(--soft-border);
  border-radius: 18px;
  background: var(--surface-soft-alt);
  transition: border-color 180ms ease, background 180ms ease, box-shadow 180ms ease, opacity 180ms ease;
}

.ai-relation-edge::before {
  content: '';
  position: absolute;
  left: 10px;
  top: 12px;
  bottom: 12px;
  width: 3px;
  border-radius: 999px;
  background: var(--relation-color, var(--accent));
  opacity: 0.78;
}

.ai-relation-edge.is-focused {
  border-color: color-mix(in srgb, var(--relation-color, #f97316) 60%, var(--accent-border));
  background: color-mix(in srgb, var(--relation-color, #f97316) 9%, var(--surface-soft-alt));
  box-shadow: 0 10px 24px color-mix(in srgb, var(--relation-color, #f97316) 16%, transparent);
}

.ai-relation-edge.is-muted {
  opacity: 0.42;
}

.relation-node {
  min-width: 0;
  overflow: hidden;
  color: var(--text-secondary);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.relation-node.align-end {
  text-align: right;
}

.relation-bridge {
  display: grid;
  gap: 6px;
}

.relation-bridge strong {
  color: color-mix(in srgb, var(--relation-color, var(--accent)) 72%, var(--text-primary));
  font-size: 12px;
  text-align: center;
}

.ai-relation-edge .relation-bridge b {
  background: var(--relation-color, var(--accent));
}

.relation-bridge small {
  color: var(--text-muted);
  font-size: 11px;
  line-height: 1.45;
  text-align: center;
}

.compact {
  padding: 12px 16px;
  margin-bottom: 0;
}

@keyframes relation-ray-draw {
  from {
    stroke-dashoffset: 96;
  }

  to {
    stroke-dashoffset: 0;
  }
}

@media (prefers-reduced-motion: reduce) {
  .relation-graph-node,
  .relation-graph-link {
    transition: none;
  }

  .relation-graph-link.is-radial {
    animation: none;
  }
}

@media (max-width: 1280px) {
  .ai-memory-strategy-strip,
  .ai-reflection-stat-grid {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }

  .ai-memory-board {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    grid-template-areas:
      "candidates longterm"
      "profile sessions"
      "observations observations"
      "personality personality"
      "relations relations";
  }

  .ai-relation-list {
    grid-template-columns: 1fr;
  }

  .ai-relation-graph-panel {
    grid-template-columns: 1fr;
  }

  .personality-report-preview {
    grid-template-columns: 1fr;
  }

  .personality-report-preview pre {
    grid-column: auto;
    grid-row: auto;
  }
}

@media (max-width: 860px) {
  .ai-memory-command-grid,
  .ai-reflection-stats,
  .ai-memory-board {
    grid-template-columns: 1fr;
  }

  .ai-memory-board {
    grid-template-areas:
      "candidates"
      "longterm"
      "profile"
      "sessions"
      "observations"
      "personality"
      "relations";
  }

  .memory-command-top {
    flex-direction: column;
    align-items: stretch;
  }

  .memory-group-switcher {
    grid-template-columns: 1fr;
  }

  .memory-group-chip-list {
    justify-content: flex-start;
  }

  .ai-memory-strategy-strip,
  .ai-reflection-stat-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .ai-memory-column,
  .ai-memory-column-primary,
  .ai-memory-column-wide {
    max-height: none;
  }

  .memory-column-head {
    position: static;
  }
}

@media (max-width: 560px) {
  .ai-memory-strategy-strip,
  .ai-reflection-stat-grid {
    grid-template-columns: 1fr;
  }

  .ai-relation-edge {
    grid-template-columns: 1fr;
  }

  .relation-graph-canvas {
    min-height: 280px;
  }

  .relation-node.align-end,
  .relation-bridge strong {
    text-align: left;
  }
}
</style>
