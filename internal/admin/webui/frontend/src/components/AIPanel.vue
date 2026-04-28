<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { APIError, requestJSON } from '../lib/http'
import { cloneRecord } from '../lib/object-path'

import AIConfig from './AIConfig.vue'
import AIMemoryGrid from './AIMemoryGrid.vue'
import AIMCPPanel from './AIMCPPanel.vue'
import AISkillsPanel from './AISkillsPanel.vue'

import type {
  AIConfigSaveResult,
  AIForwardMessage,
  AIForwardMessageNode,
  AIMessageSegment,
  AIMemoryActionResult,
  AIMessageDetail,
  AIMessageImage,
  AIMessageLog,
  AIMessageLogResponse,
  AIRecentMessagesBulkSyncResult,
  AIRecentMessagesSyncResult,
  AIMessageSendResult,
  AIMessageSuggestions,
  AIView,
  ConnectionSnapshot,
  RuntimeConfig,
} from '../types/api'

type MessageConversation = {
  key: string
  title: string
  subtitle: string
  avatarURL: string
  messages: AIMessageLog[]
  unreadImages: number
  lastAt: string
}

type ForwardMessageRef = {
  id: string
  connectionID: string
}

type ConversationSendTarget = {
  connectionID: string
  chatType: 'group' | 'private'
  groupID: string
  userID: string
  label: string
}

const messagesPerPage = 10

const props = defineProps<{
  runtimeConfig: RuntimeConfig
  busy: boolean
  connections?: ConnectionSnapshot[]
}>()

const emit = defineEmits<{
  busy: [value: boolean]
  unauthorized: []
  notice: [payload: { kind: 'success' | 'error' | 'info'; title: string; text: string }]
}>()

const view = ref<AIView | null>(null)
const draft = ref<Record<string, unknown>>({})
const activeTab = ref('logs')
const loading = ref(false)
const messagesLoading = ref(false)
const detailLoading = ref(false)
const syncMessagesLoading = ref(false)
const bulkSyncKind = ref('')
const reflectionBusy = ref(false)
const memoryActionBusy = ref('')
const localError = ref('')
const messageError = ref('')
const messageLogs = ref<AIMessageLog[]>([])
const selectedMessage = ref<AIMessageDetail | null>(null)
const inlineImageMap = ref<Record<string, AIMessageImage[]>>({})
const inlineImageLoading = ref<Record<string, boolean>>({})
const forwardMessageMap = ref<Record<string, AIForwardMessage>>({})
const forwardMessageLoading = ref<Record<string, boolean>>({})
const forwardMessageError = ref<Record<string, string>>({})
const sendDraft = ref('')
const sendLoading = ref(false)
const sendError = ref('')
const messageSyncLimit = ref(50)
const messageConnectionID = ref('')
const messageChatType = ref('')
const messageGroupID = ref('')
const messageUserID = ref('')
const messageKeyword = ref('')
const selectedConversationKey = ref('')
const messagePage = ref(1)
const messageSuggestions = ref<AIMessageSuggestions>({})

const snapshot = computed(() => view.value?.snapshot || null)
const configLoaded = computed(() => Object.keys(draft.value).length > 0)
const conversations = computed<MessageConversation[]>(() => buildConversations(messageLogs.value))
const activeConversation = computed(() => conversations.value.find((item) => item.key === selectedConversationKey.value) || conversations.value[0] || null)
const activeConversationMessages = computed(() => activeConversation.value?.messages || [])
const messagePageCount = computed(() => Math.max(1, Math.ceil(activeConversationMessages.value.length / messagesPerPage)))
const activeMessagePage = computed(() => Math.min(Math.max(messagePage.value, 1), messagePageCount.value))
const pagedActiveConversationMessages = computed(() => {
  const end = activeMessagePage.value * messagesPerPage
  return activeConversationMessages.value.slice(end - messagesPerPage, end)
})
const messagePageStart = computed(() => activeConversationMessages.value.length ? (activeMessagePage.value - 1) * messagesPerPage + 1 : 0)
const messagePageEnd = computed(() => Math.min(activeMessagePage.value * messagesPerPage, activeConversationMessages.value.length))
const selectedMessageLog = computed(() => selectedMessage.value?.message || null)
const selectedMessageImages = computed(() => selectedMessage.value?.images || [])
const selectedForwardRefs = computed(() => (selectedMessageLog.value ? forwardRefs(selectedMessageLog.value) : []))
const activeSendTarget = computed(() => resolveActiveSendTarget())
const syncConnectionOptions = computed(() => props.connections || [])

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

function debugArray(path: string): Record<string, unknown>[] {
  const value = view.value?.debug?.[path]
  return Array.isArray(value) ? value.filter(isRecord) : []
}

function debugRecord(path: string): Record<string, unknown> {
  const value = view.value?.debug?.[path]
  return isRecord(value) ? value : {}
}

const debugSessions = computed(() => debugArray('sessions'))
const candidateMemories = computed(() => debugArray('candidate_memories'))
const longTermMemories = computed(() => debugArray('long_term_memories'))
const groupProfiles = computed(() => debugArray('group_profiles'))
const groupObservations = computed(() => debugArray('group_observations'))
const userProfiles = computed(() => debugArray('user_profiles'))
const relationEdges = computed(() => debugArray('relation_edges'))
const reflectionStats = computed(() => debugRecord('reflection_stats'))
const failedAvatarURLs = ref<Record<string, boolean>>({})

const groupPolicyNameMap = computed(() => {
  const result = new Map<string, string>()
  const policies = draft.value.group_policies
  if (!Array.isArray(policies)) return result
  for (const policy of policies) {
    if (!isRecord(policy)) continue
    const groupID = cleanText(policy.group_id)
    const name = cleanText(policy.name)
    if (groupID && name) result.set(groupID, name)
  }
  return result
})

const userProfileNameMap = computed(() => {
  const result = new Map<string, string>()
  for (const profile of userProfiles.value) {
    const groupID = cleanText(profile.group_id)
    const userID = cleanText(profile.user_id)
    const displayName = cleanText(profile.display_name)
    if (groupID && userID && displayName) result.set(`${groupID}:${userID}`, displayName)
  }
  return result
})

watch(
  () => view.value?.config,
  () => resetDraft(),
  { immediate: true, deep: true },
)

watch(
  pagedActiveConversationMessages,
  (messages) => {
    void preloadInlineMessageImages(messages)
  },
  { immediate: true },
)

watch(
  activeConversationMessages,
  () => clampMessagePage(),
)

function resetDraft() {
  draft.value = cloneRecord(view.value?.config)
  localError.value = ''
}

function cleanText(value: unknown): string {
  if (typeof value === 'string') return value.trim()
  if (typeof value === 'number') return String(value)
  return ''
}

function normalizeChatType(value: unknown): 'group' | 'private' | 'other' {
  const chatType = cleanText(value).toLowerCase()
  if (chatType === 'group' || chatType === 'group_chat' || chatType === 'groupchat' || chatType === '群聊') return 'group'
  if (chatType === 'private' || chatType === 'private_chat' || chatType === 'direct' || chatType === '私聊') return 'private'
  return 'other'
}

function chatTypeLabel(value: unknown): string {
  const chatType = normalizeChatType(value)
  if (chatType === 'group') return '群聊'
  if (chatType === 'private') return '私聊'
  return cleanText(value) || '会话'
}

function numericID(value: string): string {
  return /^\d+$/.test(value) ? value : ''
}

function buildQQAvatarURL(value: unknown): string {
  const userID = numericID(cleanText(value))
  if (!userID) return ''
  return `https://q1.qlogo.cn/g?b=qq&nk=${encodeURIComponent(userID)}&s=140`
}

function connectionSnapshot(connectionID: string): ConnectionSnapshot | null {
  return props.connections?.find((item) => item.id === connectionID) || null
}

function botUserID(connectionID: string): string {
  return cleanText(connectionSnapshot(connectionID)?.self_id)
}

function botDisplayName(connectionID: string): string {
  const connection = connectionSnapshot(connectionID)
  return cleanText(connection?.self_nickname) || cleanText(connection?.self_id) || '机器人'
}

function botAvatarURL(connectionID: string): string {
  return buildQQAvatarURL(botUserID(connectionID))
}

function isBotMessage(message: AIMessageLog): boolean {
  const role = cleanText(message.sender_role).toLowerCase()
  return role === 'assistant' || role === 'system' || role === 'bot'
}

function messageSenderID(message: AIMessageLog): string {
  if (isBotMessage(message)) return botUserID(cleanText(message.connection_id)) || cleanText(message.user_id)
  return cleanText(message.user_id)
}

function avatarInitial(value: string): string {
  const text = cleanText(value)
  return text ? text.slice(0, 1).toUpperCase() : '?'
}

function markAvatarFailed(url: string) {
  if (!url || failedAvatarURLs.value[url]) return
  failedAvatarURLs.value = {
    ...failedAvatarURLs.value,
    [url]: true,
  }
}

function displayAvatar(url: string): boolean {
  return !!url && !failedAvatarURLs.value[url]
}

function preferredSenderName(message: AIMessageLog): string {
  const userID = cleanText(message.user_id)
  const groupID = cleanText(message.group_id)
  const groupProfileName = groupID && userID ? userProfileNameMap.value.get(`${groupID}:${userID}`) || '' : ''
  const candidates = [
    cleanText(message.sender_name),
    cleanText(message.sender_nickname),
    groupProfileName,
  ]
  return candidates.find((item) => item && item !== userID) || candidates.find(Boolean) || ''
}

