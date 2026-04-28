<script setup lang="ts">
import { computed } from 'vue'
import { formatWebUIThemeLabel } from '../lib/webui-theme'
import type { WebUIBootstrap } from '../types/api'

type OverviewEvent = {
  title: string
  note: string
  meta: string
  tone: 'blue' | 'green' | 'amber' | 'red'
}

type SummaryItem = {
  title: string
  value: string
  note: string
}

const props = defineProps<{
  bootstrap: WebUIBootstrap | null
}>()

function formatDateTime(value?: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

function describeTone(value: OverviewEvent['tone']): string {
  if (value === 'green') return '运行中'
  if (value === 'amber') return '待处理'
  if (value === 'red') return '异常'
  return '已记录'
}

const connectionSummary = computed<SummaryItem[]>(() => {
  const list = props.bootstrap?.connections || []
  const online = list.filter((item) => item.online).length
  return [
    {
      title: '连接总数',
      value: String(list.length),
      note: `${online} 个连接当前在线。`,
    },
    {
      title: '运行中',
      value: String(props.bootstrap?.runtime.connections ?? 0),
      note: '运行时上报的连接数量。',
    },
  ]
})

const pluginSummary = computed<SummaryItem[]>(() => {
  const list = props.bootstrap?.plugins || []
  const enabled = list.filter((item) => item.configured && item.enabled).length
  return [
    {
      title: '插件总数',
      value: String(list.length),
      note: `${enabled} 个插件已启用。`,
    },
    {
      title: '外部插件',
      value: String(list.filter((item) => !item.builtin && item.kind !== 'builtin').length),
      note: '包含外部执行插件与本地扩展。',
    },
  ]
})

const configSummary = computed<SummaryItem[]>(() => {
  return [
    {
      title: '后台服务',
      value: props.bootstrap?.meta.admin_enabled ? '已启用' : '未启用',
      note: `主题：${formatWebUIThemeLabel(props.bootstrap?.meta.webui_theme || 'blue-light')}`,
    },
    {
      title: '快照时间',
      value: formatDateTime(props.bootstrap?.generated_at),
      note: `路径：${props.bootstrap?.meta.webui_base_path || '/'}`,
    },
  ]
})

const recentEvents = computed<OverviewEvent[]>(() => {
  const list: OverviewEvent[] = []

  if (props.bootstrap?.generated_at) {
    list.push({
      title: '后台快照已刷新',
      note: '当前控制台已经同步最新运行状态。',
      meta: formatDateTime(props.bootstrap.generated_at),
      tone: 'blue',
    })
  }

  for (const connection of props.bootstrap?.connections || []) {
    list.push({
      title: connection.id,
      note: `${connection.online ? '连接在线' : '连接待命'} · ${connection.ingress_type || 'unknown'}`,
      meta: connection.self_nickname || connection.self_id || '连接信息',
      tone: connection.last_error ? 'red' : connection.online ? 'green' : 'amber',
    })
  }

  for (const plugin of props.bootstrap?.plugins || []) {
    list.push({
      title: plugin.name || plugin.id,
      note: plugin.last_error || (plugin.enabled ? '插件已启用' : '插件未启用'),
      meta: plugin.version || plugin.author || plugin.id,
      tone: plugin.last_error ? 'red' : plugin.enabled ? 'green' : 'amber',
    })
  }

  return list.slice(0, 6)
})

const pluginHealth = computed(() => {
  return (props.bootstrap?.plugins || []).slice(0, 4).map((plugin) => ({
    title: plugin.name || plugin.id,
    subtitle: plugin.id,
    status: plugin.last_error ? '异常' : plugin.enabled ? '运行中' : '待处理',
    note: plugin.description || '当前没有可展示的插件说明。',
    tone: plugin.last_error ? 'red' : plugin.enabled ? 'green' : 'amber',
  }))
})
</script>

<template>
  <section class="overview-panel">
    <section class="overview-summary-grid">
      <article class="card overview-summary-card">
        <div class="overview-card-head">
          <div>
            <span class="eyebrow">连接概况</span>
            <h3>连接状态</h3>
          </div>
        </div>
        <div class="overview-kpi-list">
          <article v-for="item in connectionSummary" :key="item.title" class="overview-kpi-item">
            <span>{{ item.title }}</span>
            <strong>{{ item.value }}</strong>
            <small>{{ item.note }}</small>
          </article>
        </div>
      </article>

      <article class="card overview-summary-card">
        <div class="overview-card-head">
          <div>
            <span class="eyebrow">插件概况</span>
            <h3>插件状态</h3>
          </div>
        </div>
        <div class="overview-kpi-list">
          <article v-for="item in pluginSummary" :key="item.title" class="overview-kpi-item">
            <span>{{ item.title }}</span>
            <strong>{{ item.value }}</strong>
            <small>{{ item.note }}</small>
          </article>
        </div>
      </article>

      <article class="card overview-summary-card">
        <div class="overview-card-head">
          <div>
            <span class="eyebrow">基础配置</span>
            <h3>控制台信息</h3>
          </div>
        </div>
        <div class="overview-kpi-list">
          <article v-for="item in configSummary" :key="item.title" class="overview-kpi-item">
            <span>{{ item.title }}</span>
            <strong>{{ item.value }}</strong>
            <small>{{ item.note }}</small>
          </article>
        </div>
      </article>
    </section>

    <section class="overview-content-grid">
      <article class="card overview-event-card">
        <div class="overview-card-head">
          <div>
            <span class="eyebrow">事件流</span>
            <h3>最近变化</h3>
          </div>
          <span class="muted">{{ recentEvents.length }} 条</span>
        </div>

        <div v-if="!recentEvents.length" class="empty-state compact">
          当前还没有可以展示的事件。
        </div>

        <div v-else class="overview-event-list">
          <article v-for="event in recentEvents" :key="`${event.title}-${event.meta}`" class="overview-event-item" :class="`tone-${event.tone}`">
            <div class="overview-event-copy">
              <strong>{{ event.title }}</strong>
              <p>{{ event.note }}</p>
            </div>
            <div class="overview-event-meta">
              <span class="status-pill" :class="`status-${event.tone === 'red' ? 'error' : event.tone === 'green' ? 'success' : 'info'}`">
                {{ describeTone(event.tone) }}
              </span>
              <small>{{ event.meta }}</small>
            </div>
          </article>
        </div>
      </article>

      <aside class="overview-side-column">
        <article class="card overview-side-card">
          <div class="overview-card-head">
            <div>
              <span class="eyebrow">插件健康摘要</span>
              <h3>插件就绪度</h3>
            </div>
          </div>

          <div v-if="!pluginHealth.length" class="empty-state compact">
            当前还没有插件数据。
          </div>

          <div v-else class="overview-health-list">
            <article v-for="plugin in pluginHealth" :key="plugin.subtitle" class="overview-health-item">
              <div>
                <strong>{{ plugin.title }}</strong>
                <small>{{ plugin.subtitle }}</small>
              </div>
              <span class="status-pill" :class="`status-${plugin.tone === 'red' ? 'error' : plugin.tone === 'green' ? 'success' : 'info'}`">
                {{ plugin.status }}
              </span>
              <p>{{ plugin.note }}</p>
            </article>
          </div>
        </article>

        <article class="card overview-side-card">
          <div class="overview-card-head">
            <div>
              <span class="eyebrow">后台元信息</span>
              <h3>当前值</h3>
            </div>
          </div>

          <dl class="detail-list compact-detail-list">
            <div><dt>应用</dt><dd>{{ props.bootstrap?.meta.app_name || props.bootstrap?.runtime.app_name || 'Go-bot' }}</dd></div>
            <div><dt>环境</dt><dd>{{ props.bootstrap?.meta.environment || props.bootstrap?.runtime.environment || '-' }}</dd></div>
            <div><dt>主人 QQ</dt><dd>{{ props.bootstrap?.meta.owner_qq || '-' }}</dd></div>
            <div><dt>WebUI</dt><dd>{{ props.bootstrap?.meta.webui_enabled ? '已启用' : '未启用' }}</dd></div>
          </dl>
        </article>
      </aside>
    </section>
  </section>
</template>

<style scoped>
.overview-panel {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.overview-summary-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 16px;
}

.overview-content-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.2fr) 360px;
  gap: 16px;
}

