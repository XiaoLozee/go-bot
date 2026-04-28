<script setup lang="ts">
import { computed } from 'vue'
import { formatWebUIThemeLabel } from '../../lib/webui-theme'
import type { AuthState, WebUIBootstrap } from '../../types/api'

type WorkspaceView = 'overview' | 'ai' | 'plugins' | 'plugin-api' | 'connections' | 'audit' | 'config'
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
  activeView: WorkspaceView
  navItems: WorkspaceNavItem[]
  bootstrap: WebUIBootstrap | null
  authState: AuthState | null
}>()

const emit = defineEmits<{
  'update:activeView': [value: WorkspaceView]
}>()

const currentTheme = computed(() => {
  const value = props.authState?.webui_theme || props.bootstrap?.meta.webui_theme || 'blue-light'
  return formatWebUIThemeLabel(value)
})
</script>

<template>
  <aside class="workspace-sidebar">
    <div class="sidebar-brand">
      <div class="brand-mark">G</div>
      <div class="brand-copy">
        <strong>{{ bootstrap?.runtime.app_name || bootstrap?.meta.app_name || 'Go-bot' }}</strong>
        <span>内部管理后台</span>
      </div>
    </div>

    <div class="sidebar-section-title">控制台</div>

    <nav class="sidebar-nav" aria-label="主导航">
      <button
        v-for="item in navItems"
        :key="item.value"
        class="sidebar-tab"
        :class="{ active: activeView === item.value }"
        type="button"
        @click="emit('update:activeView', item.value)"
      >
        <span class="sidebar-tab-icon" aria-hidden="true">{{ item.icon }}</span>
        <span class="sidebar-tab-copy">
          <strong>{{ item.label }}</strong>
          <small>{{ item.subtitle }}</small>
        </span>
        <span class="sidebar-tab-arrow" aria-hidden="true">›</span>
      </button>
    </nav>

    <footer class="sidebar-footer">
      <div class="sidebar-footer-copy">
        <strong>Admin Console</strong>
        <span>内部管理后台</span>
      </div>
      <small>{{ currentTheme }}</small>
    </footer>
  </aside>
</template>

<style scoped>
.workspace-sidebar {
  position: fixed;
  inset: 0 auto 0 0;
  width: var(--workspace-sidebar-width);
  overflow-y: auto;
  overscroll-behavior: contain;
  border: none;
  border-right: 1px solid var(--sidebar-border);
  background: var(--sidebar-bg);
  padding: 28px 18px 20px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  z-index: 4;
  box-shadow: 10px 0 30px rgba(15, 23, 42, 0.04);
}

.sidebar-brand,
.sidebar-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.sidebar-brand {
  padding: 4px 8px 16px;
}

.brand-mark {
  width: 40px;
  height: 40px;
  border-radius: 14px;
  background: var(--brand-gradient);
  color: #ffffff;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 16px;
  font-weight: 800;
  flex: none;
}

.brand-copy {
  min-width: 0;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.brand-copy strong,
.sidebar-footer-copy strong {
  color: var(--text-primary);
  font-size: 14px;
}

.brand-copy span,
.sidebar-footer-copy span,
.sidebar-footer small {
  color: var(--text-soft);
  font-size: 12px;
}

.sidebar-section-title {
  padding: 0 10px 6px;
  color: var(--sidebar-section-text);
  font-size: 13px;
  font-weight: 700;
}

.sidebar-nav {
  display: flex;
  flex-direction: column;
  gap: 4px;
  flex: 1;
}

.sidebar-tab {
  width: 100%;
  display: grid;
  grid-template-columns: 40px minmax(0, 1fr) auto;
  gap: 12px;
  align-items: center;
  padding: 12px 14px;
  border: 1px solid transparent;
  border-radius: 18px;
  background: transparent;
  color: var(--text-secondary);
  text-align: left;
  transition: background 0.18s ease, border-color 0.18s ease, color 0.18s ease, box-shadow 0.18s ease;
}

.sidebar-tab:hover {
  background: var(--sidebar-hover-bg);
}

.sidebar-tab.active {
  background: var(--selection-bg);
  border-color: var(--selection-border);
  color: var(--accent-strong);
  box-shadow: inset 0 0 0 1px var(--selection-shadow);
}

.sidebar-tab-icon {
  width: 40px;
  height: 40px;
  border-radius: 14px;
  border: 1px solid var(--soft-border);
  background: var(--card-bg);
  color: var(--text-muted);
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  font-weight: 700;
}

.sidebar-tab.active .sidebar-tab-icon {
  border-color: transparent;
  background: var(--brand-gradient);
  color: #ffffff;
}

.sidebar-tab-copy {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.sidebar-tab-copy strong {
  color: currentColor;
  font-size: 14px;
}

.sidebar-tab-copy small {
  color: var(--text-soft);
  font-size: 12px;
  line-height: 1.45;
}

.sidebar-tab-arrow {
  color: var(--text-muted);
  font-size: 16px;
}

.sidebar-footer {
  padding: 14px 10px 8px;
  border-top: 1px solid var(--soft-divider);
}

.sidebar-footer-copy {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

@media (max-width: 1120px) {
  .workspace-sidebar {
    position: static;
    inset: auto;
    width: 100%;
    min-height: 0;
    border: 1px solid var(--sidebar-border);
    border-radius: 24px;
    box-shadow: none;
    padding: 20px 16px;
    z-index: auto;
  }

  .sidebar-nav {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}

@media (max-width: 720px) {
  .sidebar-nav {
    grid-template-columns: 1fr;
  }
}
</style>