function formatNameWithID(name: unknown, id: unknown, fallback: string): string {
  const displayName = cleanText(name)
  const identity = cleanText(id)
  if (displayName && identity && displayName !== identity) return `${displayName}(${identity})`
  if (displayName) return displayName
  if (identity) return identity
  return fallback
}

function formatSenderDisplayName(message: AIMessageLog): string {
  const userID = messageSenderID(message)
  const senderName = preferredSenderName(message)
  return formatNameWithID(senderName, userID, cleanText(message.sender_role) || '未知')
}

function formatDetailSenderDisplayName(message: AIMessageLog): string {
  const chatType = normalizeChatType(message.chat_type)
  if (chatType !== 'group') return formatSenderDisplayName(message)
  return formatNameWithID(preferredSenderName(message), message.user_id, '未知用户')
}

function resolveGroupName(message: AIMessageLog): string {
  const groupID = cleanText(message.group_id)
  return cleanText(message.group_name) || (groupID ? groupPolicyNameMap.value.get(groupID) || '' : '')
}

function formatGroupDisplayName(message: AIMessageLog): string {
  const groupID = cleanText(message.group_id)
  const groupName = resolveGroupName(message)
  if (groupName && groupID && groupName !== groupID) return `${groupName}(${groupID})`
  if (groupName) return groupName
  if (groupID) return `群聊(${groupID})`
  return '未知群聊'
}

function buildUserAvatarURL(message: AIMessageLog): string {
  const explicitURL = cleanText(message.sender_avatar_url)
  if (explicitURL) return explicitURL
  return buildQQAvatarURL(message.user_id)
}

function buildGroupAvatarURL(message: AIMessageLog): string {
  const explicitURL = cleanText(message.group_avatar_url)
  if (explicitURL) return explicitURL
  const groupID = numericID(cleanText(message.group_id))
  if (!groupID) return ''
  return `https://p.qlogo.cn/gh/${encodeURIComponent(groupID)}/${encodeURIComponent(groupID)}/140`
}

function conversationAvatarURL(message: AIMessageLog): string {
  const chatType = normalizeChatType(message.chat_type)
  if (chatType === 'group') return buildGroupAvatarURL(message)
  if (chatType === 'private') return buildUserAvatarURL(message)
  return buildGroupAvatarURL(message) || buildUserAvatarURL(message)
}

function privateConversationIdentityMessage(messages: AIMessageLog[]): AIMessageLog | null {
  for (let index = messages.length - 1; index >= 0; index -= 1) {
    if (!isBotMessage(messages[index])) return messages[index]
  }
  return messages[messages.length - 1] || null
}

function formatPrivateConversationDisplayName(message: AIMessageLog): string {
  const peerName = isBotMessage(message) ? cleanText(message.sender_nickname) : preferredSenderName(message)
  return formatNameWithID(peerName, cleanText(message.user_id), '私聊')
}

function conversationTitleFromMessages(messages: AIMessageLog[]): string {
  const identity = privateConversationIdentityMessage(messages)
  const fallback = messages[messages.length - 1] || null
  const message = identity || fallback
  if (!message) return '会话'
  const chatType = normalizeChatType(message.chat_type)
  if (chatType === 'group') return formatGroupDisplayName(message)
  if (chatType === 'private') return formatPrivateConversationDisplayName(message)
  if (cleanText(message.group_id)) return formatGroupDisplayName(message)
  if (cleanText(message.user_id) || preferredSenderName(message)) return formatSenderDisplayName(message)
  return cleanText(message.chat_type) || '会话'
}

function conversationAvatarURLFromMessages(messages: AIMessageLog[]): string {
  const identity = privateConversationIdentityMessage(messages)
  const fallback = messages[messages.length - 1] || null
  const message = identity || fallback
  if (!message) return ''
  const chatType = normalizeChatType(message.chat_type)
  if (chatType === 'group') return buildGroupAvatarURL(message)
  if (chatType === 'private') return buildQQAvatarURL(message.user_id) || buildUserAvatarURL(message)
  return conversationAvatarURL(message)
}

function messageAvatarURL(message: AIMessageLog): string {
  if (isBotMessage(message)) return cleanText(message.sender_avatar_url) || botAvatarURL(cleanText(message.connection_id)) || buildUserAvatarURL(message)
  return buildUserAvatarURL(message)
}

function conversationKey(message: AIMessageLog): string {
  const chatType = normalizeChatType(message.chat_type)
  if (chatType === 'group') return `group:${cleanText(message.group_id) || 'unknown'}`
  if (chatType === 'private') return `private:${cleanText(message.user_id) || 'unknown'}`
  return `${cleanText(message.chat_type) || 'chat'}:${cleanText(message.group_id) || cleanText(message.user_id) || 'unknown'}`
}

function conversationTitle(message: AIMessageLog): string {
  const chatType = normalizeChatType(message.chat_type)
  if (chatType === 'group') return formatGroupDisplayName(message)
  if (chatType === 'private') return formatSenderDisplayName(message)
  if (cleanText(message.group_id)) return formatGroupDisplayName(message)
  if (cleanText(message.user_id) || preferredSenderName(message)) return formatSenderDisplayName(message)
  return cleanText(message.chat_type) || '会话'
}

function conversationSubtitle(message: AIMessageLog): string {
  const parts = [chatTypeLabel(message.chat_type), cleanText(message.connection_id)].filter(Boolean)
  return parts.join(' / ') || 'AI 会话'
}

function latestNonEmptyValue(messages: AIMessageLog[], pick: (message: AIMessageLog) => unknown): string {
  for (let index = messages.length - 1; index >= 0; index -= 1) {
    const value = cleanText(pick(messages[index]))
    if (value) return value
  }
  return ''
}

function resolveActiveSendTarget(): ConversationSendTarget | null {
  const conversation = activeConversation.value
  const messages = activeConversationMessages.value
  if (!conversation || !messages.length) return null

  const connectionID = latestNonEmptyValue(messages, (message) => message.connection_id)
  if (!connectionID) return null

  const [kind, rawID = ''] = conversation.key.split(':')
  if (kind === 'group') {
    const groupID = rawID && rawID !== 'unknown' ? rawID : latestNonEmptyValue(messages, (message) => message.group_id)
    if (!groupID) return null
    return {
      connectionID,
      chatType: 'group',
      groupID,
      userID: '',
      label: conversation.title,
    }
  }
  if (kind === 'private') {
    const userID = rawID && rawID !== 'unknown' ? rawID : latestNonEmptyValue(messages, (message) => message.user_id)
    if (!userID) return null
    return {
      connectionID,
      chatType: 'private',
      groupID: '',
      userID,
      label: conversation.title,
    }
  }
  return null
}

function buildConversations(messages: AIMessageLog[]): MessageConversation[] {
  const groups = new Map<string, MessageConversation>()
  for (const message of messages) {
    const key = conversationKey(message)
    const current = groups.get(key)
    if (!current) {
      groups.set(key, {
        key,
        title: conversationTitle(message),
        subtitle: conversationSubtitle(message),
        avatarURL: conversationAvatarURL(message),
        messages: [message],
        unreadImages: message.image_count || 0,
        lastAt: message.occurred_at,
      })
      continue
    }
    current.messages.push(message)
    current.unreadImages += message.image_count || 0
    if (new Date(message.occurred_at).getTime() > new Date(current.lastAt).getTime()) {
      current.lastAt = message.occurred_at
      current.title = conversationTitle(message)
      current.subtitle = conversationSubtitle(message)
      current.avatarURL = conversationAvatarURL(message)
    }
  }
  return [...groups.values()]
    .map((group) => {
      const sortedMessages = [...group.messages].sort((left, right) => new Date(left.occurred_at).getTime() - new Date(right.occurred_at).getTime())
      const latestMessage = sortedMessages[sortedMessages.length - 1] || null
      return {
        ...group,
        title: conversationTitleFromMessages(sortedMessages),
        subtitle: latestMessage ? conversationSubtitle(latestMessage) : group.subtitle,
        avatarURL: conversationAvatarURLFromMessages(sortedMessages),
        messages: sortedMessages,
      }
    })
    .sort((left, right) => new Date(right.lastAt).getTime() - new Date(left.lastAt).getTime())
}

function selectConversation(key: string) {
  selectedConversationKey.value = key
  selectedMessage.value = null
  goToLastMessagePage()
}

function messageBubbleClass(message: AIMessageLog): string {
  const role = String(message.sender_role || '').toLowerCase()
  if (role === 'assistant' || role === 'bot') return 'is-self'
  if (role === 'system') return 'is-system'
  return 'is-peer'
}

function clampMessagePage() {
  messagePage.value = activeMessagePage.value
}

function goToMessagePage(page: number) {
  messagePage.value = Math.min(Math.max(page, 1), messagePageCount.value)
}

function goToLastMessagePage() {
  messagePage.value = messagePageCount.value
}

function isSelectedMessage(message: AIMessageLog): boolean {
  return selectedMessageLog.value?.message_id === message.message_id
}

function normalizeAIMessageDetail(payload: unknown): AIMessageDetail | null {
  if (!isRecord(payload)) return null
  const item = isRecord(payload.item) ? payload.item : payload
  if (!isRecord(item.message)) return null
  return {
    ...item,
    message: item.message as unknown as AIMessageLog,
    images: Array.isArray(item.images) ? (item.images as AIMessageImage[]) : [],
  }
}

function buildSuggestionQueryURL(): string {
  const params = new URLSearchParams()
  params.set('limit', '40')
  if (messageChatType.value.trim()) params.set('chat_type', messageChatType.value.trim())
  if (messageGroupID.value.trim()) params.set('group_id', messageGroupID.value.trim())
  if (messageUserID.value.trim()) params.set('user_id', messageUserID.value.trim())
  return props.runtimeConfig.apiBasePath + '/ai/messages/suggestions?' + params.toString()
}

