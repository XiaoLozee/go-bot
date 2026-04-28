export interface APIErrorPayload {
  message?: string
  detail?: string
  error?: string
  [key: string]: unknown
}

export interface RuntimeConfig {
  basePath: string
  vueBasePath: string
  apiBasePrefix: string
  apiBasePath: string
}

export interface AuthState {
  enabled: boolean
  configured: boolean
  requires_setup: boolean
  authenticated: boolean
  webui_theme?: string
  [key: string]: unknown
}

export interface RuntimeSnapshot {
  state: string
  started_at?: string
  app_name: string
  environment: string
  connections: number
  plugins: number
  [key: string]: unknown
}

export interface Metadata {
  app_name: string
  environment: string
  owner_qq?: string
  admin_enabled: boolean
  webui_enabled: boolean
  webui_base_path?: string
  webui_theme?: string
  capabilities: Record<string, boolean>
  [key: string]: unknown
}

export interface PluginSnapshot {
  id: string
  name: string
  version: string
  description?: string
  author?: string
  homepage?: string
  kind?: string
  builtin: boolean
  configured: boolean
  enabled: boolean
  state: string
  last_error?: string
  health_reason?: string
  [key: string]: unknown
}

export interface PluginRuntimeLogEntry {
  at?: string
  level?: string
  source?: string
  message?: string
  [key: string]: unknown
}

export interface RuntimeStatus {
  state?: string
  running?: boolean
  pid?: number
  started_at?: string
  stopped_at?: string
  next_restart_at?: string
  last_started_at?: string
  last_failure_at?: string
  last_error?: string
  circuit_open?: boolean
  circuit_reason?: string
  restarting?: boolean
  restarts?: number
  health_reason?: string
  recent_logs?: PluginRuntimeLogEntry[]
  [key: string]: unknown
}

export interface PluginAPIAction {
  name: string
  title?: string
  description?: string
  timeout_ms?: number
  input_schema?: Record<string, unknown>
  output_schema?: Record<string, unknown>
  [key: string]: unknown
}

export interface PluginAPIEvent {
  topic: string
  title?: string
  description?: string
  payload_schema?: Record<string, unknown>
  [key: string]: unknown
}

export interface PluginAPISpec {
  protocol_version?: string
  plugin_name?: string
  plugin_version?: string
  actions?: PluginAPIAction[]
  events?: PluginAPIEvent[]
  [key: string]: unknown
}

export interface PluginDetail {
  snapshot: PluginSnapshot
  runtime?: RuntimeStatus
  metadata?: Record<string, unknown>
  config?: Record<string, unknown>
  config_schema?: Record<string, unknown>
  config_schema_path?: string
  config_schema_error?: string
  effective_config?: Record<string, unknown>
  config_source?: string
  api_spec?: PluginAPISpec
  [key: string]: unknown
}

export interface PluginConfigSaveResult {
  ok?: boolean
  message?: string
  detail?: PluginDetail
  [key: string]: unknown
}

export interface PluginInstallResult {
  ok?: boolean
  message?: string
  detail?: PluginDetail
  plugin_id?: string
  kind?: string
  format?: string
  installed_to?: string
  manifest_path?: string
  backup_path?: string
  dependency_env_path?: string
  dependencies_installed?: boolean
  replaced?: boolean
  reloaded?: boolean
  [key: string]: unknown
}

export interface ConnectionSnapshot {
  id: string
  platform?: string
  ingress_type?: string
  action_type?: string
  self_id?: string
  self_nickname?: string
  online?: boolean
  enabled?: boolean
  good?: boolean
  state?: string
  ingress_state?: string
  connected_clients?: number
  observed_events?: number
  updated_at?: string
  last_error?: string
  [key: string]: unknown
}

export interface ConnectionIngressConfig {
  type: string
  listen?: string
  path?: string
  url?: string
  retry_interval_ms?: number
  [key: string]: unknown
}

export interface ConnectionActionConfig {
  type: string
  base_url?: string
  timeout_ms?: number
  access_token?: string
  [key: string]: unknown
}

