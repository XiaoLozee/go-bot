<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { APIError, requestJSON } from '../lib/http'
import { useNotice } from '../composables/useNotice'
import { formatWebUIThemeLabel } from '../lib/webui-theme'

import WorkspaceSidebar from './layout/WorkspaceSidebar.vue'
import WorkspaceHeader from './layout/WorkspaceHeader.vue'
import WorkspaceSummary from './layout/WorkspaceSummary.vue'

import AIPanel from './AIPanel.vue'
import AuditPanel from './AuditPanel.vue'
import ConnectionConfigPanel from './ConnectionConfigPanel.vue'
import OverviewPanel from './OverviewPanel.vue'
import PluginAPIDebugPanel from './PluginAPIDebugPanel.vue'
import PluginPanel from './PluginPanel.vue'
import SystemConfigPanel from './SystemConfigPanel.vue'

import type {
  AuthState,
  RuntimeConfig,
  WebUIBootstrap,
} from '../types/api'

type WorkspaceView = 'overview' | 'ai' | 'plugins' | 'plugin-api' | 'connections' | 'audit' | 'config'
type SummaryAccent = 'runtime' | 'connection' | 'plugin' | 'webui' | 'danger'
type WorkspaceNavItem = {
  value: WorkspaceView
  label: string
  icon: string
  subtitle: string
  kicker: string
  heroTitle: string
  heroCopy: string
}

const props = defineProps<{
  runtimeConfig: RuntimeConfig
  authState: AuthState | null
}>()

const emit = defineEmits<{
  unauthorized: []
  logout: []
  themeChanged: [theme: string]
}>()

const { notice, showNotice } = useNotice()
const activeView = ref<WorkspaceView>('overview')
const bootstrap = ref<WebUIBootstrap | null>(null)
const loadingBootstrap = ref(false)
const actionBusy = ref(false)

const workspaceNavItems: WorkspaceNavItem[] = [
  {
    value: 'overview',
    label: '总览',
    icon: 'OV',
    subtitle: '查看运行状态、连接、插件与配置变更。',
    kicker: '运行总览',
    heroTitle: '查看运行状态、连接、插件与配置变更',
    heroCopy: '在一个主视图里查看 OneBot 连接、AI 服务、插件状态和最近配置变更。',
  },
  {
    value: 'ai',
    label: 'AI',
    icon: 'AI',
    subtitle: '管理模型状态、记忆策略和最近对话。',
    kicker: 'AI 控制台',
    heroTitle: '查看模型状态与最近对话',
    heroCopy: '集中查看 AI 运行状态、消息记录、候选记忆、长期记忆和群策略。',
  },
  {
    value: 'plugins',
    label: '插件',
    icon: 'PL',
    subtitle: '查看插件目录、版本、配置与运行状态。',
    kicker: '插件工作台',
    heroTitle: '查看插件目录、版本与运行状态',
    heroCopy: '在同一页完成插件筛选、启停、配置编辑和插件安装。',
  },
  {
    value: 'plugin-api',
    label: '接口调试',
    icon: 'PD',
    subtitle: '调试插件可用的消息、群、连接与媒体接口。',
    kicker: '插件 API 调试',
    heroTitle: '单独调试插件侧可调用的宿主接口',
    heroCopy: '集中查看调试方法、请求参数、Python 示例和响应结果，不再和插件管理混排。',
  },
  {
    value: 'connections',
    label: '连接',
    icon: 'CN',
    subtitle: '维护连接配置、探测结果与最近错误。',
    kicker: '连接管理',
    heroTitle: '维护连接配置与探测状态',
    heroCopy: '集中查看入口类型、在线状态、动作通道和最近错误，并直接编辑连接配置。',
  },
  {
    value: 'audit',
    label: '审计',
    icon: 'AT',
    subtitle: '查看后台操作时间线与失败项。',
    kicker: '操作审计',
    heroTitle: '按筛选条件查看后台操作时间线',
    heroCopy: '登录、配置保存、插件操作和 WebUI 请求都会记录在这里，便于追查失败项。',
  },
  {
    value: 'config',
    label: '配置',
    icon: 'CF',
    subtitle: '维护运行参数、WebUI 设置与后台密码。',
    kicker: '系统配置',
    heroTitle: '集中维护运行参数与后台设置',
    heroCopy: '在这里保存系统配置、切换 WebUI 主题、热重启运行时，并更新后台密码。',
  },
]