function buildMessageQueryURL(): string {
  const params = new URLSearchParams()
  params.set('limit', '200')
  if (messageChatType.value.trim()) params.set('chat_type', messageChatType.value.trim())
  if (messageGroupID.value.trim()) params.set('group_id', messageGroupID.value.trim())
  if (messageUserID.value.trim()) params.set('user_id', messageUserID.value.trim())
  if (messageKeyword.value.trim()) params.set('keyword', messageKeyword.value.trim())
  return props.runtimeConfig.apiBasePath + '/ai/messages?' + params.toString()
}

function defaultSyncConnectionID(): string {
  return cleanText(messageConnectionID.value)
    || activeSendTarget.value?.connectionID
    || cleanText(syncConnectionOptions.value[0]?.id)
}

function resolveSyncChatType(): 'group' | 'private' | '' {
  const chatType = normalizeChatType(messageChatType.value)
  if (chatType === 'group' || chatType === 'private') return chatType
  if (messageGroupID.value.trim()) return 'group'
  if (messageUserID.value.trim()) return 'private'
  const target = activeSendTarget.value
  return target?.chatType || ''
}

function buildRecentSyncPayload(): Record<string, unknown> {
  const chatType = resolveSyncChatType()
  const payload: Record<string, unknown> = {
    connection_id: defaultSyncConnectionID(),
    chat_type: chatType,
    count: Math.min(Math.max(Number(messageSyncLimit.value) || 50, 1), 100),
  }
  if (chatType === 'group') {
    payload.group_id = messageGroupID.value.trim() || activeSendTarget.value?.groupID || ''
  } else if (chatType === 'private') {
    payload.user_id = messageUserID.value.trim() || activeSendTarget.value?.userID || ''
  }
  return payload
}

function applySyncFilters(payload: Record<string, unknown>) {
  const chatType = cleanText(payload.chat_type)
  messageConnectionID.value = cleanText(payload.connection_id)
  if (chatType === 'group') {
    messageChatType.value = 'group'
    messageGroupID.value = cleanText(payload.group_id)
    messageUserID.value = ''
    return
  }
  if (chatType === 'private') {
    messageChatType.value = 'private'
    messageGroupID.value = ''
    messageUserID.value = cleanText(payload.user_id)
  }
}

function truncatePreviewText(text: string): string {
  if (text) return text.length > 180 ? text.slice(0, 180) + '...' : text
  return ''
}

function decodeCQValue(value: string): string {
  return value
    .replace(/&#91;/g, '[')
    .replace(/&#93;/g, ']')
    .replace(/&#44;/g, ',')
    .replace(/&amp;/g, '&')
}

function parseCQParams(raw: string): Record<string, string> {
  const result: Record<string, string> = {}
  for (const part of raw.split(',')) {
    const index = part.indexOf('=')
    if (index <= 0) continue
    const key = part.slice(0, index).trim()
    const value = part.slice(index + 1).trim()
    if (key) result[key] = decodeCQValue(value)
  }
  return result
}

function stripCQImageCodes(value: unknown): string {
  return String(value || '').replace(/\[CQ:image,[^\]]*]/g, '').trim()
}

function stripCQPreviewCodes(value: unknown): string {
  return stripCQImageCodes(value).replace(/\[CQ:forward,[^\]]*]/g, '').trim()
}

function forwardRefs(message: AIMessageLog): ForwardMessageRef[] {
  const text = String(message.text_content || '')
  const refs: ForwardMessageRef[] = []
  const seen = new Set<string>()
  for (const match of text.matchAll(/\[CQ:forward,([^\]]*)]/g)) {
    const id = cleanText(parseCQParams(match[1] || '').id)
    if (!id) continue
    const ref = {
      id,
      connectionID: cleanText(message.connection_id),
    }
    const key = forwardCacheKey(ref)
    if (seen.has(key)) continue
    seen.add(key)
    refs.push(ref)
  }
  return refs
}

function hasForwardMessage(message: AIMessageLog): boolean {
  return forwardRefs(message).length > 0
}

function previewText(message: AIMessageLog): string {
  const text = stripCQPreviewCodes(message.text_content)
  if (text) return truncatePreviewText(text)
  if (hasForwardMessage(message)) return '[合并转发消息]'
  if (message.has_image) return `[${message.image_count || 1} 张图片]`
  return '没有文本内容。'
}

function chatBubbleText(message: AIMessageLog): string {
  const text = stripCQPreviewCodes(message.text_content)
  if (text) return truncatePreviewText(text)
  if (message.has_image || hasForwardMessage(message)) return ''
  return '没有文本内容。'
}

function forwardCacheKey(ref: ForwardMessageRef): string {
  return `${ref.connectionID || '-'}:${ref.id}`
}

function cachedForwardMessage(ref: ForwardMessageRef): AIForwardMessage | null {
  return forwardMessageMap.value[forwardCacheKey(ref)] || null
}

function isForwardMessageLoading(ref: ForwardMessageRef): boolean {
  return !!forwardMessageLoading.value[forwardCacheKey(ref)]
}

function forwardMessageLoadError(ref: ForwardMessageRef): string {
  return forwardMessageError.value[forwardCacheKey(ref)] || ''
}

function buildForwardMessageURL(ref: ForwardMessageRef): string {
  const params = new URLSearchParams()
  if (ref.connectionID) params.set('connection_id', ref.connectionID)
  const query = params.toString()
  return `${props.runtimeConfig.apiBasePath}/ai/forward-messages/${encodeURIComponent(ref.id)}${query ? `?${query}` : ''}`
}

async function loadForwardMessage(ref: ForwardMessageRef) {
  const key = forwardCacheKey(ref)
  if (!ref.id || forwardMessageMap.value[key] || forwardMessageLoading.value[key]) return
  forwardMessageLoading.value = {
    ...forwardMessageLoading.value,
    [key]: true,
  }
  const nextErrors = { ...forwardMessageError.value }
  delete nextErrors[key]
  forwardMessageError.value = nextErrors
  try {
    const result = await requestJSON<AIForwardMessage>(buildForwardMessageURL(ref))
    forwardMessageMap.value = {
      ...forwardMessageMap.value,
      [key]: result,
    }
  } catch (error) {
    if (error instanceof APIError && error.status === 401) {
      emit('unauthorized')
      return
    }
    forwardMessageError.value = {
      ...forwardMessageError.value,
      [key]: formatError(error, '加载合并转发消息失败。'),
    }
  } finally {
    const next = { ...forwardMessageLoading.value }
    delete next[key]
    forwardMessageLoading.value = next
  }
}

async function loadForwardMessagesForLog(message: AIMessageLog) {
  await Promise.all(forwardRefs(message).map((ref) => loadForwardMessage(ref)))
}

function segmentData(segment: AIMessageSegment): Record<string, unknown> {
  return isRecord(segment.data) ? segment.data : {}
}

function segmentText(segment: AIMessageSegment): string {
  const data = segmentData(segment)
  const type = cleanText(segment.type).toLowerCase()
  if (type === 'text') return cleanText(data.text)
  if (type === 'at') {
    const qq = cleanText(data.qq)
    return qq ? `@${qq}` : '@'
  }
  return ''
}

function segmentImageURL(segment: AIMessageSegment): string {
  const data = segmentData(segment)
  const type = cleanText(segment.type).toLowerCase()
  if (type !== 'image') return ''
  return cleanText(data.url) || cleanText(data.preview_url)
}

function segmentForwardID(segment: AIMessageSegment): string {
  const data = segmentData(segment)
  const type = cleanText(segment.type).toLowerCase()
  if (type !== 'forward') return ''
  return cleanText(data.id)
}

function segmentLabel(segment: AIMessageSegment): string {
  const type = cleanText(segment.type).toLowerCase()
  if (type === 'image') return '[图片]'
  if (type === 'face') return '[表情]'
  if (type === 'record') return '[语音]'
  if (type === 'video') return '[视频]'
  if (type === 'file') return '[文件]'
  if (type === 'reply') return '[回复]'
  return type ? `[${type}]` : '[消息片段]'
}

function forwardNodeTitle(node: AIForwardMessageNode): string {
  return formatNameWithID(node.nickname, node.user_id, '未知用户')
}

function imagePreviewURL(image: { preview_url?: string; message_id: string; segment_index: number }): string {
  if (image.preview_url) return image.preview_url
  return `${props.runtimeConfig.apiBasePath}/ai/messages/${encodeURIComponent(image.message_id)}/images/${image.segment_index}/content`
}

function inlineImages(message: AIMessageLog): AIMessageImage[] {
  return inlineImageMap.value[message.message_id] || []
}

function isInlineImageLoading(message: AIMessageLog): boolean {
  return !!inlineImageLoading.value[message.message_id]
}

function inlineImageAlt(image: AIMessageImage, index: number): string {
  return image.file_name || `message-image-${index + 1}`
}

async function preloadInlineMessageImages(messages: AIMessageLog[]) {
  const targets = messages
    .filter((message) => message.has_image && message.message_id)
    .filter((message) => !inlineImageMap.value[message.message_id] && !inlineImageLoading.value[message.message_id])
    .slice(0, 24)

  await Promise.all(targets.map((message) => loadInlineMessageImages(message.message_id)))
}