export interface ConnectionConfig {
  id: string
  enabled: boolean
  platform: string
  ingress: ConnectionIngressConfig
  action: ConnectionActionConfig
  [key: string]: unknown
}

export interface ConnectionDetail {
  snapshot: ConnectionSnapshot
  config: ConnectionConfig
  [key: string]: unknown
}

export interface ConnectionSaveResult {
  ok?: boolean
  message?: string
  detail?: ConnectionDetail
  [key: string]: unknown
}

export interface WebUIBootstrap {
  auth: AuthState
  meta: Metadata
  runtime: RuntimeSnapshot
  plugins: PluginSnapshot[]
  connections: ConnectionSnapshot[]
  config: Record<string, unknown>
  generated_at?: string
  [key: string]: unknown
}

export interface RuntimeRestartResult {
  state?: string
  message?: string
  [key: string]: unknown
}

export interface ConfigSaveResult {
  ok?: boolean
  message?: string
  normalized_config?: Record<string, unknown>
  [key: string]: unknown
}

export interface AIMessageLog {
  message_id: string
  connection_id?: string
  chat_type?: string
  group_id?: string
  group_name?: string
  group_avatar_url?: string
  user_id?: string
  sender_name?: string
  sender_nickname?: string
  sender_avatar_url?: string
  sender_role?: string
  message_status?: string
  text_content?: string
  has_image?: boolean
  image_count?: number
  occurred_at: string
  [key: string]: unknown
}

export interface AIMessageImage {
  id?: string
  message_id: string
  segment_index: number
  preview_url?: string
  file_name?: string
  mime_type?: string
  size_bytes?: number
  vision_status?: string
  asset_status?: string
  vision_summary?: string
  asset_error?: string
  [key: string]: unknown
}

export interface AIMessageSegment {
  type: string
  data?: Record<string, unknown>
  [key: string]: unknown
}

export interface AIForwardMessageNode {
  time?: string
  message_id?: string
  user_id?: string
  nickname?: string
  content?: AIMessageSegment[]
  [key: string]: unknown
}

export interface AIForwardMessage {
  connection_id: string
  forward_id: string
  nodes?: AIForwardMessageNode[]
  fetched_at?: string
  cached?: boolean
  [key: string]: unknown
}

export interface AIMessageDetail {
  message: AIMessageLog
  images?: AIMessageImage[]
  [key: string]: unknown
}

export interface AIMessageLogResponse {
  items?: AIMessageLog[]
  [key: string]: unknown
}

export interface AIMessageSendResult {
  accepted?: boolean
  connection_id: string
  chat_type: string
  group_id?: string
  user_id?: string
  sent_at?: string
  message?: string
  [key: string]: unknown
}

export interface AIRecentMessagesSyncResult {
  accepted?: boolean
  connection_id: string
  chat_type: string
  group_id?: string
  user_id?: string
  requested?: number
  fetched?: number
  synced?: number
  synced_at?: string
  message?: string
  [key: string]: unknown
}

export interface AIRecentMessagesBulkSyncResult {
  accepted?: boolean
  connection_id: string
  chat_type: string
  targets?: number
  requested?: number
  fetched?: number
  synced?: number
  failed?: number
  synced_at?: string
  message?: string
  [key: string]: unknown
}

export interface AIMessageSuggestions {
  groups?: string[]
  users?: string[]
  [key: string]: unknown
}

export interface AIViewSnapshot {
  state?: string
  ready?: boolean
  enabled?: boolean
  session_count?: number
  candidate_count?: number
  long_term_count?: number
  provider_vendor?: string
  provider_kind?: string
  model?: string
  store_engine?: string
  store_ready?: boolean
  vision_enabled?: boolean
  vision_mode?: string
  vision_provider?: string
  vision_model?: string
  group_profile_count?: number
  user_profile_count?: number
  relation_edge_count?: number
  skill_provider_count?: number
  skill_tool_count?: number
  private_persona_count?: number
  last_reply_at?: string
  last_vision_at?: string
  last_reflection_at?: string
  reflection_running?: boolean
  last_error?: string
  last_vision_error?: string
  last_reflection_error?: string
  last_decision_reason?: string
  [key: string]: unknown
}