const activeViewMeta = computed(() => workspaceNavItems.find((item) => item.value === activeView.value) || workspaceNavItems[0])
const showOverviewMeta = computed(() => activeView.value === 'overview')

function presentTheme(value?: string): string {
  return formatWebUIThemeLabel(value)
}

function formatDateTime(value?: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function presentAuthState(state: AuthState | null): string {
  if (!state?.enabled) return '未启用'
  if (state.requires_setup) return '待初始化'
  if (state.authenticated) return '已登录'
  return '已启用'
}

const summaryCards = computed<Array<{ accent: SummaryAccent; label: string; value: string; note: string }>>(() => {
  const list = bootstrap.value?.plugins || []
  const enabledCount = list.filter((item) => item.configured && item.enabled).length
  const totalConnections = bootstrap.value?.connections.length ?? bootstrap.value?.runtime.connections ?? 0
  const onlineConnections = bootstrap.value?.connections.filter((item) => item.online).length ?? 0
  const webuiPath = bootstrap.value?.meta.webui_base_path || props.runtimeConfig.vueBasePath || '/'
  return [
    {
      accent: 'runtime',
      label: '运行状态',
      value: bootstrap.value?.runtime.state || '未知',
      note: bootstrap.value?.generated_at ? `最近快照：${formatDateTime(bootstrap.value.generated_at)}` : '等待后台快照。',
    },
    {
      accent: 'connection',
      label: '在线连接',
      value: String(onlineConnections),
      note: `共 ${totalConnections} 个连接配置。`,
    },
    {
      accent: 'plugin',
      label: '已启用插件',
      value: String(enabledCount),
      note: `当前识别到 ${list.length} 个插件。`,
    },
    {
      accent: 'webui',
      label: '当前主题',
      value: presentTheme(props.authState?.webui_theme || bootstrap.value?.meta.webui_theme || 'blue-light'),
      note: `WebUI 路径：${webuiPath}`,
    },
  ]
})

const heroPills = computed(() => [
  { label: '应用', value: bootstrap.value?.runtime.app_name || bootstrap.value?.meta.app_name || 'Go-bot' },
  { label: '环境', value: bootstrap.value?.runtime.environment || bootstrap.value?.meta.environment || '未设置' },
  { label: '后台鉴权', value: presentAuthState(props.authState) },
  { label: '主题', value: presentTheme(props.authState?.webui_theme || bootstrap.value?.meta.webui_theme || 'blue-light') },
])

const heroMetrics = computed(() => {
  const totalConnections = bootstrap.value?.connections.length ?? bootstrap.value?.runtime.connections ?? 0
  const onlineConnections = bootstrap.value?.connections.filter((item) => item.online).length ?? 0
  const enabledCount = (bootstrap.value?.plugins || []).filter((item) => item.configured && item.enabled).length
  return [
    {
      label: '连接',
      value: String(totalConnections),
      note: `${onlineConnections} 个在线`,
    },
    {
      label: '插件',
      value: String((bootstrap.value?.plugins || []).length),
      note: `${enabledCount} 个已启用`,
    },
    {
      label: '入口',
      value: bootstrap.value?.meta.webui_base_path || props.runtimeConfig.vueBasePath || '/',
      note: `主人 QQ：${bootstrap.value?.meta.owner_qq || '-'}`,
    },
  ]
})

function apiURL(path: string): string {
  return props.runtimeConfig.apiBasePath + path
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

async function loadBootstrap() {
  loadingBootstrap.value = true
  try {
    const data = await requestJSON<WebUIBootstrap>(apiURL('/webui/bootstrap'))
    bootstrap.value = data
  } catch (error) {
    showNotice('error', '工作台加载失败', formatError(error, '加载管理工作台失败。'))
  } finally {
    loadingBootstrap.value = false
  }
}

async function refreshWorkspace() {
  await loadBootstrap()
}

onMounted(async () => {
  await loadBootstrap()
})
</script>

<template>
  <section class="workspace-app-shell">
    <WorkspaceSidebar
      v-model:activeView="activeView"
      :navItems="workspaceNavItems"
      :bootstrap="bootstrap"
      :authState="authState"
    />

    <section class="workspace-main">
      <WorkspaceHeader
        :activeViewMeta="activeViewMeta"
        :bootstrap="bootstrap"
        :heroPills="heroPills"
        :heroMetrics="heroMetrics"
        :showOverviewMeta="showOverviewMeta"
        :loadingBootstrap="loadingBootstrap"
        :actionBusy="actionBusy"
        @refresh="refreshWorkspace"
        @logout="emit('logout')"
      />

      <div v-if="notice" class="banner" :class="`banner-${notice.kind}`">
        <strong>{{ notice.title }}</strong>
        <span>{{ notice.text }}</span>
      </div>

      <WorkspaceSummary v-if="showOverviewMeta" :summaryCards="summaryCards" />

      <div class="main-panel">
        <OverviewPanel v-if="activeView === 'overview'" :bootstrap="bootstrap" />
        <AIPanel
          v-else-if="activeView === 'ai'"
          :runtime-config="runtimeConfig"
          :busy="actionBusy"
          :connections="bootstrap?.connections || []"
          @busy="actionBusy = $event"
          @notice="showNotice($event.kind, $event.title, $event.text)"
          @unauthorized="emit('unauthorized')"
        />
        <PluginPanel
          v-else-if="activeView === 'plugins'"
          :runtime-config="runtimeConfig"
          :bootstrap="bootstrap"
          :busy="actionBusy"
          @busy="actionBusy = $event"
          @unauthorized="emit('unauthorized')"
          @refresh="refreshWorkspace"
          @notice="showNotice($event.kind, $event.title, $event.text)"
        />
        <PluginAPIDebugPanel
          v-else-if="activeView === 'plugin-api'"
          :runtime-config="runtimeConfig"
          :bootstrap="bootstrap"
          :busy="actionBusy"
          @busy="actionBusy = $event"
          @unauthorized="emit('unauthorized')"
        />
        <ConnectionConfigPanel
          v-else-if="activeView === 'connections'"
          :runtime-config="runtimeConfig"
          :bootstrap="bootstrap"
          :busy="actionBusy"
          @busy="actionBusy = $event"
          @refresh="refreshWorkspace"
          @notice="showNotice($event.kind, $event.title, $event.text)"
          @unauthorized="emit('unauthorized')"
        />
        <AuditPanel
          v-else-if="activeView === 'audit'"
          :runtime-config="runtimeConfig"
          :busy="actionBusy"
          @busy="actionBusy = $event"
          @notice="showNotice($event.kind, $event.title, $event.text)"
          @unauthorized="emit('unauthorized')"
        />
        <SystemConfigPanel
          v-else
          :runtime-config="runtimeConfig"
          :bootstrap="bootstrap"
          :busy="actionBusy"
          @busy="actionBusy = $event"
          @refresh="refreshWorkspace"
          @notice="showNotice($event.kind, $event.title, $event.text)"
          @theme-changed="emit('themeChanged', $event)"
          @unauthorized="emit('unauthorized')"
        />
      </div>
    </section>
  </section>
</template>

<style scoped>
.workspace-app-shell {
  --workspace-shell-padding: 24px;
  --workspace-sidebar-width: 272px;
  width: 100%;
  min-height: 100vh;
  margin: 0;
  padding: var(--workspace-shell-padding);
  background: transparent;
}

.workspace-main {
  min-width: 0;
  margin-left: var(--workspace-sidebar-width);
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.main-panel {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 18px;
}

@media (max-width: 1120px) {
  .workspace-app-shell {
    padding: 0;
  }

  .workspace-main {
    margin-left: 0;
  }
}
</style>