async function loadInlineMessageImages(messageID: string) {
  if (!messageID || inlineImageLoading.value[messageID] || inlineImageMap.value[messageID]) return
  inlineImageLoading.value = {
    ...inlineImageLoading.value,
    [messageID]: true,
  }
  try {
    const detail = await requestJSON<unknown>(
      props.runtimeConfig.apiBasePath + '/ai/messages/' + encodeURIComponent(messageID),
    )
    const normalized = normalizeAIMessageDetail(detail)
    if (!normalized) return
    inlineImageMap.value = {
      ...inlineImageMap.value,
      [messageID]: normalized.images || [],
    }
  } catch (error) {
    inlineImageMap.value = {
      ...inlineImageMap.value,
      [messageID]: [],
    }
    if (error instanceof APIError && error.status === 401) {
      emit('unauthorized')
    }
  } finally {
    const next = { ...inlineImageLoading.value }
    delete next[messageID]
    inlineImageLoading.value = next
  }
}

function formatBytes(value: number | undefined): string {
  const size = Number(value || 0)
  if (!Number.isFinite(size) || size <= 0) return '-'
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KiB`
  return `${(size / 1024 / 1024).toFixed(1)} MiB`
}

function formatDateTime(value: string | undefined): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function formatError(error: unknown, fallback: string): string {
  if (error instanceof APIError) return error.message || fallback
  if (error instanceof Error) return error.message
  return fallback
}

function applyMemoryActionResult(result: AIMemoryActionResult) {
  if (result.view) {
    view.value = result.view
  }
  emit('notice', {
    kind: 'success',
    title: 'AI 记忆已更新',
    text: result.message || 'AI 记忆状态已更新。',
  })
}

async function runReflection() {
  if (reflectionBusy.value || props.busy) return
  reflectionBusy.value = true
  emit('busy', true)
  try {
    const result = await requestJSON<AIMemoryActionResult>(props.runtimeConfig.apiBasePath + '/ai/reflection/run', {
      method: 'POST',
    })
    applyMemoryActionResult(result)
  } catch (error) {
    if (error instanceof APIError && error.status === 401) {
      emit('unauthorized')
      return
    }
    emit('notice', {
      kind: 'error',
      title: '反思执行失败',
      text: formatError(error, '执行 AI 反思失败。'),
    })
  } finally {
    reflectionBusy.value = false
    emit('busy', false)
  }
}

async function runCandidateAction(id: string, action: 'promote' | 'delete') {
  if (!id || memoryActionBusy.value || props.busy) return
  memoryActionBusy.value = `candidate:${id}:${action}`
  emit('busy', true)
  try {
    const result = await requestJSON<AIMemoryActionResult>(
      `${props.runtimeConfig.apiBasePath}/ai/candidates/${encodeURIComponent(id)}/${action}`,
      { method: 'POST' },
    )
    applyMemoryActionResult(result)
  } catch (error) {
    if (error instanceof APIError && error.status === 401) {
      emit('unauthorized')
      return
    }
    emit('notice', {
      kind: 'error',
      title: '候选记忆操作失败',
      text: formatError(error, '更新候选记忆失败。'),
    })
  } finally {
    memoryActionBusy.value = ''
    emit('busy', false)
  }
}

async function deleteLongTermMemory(id: string) {
  if (!id || memoryActionBusy.value || props.busy) return
  memoryActionBusy.value = `long-term:${id}:delete`
  emit('busy', true)
  try {
    const result = await requestJSON<AIMemoryActionResult>(
      `${props.runtimeConfig.apiBasePath}/ai/long-term/${encodeURIComponent(id)}/delete`,
      { method: 'POST' },
    )
    applyMemoryActionResult(result)
  } catch (error) {
    if (error instanceof APIError && error.status === 401) {
      emit('unauthorized')
      return
    }
    emit('notice', {
      kind: 'error',
      title: '长期记忆操作失败',
      text: formatError(error, '更新长期记忆失败。'),
    })
  } finally {
    memoryActionBusy.value = ''
    emit('busy', false)
  }
}

async function saveAIConfig() {
  if (loading.value || props.busy || !configLoaded.value) return
  localError.value = ''
  loading.value = true
  emit('busy', true)
  try {
    const result = await requestJSON<AIConfigSaveResult>(props.runtimeConfig.apiBasePath + '/ai/save', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(draft.value),
    })
    if (result.view) {
      view.value = result.view
    }
    emit('notice', {
      kind: 'success',
      title: 'AI 配置已保存',
      text: result.message || 'AI 配置已保存。',
    })
  } catch (error) {
    if (error instanceof APIError && error.status === 401) {
      emit('unauthorized')
      return
    }
    const message = formatError(error, '保存 AI 配置失败。')
    localError.value = message
    emit('notice', {
      kind: 'error',
      title: 'AI 配置保存失败',
      text: message,
    })
  } finally {
    loading.value = false
    emit('busy', false)
  }
}

async function loadMessageSuggestions() {
  try {
    messageSuggestions.value = await requestJSON<AIMessageSuggestions>(buildSuggestionQueryURL())
  } catch (error) {
    if (error instanceof APIError && error.status === 401) {
      emit('unauthorized')
    }
  }
}

function applyAIMessageLogResponse(response: AIMessageLogResponse) {
  messageLogs.value = response.items || []
  if (selectedMessageLog.value && !messageLogs.value.some((item) => item.message_id === selectedMessageLog.value?.message_id)) {
    selectedMessage.value = null
  }
  if (!selectedConversationKey.value || !conversations.value.some((item) => item.key === selectedConversationKey.value)) {
    selectedConversationKey.value = conversations.value[0]?.key || ''
  }
  goToLastMessagePage()
}

async function loadAIMessageLogs() {
  if (messagesLoading.value || props.busy) return
  messagesLoading.value = true
  messageError.value = ''
  emit('busy', true)
  try {
    await loadMessageSuggestions()
    const response = await requestJSON<AIMessageLogResponse>(buildMessageQueryURL())
    applyAIMessageLogResponse(response)
  } catch (error) {
    if (error instanceof APIError && error.status === 401) {
      emit('unauthorized')
      return
    }
    const message = formatError(error, '加载 AI 聊天记录失败。')
    messageError.value = message
    emit('notice', { kind: 'error', title: '聊天记录加载失败', text: message })
  } finally {
    messagesLoading.value = false
    emit('busy', false)
  }
}

async function syncAIRecentMessages() {
  if (syncMessagesLoading.value || messagesLoading.value || props.busy) return
  const payload = buildRecentSyncPayload()
  if (!cleanText(payload.connection_id)) {
    messageError.value = '请先选择一个可用连接。'
    return
  }
  if (payload.chat_type !== 'group' && payload.chat_type !== 'private') {
    messageError.value = '请先选择群聊或私聊，或填写群 ID / 用户 ID。'
    return
  }
  if (payload.chat_type === 'group' && !cleanText(payload.group_id)) {
    messageError.value = '同步群聊最近消息需要填写群 ID。'
    return
  }
  if (payload.chat_type === 'private' && !cleanText(payload.user_id)) {
    messageError.value = '同步私聊最近消息需要填写用户 ID。'
    return
  }

  syncMessagesLoading.value = true
  messageError.value = ''
  emit('busy', true)
  try {
    const result = await requestJSON<AIRecentMessagesSyncResult>(props.runtimeConfig.apiBasePath + '/ai/messages/sync', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    })
    emit('notice', {
      kind: 'success',
      title: '最近消息已同步',
      text: result.message || `读取 ${result.fetched ?? 0} 条，写入 ${result.synced ?? 0} 条最近消息。`,
    })
    applySyncFilters(payload)
    await refreshConversationPresentationAfterSync()
  } catch (error) {
    if (error instanceof APIError && error.status === 401) {
      emit('unauthorized')
      return
    }
    const message = formatError(error, '同步最近消息失败。')
    messageError.value = message
    emit('notice', { kind: 'error', title: '最近消息同步失败', text: message })
  } finally {
    syncMessagesLoading.value = false
    emit('busy', false)
  }
}

async function syncAllAIRecentMessages(chatType: 'group' | 'private') {
  if (syncMessagesLoading.value || messagesLoading.value || props.busy) return
  const connectionID = defaultSyncConnectionID()
  if (!connectionID) {
    messageError.value = '请先选择一个可用连接。'
    return
  }

  const payload = {
    connection_id: connectionID,
    chat_type: chatType,
    count: Math.min(Math.max(Number(messageSyncLimit.value) || 50, 1), 100),
  }

  syncMessagesLoading.value = true
  bulkSyncKind.value = chatType
  messageError.value = ''
  emit('busy', true)
  try {
    const result = await requestJSON<AIRecentMessagesBulkSyncResult>(props.runtimeConfig.apiBasePath + '/ai/messages/sync-all', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    })
    emit('notice', {
      kind: (result.failed ?? 0) > 0 ? 'info' : 'success',
      title: chatType === 'group' ? '群聊消息已同步' : '私聊消息已同步',
      text: result.message || `同步 ${result.targets ?? 0} 个会话，读取 ${result.fetched ?? 0} 条，写入 ${result.synced ?? 0} 条最近消息。`,
    })
    messageConnectionID.value = connectionID
    messageChatType.value = chatType
    messageGroupID.value = ''
    messageUserID.value = ''
    messagePage.value = 1
    await refreshConversationPresentationAfterSync()
  } catch (error) {
    if (error instanceof APIError && error.status === 401) {
      emit('unauthorized')
      return
    }
    const message = formatError(error, chatType === 'group' ? '同步所有群聊消息失败。' : '同步所有私聊消息失败。')
    messageError.value = message
    emit('notice', { kind: 'error', title: '批量同步失败', text: message })
  } finally {
    bulkSyncKind.value = ''
    syncMessagesLoading.value = false
    emit('busy', false)
  }
}

async function loadAIMessageDetail(messageID: string) {
  if (!messageID || detailLoading.value || props.busy) return
  detailLoading.value = true
  messageError.value = ''
  emit('busy', true)
  try {
    const detail = await requestJSON<unknown>(
      props.runtimeConfig.apiBasePath + '/ai/messages/' + encodeURIComponent(messageID),
    )
    const normalized = normalizeAIMessageDetail(detail)
    if (!normalized) {
      throw new Error('Invalid AI message detail response.')
    }
    selectedMessage.value = normalized
    inlineImageMap.value = {
      ...inlineImageMap.value,
      [messageID]: normalized.images || [],
    }
    void loadForwardMessagesForLog(normalized.message)
  } catch (error) {
    if (error instanceof APIError && error.status === 401) {
      emit('unauthorized')
      return
    }
    const message = formatError(error, '加载消息详情失败。')
    messageError.value = message
    emit('notice', { kind: 'error', title: '消息详情加载失败', text: message })
  } finally {
    detailLoading.value = false
    emit('busy', false)
  }
}

async function refreshAIView() {
  view.value = await requestJSON<AIView>(props.runtimeConfig.apiBasePath + '/ai')
}

function handleSkillsViewUpdated(nextView: AIView) {
  view.value = nextView
}

function handleSkillConfigUpdated(nextConfig: Record<string, unknown>) {
  draft.value = nextConfig
}

async function refreshAIViewAfterMessageSync() {
  try {
    await refreshAIView()
  } catch (error) {
    if (error instanceof APIError && error.status === 401) {
      emit('unauthorized')
      return
    }
    emit('notice', {
      kind: 'info',
      title: 'AI 状态未刷新',
      text: formatError(error, '最近消息已同步，但 AI 记忆状态刷新失败。'),
    })
  }
}

async function refreshSelectedMessageDetailAfterSync() {
  const messageID = cleanText(selectedMessageLog.value?.message_id)
  if (!messageID) return
  try {
    const detail = await requestJSON<unknown>(
      props.runtimeConfig.apiBasePath + '/ai/messages/' + encodeURIComponent(messageID),
    )
    const normalized = normalizeAIMessageDetail(detail)
    if (!normalized) return
    selectedMessage.value = normalized
    inlineImageMap.value = {
      ...inlineImageMap.value,
      [messageID]: normalized.images || [],
    }
    void loadForwardMessagesForLog(normalized.message)
  } catch (error) {
    if (error instanceof APIError && error.status === 401) {
      emit('unauthorized')
    }
  }
}

async function refreshConversationPresentationAfterSync() {
  failedAvatarURLs.value = {}
  await refreshAIViewAfterMessageSync()
  await loadMessageSuggestions()
  applyAIMessageLogResponse(await requestJSON<AIMessageLogResponse>(buildMessageQueryURL()))
  await refreshSelectedMessageDetailAfterSync()
}

async function loadAIView() {
  if (loading.value || props.busy) return
  loading.value = true
  emit('busy', true)
  try {
    await refreshAIView()
  } catch (error) {
    if (error instanceof APIError && error.status === 401) {
      emit('unauthorized')
      return
    }
    emit('notice', {
      kind: 'error',
      title: 'AI 状态加载失败',
      text: formatError(error, '加载 AI 状态失败。'),
    })
  } finally {
    loading.value = false
    emit('busy', false)
  }
}

function appendSentMessage(target: ConversationSendTarget, text: string) {
  const now = new Date().toISOString()
  const messageID = `admin-${Date.now()}-${Math.random().toString(16).slice(2)}`
  const botID = botUserID(target.connectionID)
  const botName = botDisplayName(target.connectionID)
  messageLogs.value = [
    {
      message_id: messageID,
      connection_id: target.connectionID,
      chat_type: target.chatType,
      group_id: target.groupID,
      user_id: target.chatType === 'group' ? botID : target.userID,
      sender_role: 'assistant',
      sender_name: botName,
      sender_avatar_url: botAvatarURL(target.connectionID),
      text_content: text,
      has_text: true,
      has_image: false,
      image_count: 0,
      message_status: 'sent',
      occurred_at: now,
    },
    ...messageLogs.value,
  ]
  selectedConversationKey.value = target.chatType === 'group' ? `group:${target.groupID}` : `private:${target.userID}`
  goToLastMessagePage()
}

async function sendActiveConversationMessage() {
  const target = activeSendTarget.value
  const text = sendDraft.value.trim()
  if (!target || !text || sendLoading.value || props.busy) return

  sendLoading.value = true
  sendError.value = ''
  emit('busy', true)
  try {
    await requestJSON<AIMessageSendResult>(props.runtimeConfig.apiBasePath + '/ai/messages/send', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        connection_id: target.connectionID,
        chat_type: target.chatType,
        group_id: target.groupID,
        user_id: target.userID,
        text,
      }),
    })
    appendSentMessage(target, text)
    sendDraft.value = ''
    emit('notice', { kind: 'success', title: '消息已发送', text: `已发送到 ${target.label}` })
  } catch (error) {
    if (error instanceof APIError && error.status === 401) {
      emit('unauthorized')
      return
    }
    const message = formatError(error, '发送消息失败。')
    sendError.value = message
    emit('notice', { kind: 'error', title: '发送消息失败', text: message })
  } finally {
    sendLoading.value = false
    emit('busy', false)
  }
}

onMounted(async () => {
  await loadAIView()
  await loadAIMessageLogs()
})
</script>

<template>
  <section class="ai-panel">
    <section class="summary-grid compact-summary ai-summary-grid">
      <article class="card stat-card"><span class="stat-label">AI 状态</span><strong class="stat-value text-stat">{{ snapshot?.state || '未知' }}</strong><p class="stat-note">{{ snapshot?.ready ? '等待下一次消息决策' : '服务未就绪' }}</p></article>
      <article class="card stat-card"><span class="stat-label">会话窗口</span><strong class="stat-value text-stat">{{ snapshot?.session_count ?? 0 }}</strong><p class="stat-note">最近活跃会话数量。</p></article>
      <article class="card stat-card"><span class="stat-label">候选记忆</span><strong class="stat-value text-stat">{{ snapshot?.candidate_count ?? 0 }}</strong><p class="stat-note">候选记忆池。</p></article>
      <article class="card stat-card"><span class="stat-label">长期记忆</span><strong class="stat-value text-stat">{{ snapshot?.long_term_count ?? 0 }}</strong><p class="stat-note">群策略 {{ snapshot?.group_profile_count ?? 0 }} 条。</p></article>
    </section>

    <section class="card ai-master-card">
      <div class="ai-master-head">
        <div>
          <span class="eyebrow">AI 控制台</span>
          <h3>模型状态、记忆与最近对话</h3>
          <p>统一查看 AI 运行状态、配置、记忆池和聊天记录。</p>
        </div>
        <div class="action-row">
          <button class="secondary-btn" type="button" :disabled="loading || props.busy" @click="resetDraft">恢复当前配置</button>
          <button class="secondary-btn" type="button" :disabled="loading || props.busy" @click="loadAIView">{{ loading ? '加载中...' : '刷新状态' }}</button>
          <button class="primary-btn" type="button" :disabled="loading || props.busy || !configLoaded" @click="saveAIConfig">保存 AI 配置</button>
        </div>
      </div>

      <nav class="ai-pill-tabs">
        <button class="ai-pill-btn" :class="{ active: activeTab === 'logs' }" type="button" @click="activeTab = 'logs'">最近对话</button>
        <button class="ai-pill-btn" :class="{ active: activeTab === 'runtime' }" type="button" @click="activeTab = 'runtime'">运行状态</button>
        <button class="ai-pill-btn" :class="{ active: activeTab === 'base' }" type="button" @click="activeTab = 'base'">基础与回复</button>
        <button class="ai-pill-btn" :class="{ active: activeTab === 'model' }" type="button" @click="activeTab = 'model'">模型与视觉</button>
        <button class="ai-pill-btn" :class="{ active: activeTab === 'memory' }" type="button" @click="activeTab = 'memory'">记忆策略</button>
        <button class="ai-pill-btn" :class="{ active: activeTab === 'skills' }" type="button" @click="activeTab = 'skills'">技能</button>
        <button class="ai-pill-btn" :class="{ active: activeTab === 'mcp' }" type="button" @click="activeTab = 'mcp'">MCP</button>
        <button class="ai-pill-btn" :class="{ active: activeTab === 'persona' }" type="button" @click="activeTab = 'persona'">人格管理</button>
        <button class="ai-pill-btn" :class="{ active: activeTab === 'group' }" type="button" @click="activeTab = 'group'">群策略画像</button>
      </nav>

      <div v-show="activeTab === 'runtime'" class="ai-tab-body">
        <section v-if="localError" class="banner banner-danger"><strong>AI 配置错误</strong><span>{{ localError }}</span></section>
        <section class="detail-grid">
          <article class="subcard"><h4>模型服务</h4><dl class="detail-list"><div><dt>启用</dt><dd>{{ snapshot?.enabled ? '是' : '否' }}</dd></div><div><dt>服务方</dt><dd>{{ snapshot?.provider_vendor || snapshot?.provider_kind || '-' }}</dd></div><div><dt>模型</dt><dd>{{ snapshot?.model || '-' }}</dd></div><div><dt>存储</dt><dd>{{ snapshot?.store_engine || '-' }} / {{ snapshot?.store_ready ? '就绪' : '未就绪' }}</dd></div></dl></article>
          <article class="subcard"><h4>视觉</h4><dl class="detail-list"><div><dt>启用</dt><dd>{{ snapshot?.vision_enabled ? '是' : '否' }}</dd></div><div><dt>模式</dt><dd>{{ snapshot?.vision_mode || '-' }}</dd></div><div><dt>服务方</dt><dd>{{ snapshot?.vision_provider || '-' }}</dd></div><div><dt>模型</dt><dd>{{ snapshot?.vision_model || '-' }}</dd></div></dl></article>
          <article class="subcard"><h4>记忆关系</h4><dl class="detail-list"><div><dt>群策略</dt><dd>{{ snapshot?.group_profile_count ?? 0 }}</dd></div><div><dt>用户</dt><dd>{{ snapshot?.user_profile_count ?? 0 }}</dd></div><div><dt>关系</dt><dd>{{ snapshot?.relation_edge_count ?? 0 }}</dd></div><div><dt>人格</dt><dd>{{ snapshot?.private_persona_count ?? 0 }}</dd></div></dl></article>
          <article class="subcard"><h4>最近活动</h4><dl class="detail-list"><div><dt>最近回复</dt><dd>{{ formatDateTime(snapshot?.last_reply_at) }}</dd></div><div><dt>最近视觉</dt><dd>{{ formatDateTime(snapshot?.last_vision_at) }}</dd></div><div><dt>最近反思</dt><dd>{{ formatDateTime(snapshot?.last_reflection_at) }}</dd></div><div><dt>执行反思</dt><dd>{{ snapshot?.reflection_running ? '是' : '否' }}</dd></div></dl></article>
        </section>
        <section v-if="snapshot?.last_error || snapshot?.last_vision_error || snapshot?.last_reflection_error" class="banner banner-danger"><strong>AI 诊断信息</strong><span>{{ snapshot?.last_error || snapshot?.last_vision_error || snapshot?.last_reflection_error }}</span></section>
        <section class="subcard"><h4>最近决策</h4><p class="inline-note">{{ snapshot?.last_decision_reason || '暂时还没有记录到决策原因。' }}</p></section>
      </div>

      <div v-show="activeTab === 'base'" class="ai-tab-body"><AIConfig v-model:config="draft" :configLoaded="configLoaded" :busy="props.busy" :apiBasePath="props.runtimeConfig.apiBasePath" activeSection="base" @unauthorized="emit('unauthorized')" @notice="emit('notice', $event)" /></div>
      <div v-show="activeTab === 'model'" class="ai-tab-body"><AIConfig v-model:config="draft" :configLoaded="configLoaded" :busy="props.busy" :apiBasePath="props.runtimeConfig.apiBasePath" activeSection="model" @unauthorized="emit('unauthorized')" @notice="emit('notice', $event)" /></div>
      <div v-show="activeTab === 'memory'" class="ai-tab-body">
        <AIMemoryGrid
          :debug-sessions="debugSessions"
          :candidate-memories="candidateMemories"
          :long-term-memories="longTermMemories"
          :group-profiles="groupProfiles"
          :group-observations="groupObservations"
          :user-profiles="userProfiles"
          :relation-edges="relationEdges"
          :reflection-stats="reflectionStats"
          :last-reflection-at="snapshot?.last_reflection_at"
          :last-reflection-error="snapshot?.last_reflection_error"
          :api-base-path="props.runtimeConfig.apiBasePath"
          :busy="props.busy"
          :memory-action-busy="memoryActionBusy"
          :reflection-busy="reflectionBusy"
          @run-reflection="runReflection"
          @run-candidate-action="runCandidateAction"
          @delete-long-term-memory="deleteLongTermMemory"
        />
      </div>
      <div v-show="activeTab === 'skills'" class="ai-tab-body">
        <AISkillsPanel
          :view="view"
          :config="draft"
          :configLoaded="configLoaded"
          :busy="props.busy"
          :apiBasePath="props.runtimeConfig.apiBasePath"
          @update:config="handleSkillConfigUpdated"
          @replace-view="handleSkillsViewUpdated"
          @unauthorized="emit('unauthorized')"
          @notice="emit('notice', $event)"
        />
      </div>
      <div v-show="activeTab === 'mcp'" class="ai-tab-body">
        <AIMCPPanel
          :view="view"
          :config="draft"
          :configLoaded="configLoaded"
          :busy="props.busy"
          @update:config="handleSkillConfigUpdated"
          @notice="emit('notice', $event)"
        />
      </div>
      <div v-show="activeTab === 'persona'" class="ai-tab-body"><AIConfig v-model:config="draft" :configLoaded="configLoaded" :busy="props.busy" :apiBasePath="props.runtimeConfig.apiBasePath" activeSection="persona" @unauthorized="emit('unauthorized')" @notice="emit('notice', $event)" /></div>
      <div v-show="activeTab === 'group'" class="ai-tab-body"><AIConfig v-model:config="draft" :configLoaded="configLoaded" :busy="props.busy" :apiBasePath="props.runtimeConfig.apiBasePath" activeSection="group" @unauthorized="emit('unauthorized')" @notice="emit('notice', $event)" /></div>

      <div v-show="activeTab === 'logs'" class="ai-tab-body">
        <div class="filter-row ai-message-filter-row">
          <select v-model="messageConnectionID" class="text-control small-control">
            <option value="">默认连接</option>
            <option v-for="connection in syncConnectionOptions" :key="connection.id" :value="connection.id">{{ connection.id }}</option>
          </select>
          <select v-model="messageChatType" class="text-control small-control" @change="loadAIMessageLogs"><option value="">全部会话</option><option value="group">群聊</option><option value="private">私聊</option></select>
          <input v-model="messageGroupID" class="text-control small-control" type="text" list="ai-message-group-suggestions" placeholder="群 ID" @keyup.enter="loadAIMessageLogs" />
          <input v-model="messageUserID" class="text-control small-control" type="text" list="ai-message-user-suggestions" placeholder="用户 ID" @keyup.enter="loadAIMessageLogs" />
          <datalist id="ai-message-group-suggestions"><option v-for="group in messageSuggestions.groups || []" :key="group" :value="group"></option></datalist>
          <datalist id="ai-message-user-suggestions"><option v-for="user in messageSuggestions.users || []" :key="user" :value="user"></option></datalist>
          <input v-model="messageKeyword" class="search-input" type="search" placeholder="关键词" @keyup.enter="loadAIMessageLogs" />
          <select v-model="messageSyncLimit" class="text-control small-control" title="每次同步数量"><option :value="20">同步 20 条</option><option :value="50">同步 50 条</option><option :value="100">同步 100 条</option></select>
          <button class="secondary-btn" type="button" :disabled="syncMessagesLoading || messagesLoading || props.busy" @click="syncAIRecentMessages">{{ syncMessagesLoading ? '同步中...' : '同步最近消息' }}</button>
          <button class="secondary-btn" type="button" :disabled="syncMessagesLoading || messagesLoading || props.busy" @click="syncAllAIRecentMessages('group')">{{ bulkSyncKind === 'group' ? '同步群聊中...' : '同步所有群聊消息' }}</button>
          <button class="secondary-btn" type="button" :disabled="syncMessagesLoading || messagesLoading || props.busy" @click="syncAllAIRecentMessages('private')">{{ bulkSyncKind === 'private' ? '同步私聊中...' : '同步所有私聊消息' }}</button>
          <button class="primary-btn" type="button" :disabled="messagesLoading || props.busy" @click="loadAIMessageLogs">{{ messagesLoading ? '加载中...' : '刷新消息' }}</button>
        </div>
        <div v-if="messageError" class="banner banner-danger"><strong>消息加载失败</strong><span>{{ messageError }}</span></div>

        <div class="ai-chat-shell">
          <aside class="ai-pane ai-conversation-pane">
            <div class="ai-pane-header">
              <div>
                <h4>会话列表</h4>
                <p>按最近消息排序。</p>
              </div>
            </div>
            <div v-if="!conversations.length" class="empty-state compact">没有匹配的会话。</div>
            <button v-for="conversation in conversations" v-else :key="conversation.key" class="ai-conversation-item" :class="{ active: activeConversation?.key === conversation.key }" type="button" @click="selectConversation(conversation.key)">
              <span class="ai-conversation-avatar" :class="{ 'has-image': displayAvatar(conversation.avatarURL) }">
                <img v-if="displayAvatar(conversation.avatarURL)" class="ai-avatar-image" :src="conversation.avatarURL" :alt="conversation.title" loading="lazy" referrerpolicy="no-referrer" @error="markAvatarFailed(conversation.avatarURL)" />
                <template v-else>{{ avatarInitial(conversation.title) }}</template>
              </span>
              <div class="ai-conversation-copy">
                <div class="ai-conversation-head"><strong>{{ conversation.title }}</strong><time>{{ formatDateTime(conversation.lastAt) }}</time></div>
                <p>{{ previewText(conversation.messages[conversation.messages.length - 1]) }}</p>
                <span>{{ conversation.subtitle }}</span>
              </div>
              <span v-if="conversation.unreadImages" class="ai-conversation-badge">{{ conversation.unreadImages }}</span>
            </button>
          </aside>

          <section class="ai-pane ai-chat-thread-pane">
            <div class="ai-pane-header">
              <div>
                <h4>{{ activeConversation?.title || '选择一个会话' }}</h4>
                <p>{{ activeConversation?.subtitle || '会话详情' }}</p>
              </div>
              <span class="muted">{{ activeConversationMessages.length }} 条消息</span>
            </div>

            <div v-if="activeConversationMessages.length" class="ai-message-pager">
              <span>第 {{ activeMessagePage }} / {{ messagePageCount }} 页</span>
              <span>{{ messagePageStart }}-{{ messagePageEnd }} / {{ activeConversationMessages.length }}</span>
              <div class="ai-message-pager-actions">
                <button class="secondary-btn slim-btn" type="button" :disabled="activeMessagePage <= 1" @click="goToMessagePage(1)">首页</button>
                <button class="secondary-btn slim-btn" type="button" :disabled="activeMessagePage <= 1" @click="goToMessagePage(activeMessagePage - 1)">上一页</button>
                <button class="secondary-btn slim-btn" type="button" :disabled="activeMessagePage >= messagePageCount" @click="goToMessagePage(activeMessagePage + 1)">下一页</button>
                <button class="secondary-btn slim-btn" type="button" :disabled="activeMessagePage >= messagePageCount" @click="goToLastMessagePage">末页</button>
              </div>
            </div>

            <div class="ai-chat-scroll">
              <div v-if="!activeConversationMessages.length" class="empty-state compact">这个会话里还没有消息。</div>
              <template v-else>
                <article v-for="message in pagedActiveConversationMessages" :key="message.message_id" class="ai-chat-message" :class="[messageBubbleClass(message), { selected: isSelectedMessage(message) }]">
                  <span class="ai-chat-avatar" :class="{ 'has-image': displayAvatar(messageAvatarURL(message)) }">
                    <img v-if="displayAvatar(messageAvatarURL(message))" class="ai-avatar-image" :src="messageAvatarURL(message)" :alt="formatSenderDisplayName(message)" loading="lazy" referrerpolicy="no-referrer" @error="markAvatarFailed(messageAvatarURL(message))" />
                    <template v-else>{{ avatarInitial(formatSenderDisplayName(message)) }}</template>
                  </span>
                  <div class="ai-chat-bubble-shell">
                    <div class="ai-chat-message-meta"><strong>{{ formatSenderDisplayName(message) }}</strong><span v-if="message.sender_role" class="ai-dark-badge">{{ message.sender_role }}</span><time>{{ formatDateTime(message.occurred_at) }}</time></div>
                    <button class="ai-chat-bubble" type="button" @click="loadAIMessageDetail(message.message_id)">
                      <span v-if="chatBubbleText(message)">{{ chatBubbleText(message) }}</span>
                      <span v-if="message.has_image && isInlineImageLoading(message)" class="ai-inline-image-loading">图片加载中...</span>
                      <span v-if="inlineImages(message).length" class="ai-inline-image-grid">
                        <img
                          v-for="(image, index) in inlineImages(message)"
                          :key="image.id || `${message.message_id}-${image.segment_index}`"
                          class="ai-inline-image"
                          :src="imagePreviewURL(image)"
                          :alt="inlineImageAlt(image, index)"
                          loading="lazy"
                        />
                      </span>
                      <small v-else-if="message.has_image && !isInlineImageLoading(message)">{{ message.image_count || 1 }} 张图片</small>
                      <span v-if="forwardRefs(message).length" class="ai-forward-chip-list">
                        <span v-for="ref in forwardRefs(message)" :key="forwardCacheKey(ref)" class="ai-forward-chip">
                          <strong>合并转发消息</strong>
                          <small>{{ ref.id }}</small>
                        </span>
                      </span>
                    </button>
                  </div>
                </article>
              </template>
            </div>

            <form v-if="activeSendTarget" class="ai-message-compose" @submit.prevent="sendActiveConversationMessage">
              <div class="ai-message-compose-target">
                <strong>发送到 {{ activeSendTarget.label }}</strong>
                <span>{{ activeSendTarget.connectionID }} / {{ chatTypeLabel(activeSendTarget.chatType) }}</span>
              </div>
              <div class="ai-message-compose-row">
                <textarea v-model="sendDraft" class="text-control ai-message-compose-input" rows="2" placeholder="输入要发送的消息，Enter 发送，Shift+Enter 换行" :disabled="sendLoading || props.busy" @keydown.enter.exact.prevent="sendActiveConversationMessage"></textarea>
                <button class="primary-btn ai-message-send-btn" type="submit" :disabled="sendLoading || props.busy || !sendDraft.trim()">{{ sendLoading ? '发送中...' : '发送' }}</button>
              </div>
              <p v-if="sendError" class="error-copy">{{ sendError }}</p>
            </form>
          </section>

          <aside class="ai-pane ai-inspector-pane">
            <div class="ai-pane-header">
              <div>
                <h4>{{ selectedMessageLog ? '消息详情' : '消息摘要' }}</h4>
                <p>{{ selectedMessageLog ? '当前选中消息的完整内容。' : '点击中间消息后在这里查看详情。' }}</p>
              </div>
              <span v-if="detailLoading" class="muted">加载中...</span>
            </div>

            <template v-if="selectedMessageLog">
              <dl class="detail-list compact-detail-list">
                <div><dt>连接</dt><dd>{{ selectedMessageLog.connection_id || '-' }}</dd></div>
                <div><dt>会话类型</dt><dd>{{ chatTypeLabel(selectedMessageLog.chat_type) }}</dd></div>
                <div><dt>群聊</dt><dd>{{ normalizeChatType(selectedMessageLog.chat_type) === 'group' ? formatGroupDisplayName(selectedMessageLog) : '-' }}</dd></div>
                <div><dt>用户</dt><dd>{{ formatDetailSenderDisplayName(selectedMessageLog) }}</dd></div>
                <div><dt>状态</dt><dd>{{ selectedMessageLog.message_status || '-' }}</dd></div>
                <div><dt>发生时间</dt><dd>{{ formatDateTime(selectedMessageLog.occurred_at) }}</dd></div>
              </dl>
              <article class="ai-message-text-block">{{ selectedMessageLog.text_content || '没有文本内容。' }}</article>
              <section v-if="selectedForwardRefs.length" class="ai-forward-detail-section">
                <div class="ai-forward-section-head">
                  <h4>合并转发</h4>
                  <span>{{ selectedForwardRefs.length }} 条引用</span>
                </div>
                <article v-for="ref in selectedForwardRefs" :key="forwardCacheKey(ref)" class="ai-forward-detail-card">
                  <div class="ai-forward-detail-head">
                    <div>
                      <strong>合并转发消息</strong>
                      <p>{{ ref.id }}</p>
                    </div>
                    <span v-if="cachedForwardMessage(ref)" class="ai-dark-badge">已缓存</span>
                    <span v-else-if="isForwardMessageLoading(ref)" class="muted">加载中...</span>
                  </div>
                  <p v-if="forwardMessageLoadError(ref)" class="error-copy">{{ forwardMessageLoadError(ref) }}</p>
                  <div v-else-if="cachedForwardMessage(ref)" class="ai-forward-node-list">
                    <article v-for="(node, nodeIndex) in cachedForwardMessage(ref)?.nodes || []" :key="node.message_id || `${ref.id}-${nodeIndex}`" class="ai-forward-node">
                      <div class="ai-forward-node-head">
                        <strong>{{ forwardNodeTitle(node) }}</strong>
                        <time>{{ formatDateTime(node.time) }}</time>
                      </div>
                      <div class="ai-forward-segment-list">
                        <template v-for="(segment, segmentIndex) in node.content || []" :key="`${nodeIndex}-${segmentIndex}`">
                          <p v-if="segmentText(segment)" class="ai-forward-text">{{ segmentText(segment) }}</p>
                          <img v-else-if="segmentImageURL(segment)" class="ai-forward-image" :src="segmentImageURL(segment)" :alt="segmentLabel(segment)" loading="lazy" referrerpolicy="no-referrer" />
                          <span v-else-if="segmentForwardID(segment)" class="ai-forward-nested">合并转发：{{ segmentForwardID(segment) }}</span>
                          <span v-else class="ai-forward-segment-placeholder">{{ segmentLabel(segment) }}</span>
                        </template>
                        <span v-if="!(node.content || []).length" class="ai-forward-segment-placeholder">空消息</span>
                      </div>
                    </article>
                  </div>
                </article>
              </section>
              <div v-if="selectedMessageImages.length" class="ai-image-grid">
                <article v-for="image in selectedMessageImages" :key="image.id || image.segment_index" class="ai-image-card">
                  <a :href="imagePreviewURL(image)" target="_blank" rel="noreferrer"><img class="ai-image-preview" :src="imagePreviewURL(image)" :alt="image.file_name || `image-${image.segment_index}`" loading="lazy" /></a>
                  <dl class="detail-list compact-detail-list"><div><dt>状态</dt><dd>{{ image.vision_status || image.asset_status || '-' }}</dd></div><div><dt>文件</dt><dd>{{ image.file_name || '-' }}</dd></div><div><dt>类型</dt><dd>{{ image.mime_type || '-' }}</dd></div><div><dt>大小</dt><dd>{{ formatBytes(image.size_bytes) }}</dd></div></dl>
                  <p v-if="image.vision_summary" class="inline-note">{{ image.vision_summary }}</p>
                  <p v-if="image.asset_error" class="error-copy">{{ image.asset_error }}</p>
                </article>
              </div>
            </template>

            <template v-else>
              <dl class="detail-list compact-detail-list">
                <div><dt>消息总数</dt><dd>{{ activeConversationMessages.length }}</dd></div>
                <div><dt>图片数</dt><dd>{{ activeConversation?.unreadImages || 0 }}</dd></div>
                <div><dt>最近消息</dt><dd>{{ formatDateTime(activeConversation?.lastAt) }}</dd></div>
                <div><dt>会话类型</dt><dd>{{ activeConversation?.subtitle || '-' }}</dd></div>
              </dl>
              <div class="subcard ai-note-card">
                <h4>最近说明</h4>
                <p class="inline-note">{{ activeConversationMessages.length ? previewText(activeConversationMessages[activeConversationMessages.length - 1]) : '选择一个会话后在这里查看摘要。' }}</p>
              </div>
            </template>
          </aside>
        </div>
      </div>
    </section>
  </section>
</template>

<style scoped>
.ai-panel {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.ai-summary-grid {
  margin-bottom: 0;
}

.ai-master-card {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.ai-master-head {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 16px;
}

.ai-master-head h3,
.ai-master-head p {
  margin: 0;
}

.ai-master-head p {
  margin-top: 6px;
  color: var(--text-soft);
  line-height: 1.6;
}

.ai-pill-tabs {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.ai-pill-btn {
  min-height: 40px;
  padding: 0 18px;
  border-radius: 999px;
  border: 1px solid var(--soft-border);
  background: var(--chip-bg);
  color: var(--text-secondary);
  font-weight: 600;
}

.ai-pill-btn.active {
  border-color: transparent;
  background: var(--button-primary-bg);
  color: var(--button-primary-text);
}

.ai-tab-body {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.ai-message-filter-row {
  margin: 0;
  padding: 0;
}

.ai-chat-shell {
  display: grid;
  grid-template-columns: minmax(260px, 0.7fr) minmax(0, 1.3fr) minmax(280px, 0.72fr);
  min-height: 720px;
  border: 1px solid var(--soft-border);
  border-radius: 24px;
  overflow: hidden;
  background: var(--surface-soft-alt);
}

.ai-pane {
  min-width: 0;
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 14px;
  background: var(--card-bg);
}

.ai-conversation-pane,
.ai-chat-thread-pane {
  border-right: 1px solid var(--soft-divider);
}

.ai-pane-header {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.ai-pane-header h4,
.ai-pane-header p {
  margin: 0;
}

.ai-pane-header p {
  color: var(--text-soft);
  line-height: 1.6;
}

.ai-message-pager {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 10px 12px;
  border: 1px solid var(--soft-border);
  border-radius: 16px;
  background: var(--surface-soft-alt);
  color: var(--text-muted);
  font-size: 12px;
}

.ai-message-pager-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: flex-end;
  gap: 6px;
}

.ai-message-compose {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin-top: auto;
  padding: 12px;
  border: 1px solid var(--soft-border);
  border-radius: 18px;
  background: var(--surface-soft-alt);
}

.ai-message-compose-target {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  color: var(--text-secondary);
  font-size: 12px;
}

.ai-message-compose-target span {
  color: var(--text-muted);
}

.ai-message-compose-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 10px;
  align-items: stretch;
}

.ai-message-compose-input {
  min-height: 52px;
  resize: vertical;
}

.ai-message-send-btn {
  min-width: 86px;
}

.ai-conversation-item {
  position: relative;
  display: grid;
  grid-template-columns: 42px minmax(0, 1fr);
  gap: 12px;
  align-items: start;
  width: 100%;
  border: 1px solid var(--soft-border);
  border-radius: 18px;
  background: var(--surface-soft-alt);
  color: var(--text-primary);
  padding: 12px;
  text-align: left;
}

.ai-conversation-item.active {
  border-color: var(--selection-border);
  background: var(--selection-bg);
  box-shadow: inset 0 0 0 1px var(--selection-shadow);
}

.ai-conversation-avatar,
.ai-chat-avatar {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex: 0 0 42px;
  width: 42px;
  height: 42px;
  min-width: 42px;
  min-height: 42px;
  aspect-ratio: 1 / 1;
  border-radius: 16px;
  background: var(--avatar-bg);
  color: #ffffff;
  font-weight: 800;
}

.ai-conversation-avatar.has-image,
.ai-chat-avatar.has-image {
  overflow: hidden;
  padding: 0;
  background: var(--selection-bg);
}

.ai-avatar-image {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
  border-radius: inherit;
}

.ai-conversation-copy,
.ai-chat-bubble-shell {
  min-width: 0;
  flex: 1 1 auto;
}

.ai-conversation-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}

.ai-chat-message-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.ai-conversation-head strong,
.ai-conversation-copy p,
.ai-conversation-copy span {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ai-chat-message-meta strong {
  min-width: 0;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ai-conversation-head time,
.ai-conversation-copy span,
.ai-chat-message-meta time {
  color: var(--text-muted);
  font-size: 12px;
}

.ai-chat-message-meta time {
  margin-left: auto;
}

.ai-conversation-copy p {
  margin: 4px 0 2px;
  color: var(--text-secondary);
}

.ai-conversation-badge {
  position: absolute;
  top: 10px;
  right: 10px;
  min-width: 20px;
  min-height: 20px;
  border-radius: 999px;
  padding: 2px 6px;
  background: #ef4444;
  color: #ffffff;
  font-size: 11px;
  font-weight: 800;
  text-align: center;
}

.ai-chat-scroll {
  display: flex;
  flex-direction: column;
  gap: 14px;
  overflow: auto;
}

.ai-chat-message {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  width: fit-content;
  max-width: min(100%, 760px);
}

.ai-chat-message.is-peer {
  align-self: flex-start;
}

.ai-chat-message.is-self {
  align-self: flex-end;
  flex-direction: row-reverse;
}

.ai-chat-message.is-system {
  align-self: center;
  max-width: min(100%, 94%);
}

.ai-chat-message.is-self .ai-chat-avatar {
  background: var(--chat-user-bg);
}

.ai-chat-bubble {
  width: 100%;
  padding: 12px 14px;
  border-radius: 18px;
  border: 1px solid var(--soft-border);
  background: var(--chat-assistant-bg);
  color: var(--text-primary);
  text-align: left;
  display: flex;
  flex-direction: column;
  gap: 6px;
  white-space: pre-wrap;
  word-break: break-word;
}

.ai-chat-message.is-self .ai-chat-bubble {
  background: var(--selection-bg);
  border-color: var(--selection-border);
}

.ai-chat-message.is-system .ai-chat-bubble {
  background: var(--info-bg-soft);
}

.ai-chat-message.selected .ai-chat-bubble {
  border-color: var(--selection-border);
  box-shadow: 0 0 0 3px var(--selection-shadow);
}

.ai-inline-image-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(120px, 1fr));
  gap: 8px;
  width: min(360px, 100%);
}

.ai-inline-image {
  display: block;
  width: 100%;
  max-height: 220px;
  object-fit: contain;
  border-radius: 14px;
  border: 1px solid var(--soft-border);
  background: var(--code-surface);
}

.ai-inline-image-loading {
  color: var(--text-soft);
  font-size: 13px;
}

.ai-forward-chip-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.ai-forward-chip {
  display: flex;
  flex-direction: column;
  gap: 4px;
  width: min(320px, 100%);
  padding: 12px;
  border: 1px solid var(--selection-border);
  border-radius: 16px;
  background: var(--selection-bg);
}

.ai-forward-chip small {
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--text-muted);
}

.ai-dark-badge,
.ai-chat-bubble > small {
  display: inline-flex;
  align-items: center;
  width: fit-content;
  min-height: 22px;
  padding: 0 8px;
  border-radius: 999px;
  background: var(--chip-bg);
  color: var(--accent-strong);
  font-size: 11px;
  font-weight: 700;
}

.ai-note-card {
  margin-top: auto;
}

.ai-inspector-pane {
  overflow: auto;
}

.ai-message-text-block {
  padding: 14px;
  border: 1px solid var(--soft-border);
  border-radius: 16px;
  background: var(--surface-soft-alt);
  color: var(--text-primary);
  line-height: 1.7;
  white-space: pre-wrap;
  word-break: break-word;
}

.ai-forward-detail-section,
.ai-forward-node-list,
.ai-forward-segment-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.ai-forward-section-head,
.ai-forward-detail-head,
.ai-forward-node-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 10px;
}

.ai-forward-section-head h4,
.ai-forward-detail-head p,
.ai-forward-text {
  margin: 0;
}

.ai-forward-section-head span,
.ai-forward-detail-head p,
.ai-forward-node-head time {
  color: var(--text-muted);
  font-size: 12px;
}

.ai-forward-detail-card,
.ai-forward-node {
  padding: 12px;
  border: 1px solid var(--soft-border);
  border-radius: 18px;
  background: var(--surface-soft-alt);
}

.ai-forward-node {
  background: var(--card-bg);
}

.ai-forward-text {
  white-space: pre-wrap;
  word-break: break-word;
  line-height: 1.65;
}

.ai-forward-image {
  display: block;
  width: 100%;
  max-height: 240px;
  object-fit: contain;
  border: 1px solid var(--soft-border);
  border-radius: 14px;
  background: var(--code-surface);
}

.ai-forward-nested,
.ai-forward-segment-placeholder {
  display: inline-flex;
  width: fit-content;
  min-height: 24px;
  align-items: center;
  border-radius: 999px;
  padding: 0 9px;
  background: var(--chip-bg);
  color: var(--text-secondary);
  font-size: 12px;
  font-weight: 700;
}

.ai-image-grid {
  display: grid;
  grid-template-columns: 1fr;
  gap: 12px;
}

.ai-image-card {
  padding: 12px;
  border: 1px solid var(--soft-border);
  border-radius: 18px;
  background: var(--surface-soft-alt);
}

.ai-image-card a {
  display: block;
}

.ai-image-preview {
  display: block;
  width: 100%;
  max-height: 280px;
  object-fit: contain;
  border: 1px solid var(--soft-border);
  border-radius: 14px;
  background: var(--code-surface);
}

@media (max-width: 1240px) {
  .ai-chat-shell {
    grid-template-columns: 1fr;
  }

  .ai-conversation-pane,
  .ai-chat-thread-pane {
    border-right: none;
    border-bottom: 1px solid var(--soft-divider);
  }

  .ai-chat-message {
    max-width: 100%;
  }
}

@media (max-width: 860px) {
  .ai-master-head {
    flex-direction: column;
  }

  .ai-message-compose-row {
    grid-template-columns: 1fr;
  }
}
</style>
