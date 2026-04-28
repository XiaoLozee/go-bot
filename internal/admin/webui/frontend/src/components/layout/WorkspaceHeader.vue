<script setup lang="ts">
import type { WebUIBootstrap } from '../../types/api'

type WorkspaceNavItem = {
  value: string
  label: string
  icon: string
  subtitle: string
  kicker: string
  heroTitle: string
  heroCopy: string
}

const props = defineProps<{
  activeViewMeta: WorkspaceNavItem
  bootstrap: WebUIBootstrap | null
  heroPills: Array<{ label: string; value: string }>
  heroMetrics: Array<{ label: string; value: string; note: string }>
  showOverviewMeta: boolean
  loadingBootstrap: boolean
  actionBusy: boolean
}>()

const emit = defineEmits<{
  refresh: []
  logout: []
}>()

function formatDateTime(value?: string): string {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}
</script>

<template>
  <div class="workspace-header-wrapper">
    <header class="workspace-topbar">
      <div class="workspace-heading">
        <span class="workspace-chip">{{ activeViewMeta.kicker }}</span>
        <h1>{{ activeViewMeta.label }}</h1>
        <p>{{ activeViewMeta.subtitle }}</p>
      </div>

      <div class="workspace-header-actions">
        <span v-if="props.showOverviewMeta" class="main-status">
          <span class="status-dot" aria-hidden="true"></span>
          最近快照：{{ formatDateTime(bootstrap?.generated_at) }}
        </span>
        <div class="workspace-action-row">
          <button class="secondary-btn" type="button" :disabled="loadingBootstrap || actionBusy" @click="emit('refresh')">
            {{ loadingBootstrap ? '刷新中...' : '刷新快照' }}
          </button>
          <button class="primary-btn" type="button" :disabled="actionBusy" @click="emit('logout')">
            退出登录
          </button>
        </div>
      </div>
    </header>

    <section class="workspace-hero-card" :class="{ 'workspace-hero-card-solo': !props.showOverviewMeta }">
      <div class="workspace-hero-main">
        <span class="hero-kicker">{{ activeViewMeta.kicker }}</span>
        <h2>{{ activeViewMeta.heroTitle }}</h2>
        <p>{{ activeViewMeta.heroCopy }}</p>

        <div v-if="props.showOverviewMeta && heroPills.length" class="hero-pill-list">
          <span v-for="pill in heroPills" :key="pill.label" class="hero-pill">
            {{ pill.label }}：{{ pill.value }}
          </span>
        </div>
      </div>

      <aside v-if="props.showOverviewMeta" class="workspace-hero-status">
        <div class="hero-status-head">
          <span class="hero-status-label">当前工作区</span>
          <span class="hero-status-chip">{{ activeViewMeta.label }}</span>
        </div>
        <strong>{{ bootstrap?.runtime.app_name || bootstrap?.meta.app_name || 'Go-bot' }}</strong>
        <p>
          {{ bootstrap?.runtime.state || '未知状态' }}
          ·
          {{ bootstrap?.runtime.environment || bootstrap?.meta.environment || '未设置环境' }}
        </p>

        <div class="hero-metric-grid">
          <article v-for="metric in heroMetrics" :key="metric.label" class="hero-metric">
            <span>{{ metric.label }}</span>
            <strong>{{ metric.value }}</strong>
            <small>{{ metric.note }}</small>
          </article>
        </div>
      </aside>
    </section>
  </div>
</template>

<style scoped>
.workspace-header-wrapper {
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.workspace-topbar {
  display: flex;
  justify-content: space-between;
  align-items: flex-start;
  gap: 18px;
}

.workspace-heading {
  min-width: 0;
}

.workspace-chip {
  display: inline-flex;
  align-items: center;
  height: 32px;
  padding: 0 14px;
  border-radius: 999px;
  border: 1px solid var(--accent-border);
  background: var(--chip-bg);
  color: var(--accent-strong);
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.04em;
}

.workspace-heading h1,
.workspace-hero-main h2,
.workspace-hero-status strong {
  margin: 0;
}

.workspace-heading h1 {
  margin-top: 10px;
  font-size: clamp(30px, 4vw, 38px);
  line-height: 1.06;
  color: var(--text-primary);
}

.workspace-heading p,
.workspace-hero-main p,
.workspace-hero-status p,
.hero-metric small {
  margin: 0;
  line-height: 1.6;
}

.workspace-heading p {
  margin-top: 8px;
  color: var(--text-soft);
}

.workspace-hero-main p,
.workspace-hero-status p,
.hero-metric small {
  color: var(--hero-soft-text);
}

.workspace-header-actions {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 12px;
}

.workspace-action-row,
.hero-pill-list {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.main-status,
.hero-pill,
.hero-status-chip {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-height: 36px;
  padding: 0 14px;
  border-radius: 999px;
  font-size: 13px;
}

.main-status {
  background: var(--surface-soft-deep);
  border: 1px solid var(--card-border);
  color: var(--text-primary);
}

.status-dot {
  width: 8px;
  height: 8px;
  border-radius: 999px;
  background: var(--status-dot);
}

.workspace-hero-card {
  display: grid;
  grid-template-columns: minmax(0, 1.35fr) minmax(320px, 0.85fr);
  gap: 18px;
  padding: 20px;
  border-radius: 28px;
  background: var(--hero-gradient);
  color: #ffffff;
  box-shadow: var(--hero-shadow);
}

.workspace-hero-card.workspace-hero-card-solo {
  grid-template-columns: minmax(0, 1fr);
}

.workspace-hero-main {
  min-width: 0;
  padding: 8px 8px 8px 4px;
}

.hero-kicker,
.hero-status-label {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  color: var(--hero-kicker);
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}

.workspace-hero-main h2 {
  margin-top: 12px;
  font-size: clamp(28px, 3.5vw, 36px);
  line-height: 1.1;
}

.workspace-hero-main p {
  margin-top: 10px;
  max-width: 720px;
}

.hero-pill-list {
  margin-top: 18px;
}

.hero-pill {
  background: var(--hero-pill-bg);
  border: 1px solid var(--hero-pill-border);
  color: var(--hero-pill-text);
}

.workspace-hero-status {
  display: flex;
  flex-direction: column;
  gap: 14px;
  min-width: 0;
  border-radius: 22px;
  border: 1px solid var(--hero-panel-border);
  background: var(--hero-panel-bg);
  padding: 18px;
}

.hero-status-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
}

.hero-status-chip {
  border: 1px solid var(--hero-pill-border);
  background: var(--hero-panel-surface);
  color: var(--hero-pill-text);
}

.workspace-hero-status strong {
  font-size: 24px;
  line-height: 1.2;
}

.hero-metric-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 10px;
}

.hero-metric {
  display: flex;
  flex-direction: column;
  gap: 4px;
  min-width: 0;
  padding: 14px;
  border-radius: 18px;
  background: var(--hero-panel-surface);
}

.hero-metric span {
  color: var(--hero-kicker);
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.04em;
}

.hero-metric strong {
  font-size: 20px;
  line-height: 1.2;
  word-break: break-word;
}

@media (max-width: 1240px) {
  .workspace-hero-card {
    grid-template-columns: 1fr;
  }
}

@media (max-width: 860px) {
  .workspace-topbar,
  .workspace-header-actions {
    flex-direction: column;
    align-items: stretch;
  }

  .workspace-header-actions {
    gap: 10px;
  }

  .hero-metric-grid {
    grid-template-columns: 1fr;
  }
}
</style>