export interface AISkillToolView {
  name: string
  description?: string
  display_name?: string
  display_description?: string
  availability?: string
  [key: string]: unknown
}

export interface AISkillView {
  provider_id: string
  source: string
  plugin_id?: string
  namespace: string
  name: string
  description?: string
  tool_count: number
  tools?: AISkillToolView[]
  [key: string]: unknown
}

export interface AIInstalledSkillView {
  id: string
  name: string
  description?: string
  source_type: string
  source_label?: string
  source_url?: string
  provider?: string
  enabled: boolean
  installed_at?: string
  updated_at?: string
  entry_path?: string
  format?: string
  instruction_preview?: string
  content_length?: number
  [key: string]: unknown
}

export interface AIInstalledSkillDetailView extends AIInstalledSkillView {
  content?: string
}

export interface AIMCPToolView {
  name: string
  original: string
  description?: string
  [key: string]: unknown
}

export interface AIMCPServerView {
  id: string
  name?: string
  enabled: boolean
  transport: string
  state: string
  protocol_version?: string
  server_name?: string
  server_version?: string
  tool_count: number
  tools?: AIMCPToolView[]
  last_error?: string
  connected_at?: string
  sse_state?: string
  last_sse_error?: string
  last_sse_event_id?: string
  last_sse_at?: string
  last_refresh_at?: string
  [key: string]: unknown
}

export interface AIDebugView {
  mcp_servers?: AIMCPServerView[]
  [key: string]: unknown
}

export interface AIView {
  snapshot?: AIViewSnapshot | null
  config?: Record<string, unknown>
  debug?: AIDebugView
  skills?: AISkillView[]
  installed_skills?: AIInstalledSkillView[]
  [key: string]: unknown
}

export interface AIMemoryActionResult {
  message?: string
  action?: string
  target?: string
  view?: AIView
  [key: string]: unknown
}

export interface AIRelationAnalysisResult {
  accepted?: boolean
  group_id?: string
  markdown?: string
  generated_at?: string
  expires_at?: string
  user_count?: number
  edge_count?: number
  memory_count?: number
  input_hash?: string
  cache_hit?: boolean
  message?: string
  [key: string]: unknown
}

export interface AIRelationAnalysisTaskView {
  accepted?: boolean
  task_id?: string
  status?: string
  group_id?: string
  force?: boolean
  created_at?: string
  started_at?: string
  finished_at?: string
  result?: AIRelationAnalysisResult | null
  error?: string
  message?: string
  [key: string]: unknown
}

export interface AIConfigSaveResult {
  message?: string
  view?: AIView
  [key: string]: unknown
}

export interface AISkillActionResult {
  accepted?: boolean
  action?: string
  id?: string
  enabled?: boolean
  skill?: AIInstalledSkillView | null
  view?: AIView
  message?: string
  [key: string]: unknown
}

export interface AISkillInstallResult {
  accepted?: boolean
  replaced?: boolean
  installed_to?: string
  backup_path?: string
  skill?: AIInstalledSkillDetailView | null
  view?: AIView
  message?: string
  [key: string]: unknown
}

export interface AIProviderModel {
  id: string
  owned_by?: string
  [key: string]: unknown
}

export interface AIProviderModelsResult {
  accepted?: boolean
  models?: AIProviderModel[]
  fetched_at?: string
  message?: string
  [key: string]: unknown
}

export interface AuditLogEntry {
  at?: string
  actor?: string
  category?: string
  action?: string
  result: string
  summary?: string
  detail?: string
  target?: string
  [key: string]: unknown
}

export interface AuditLogResponse {
  items?: AuditLogEntry[]
  [key: string]: unknown
}

export interface AdminSystemLogResponse {
  available?: boolean
  path?: string
  dir?: string
  lines?: string[]
  limit?: number
  message?: string
  [key: string]: unknown
}