.overview-summary-card,
.overview-side-card,
.overview-event-card {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.overview-card-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.overview-card-head h3,
.overview-event-copy p,
.overview-health-item p {
  margin: 0;
}

.overview-kpi-list,
.overview-health-list,
.overview-side-column {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.overview-kpi-item {
  padding: 16px 18px;
  border-radius: 20px;
  background: var(--surface-soft);
  border: 1px solid var(--soft-border);
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.overview-kpi-item span,
.overview-kpi-item small,
.overview-health-item small {
  color: var(--text-soft);
}

.overview-kpi-item strong {
  font-size: 30px;
  line-height: 1.1;
  color: var(--text-primary);
}

.overview-event-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.overview-event-item {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  gap: 16px;
  padding: 16px 18px;
  border-radius: 20px;
  border: 1px solid var(--soft-border);
}

.overview-event-item.tone-blue {
  background: var(--info-bg-soft);
}

.overview-event-item.tone-green {
  background: #f0fdf4;
}

.overview-event-item.tone-amber {
  background: #fffbeb;
}

.overview-event-item.tone-red {
  background: #fef2f2;
}

.overview-event-copy {
  min-width: 0;
}

.overview-event-copy strong,
.overview-health-item strong {
  color: var(--text-primary);
}

.overview-event-copy p,
.overview-health-item p {
  margin-top: 6px;
  color: var(--text-soft);
  line-height: 1.55;
}

.overview-event-meta {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 8px;
}

.overview-event-meta small {
  color: var(--text-muted);
}

.overview-health-item {
  display: grid;
  gap: 8px;
  padding: 16px 18px;
  border-radius: 20px;
  border: 1px solid var(--soft-border);
  background: var(--surface-soft);
}

.overview-health-item div {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.compact-detail-list div {
  gap: 8px;
}

.compact-detail-list dt,
.compact-detail-list dd {
  min-width: 0;
}

@media (max-width: 1180px) {
  .overview-summary-grid,
  .overview-content-grid {
    grid-template-columns: 1fr;
  }
}
</style>
