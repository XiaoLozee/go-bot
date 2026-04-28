<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import AuthPanel from './components/AuthPanel.vue'
import AdminWorkspace from './components/AdminWorkspace.vue'
import { APIError, requestJSON } from './lib/http'
import { readRuntimeConfig } from './lib/runtime-config'
import { DEFAULT_WEBUI_THEME } from './lib/webui-theme'
import type { AuthState } from './types/api'

const runtimeConfig = readRuntimeConfig()
const authState = ref<AuthState | null>(null)
const checkingAuth = ref(true)
const authBusy = ref(false)
const authError = ref('')
const workspaceKey = ref(0)

const authMode = computed<'login' | 'setup'>(() => (
  authState.value?.requires_setup ? 'setup' : 'login'
))
const activeTheme = computed(() => String(authState.value?.webui_theme || DEFAULT_WEBUI_THEME))

function formatError(error: unknown, fallback: string): string {
  if (error instanceof APIError) {
    return error.message || fallback
  }
  if (error instanceof Error) return error.message
  return fallback
}

async function loadAuthState() {
  checkingAuth.value = true
  authError.value = ''
  try {
    authState.value = await requestJSON<AuthState>(runtimeConfig.apiBasePath + '/auth/state')
  } catch (error) {
    authError.value = formatError(error, '加载管理后台认证状态失败。')
    authState.value = {
      enabled: true,
      configured: true,
      requires_setup: false,
      authenticated: false,
    }
  } finally {
    checkingAuth.value = false
  }
}

async function submitAuth(password: string) {
  authBusy.value = true
  authError.value = ''
  try {
    const endpoint = authMode.value === 'setup' ? '/auth/setup' : '/auth/login'
    await requestJSON<Record<string, unknown>>(runtimeConfig.apiBasePath + endpoint, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({ password }),
    })
    await loadAuthState()
    workspaceKey.value += 1
  } catch (error) {
    authError.value = formatError(error, '认证失败。')
  } finally {
    authBusy.value = false
  }
}

async function logout() {
  authBusy.value = true
  authError.value = ''
  try {
    await requestJSON<Record<string, unknown>>(runtimeConfig.apiBasePath + '/auth/logout', {
      method: 'POST',
    })
  } catch (error) {
    authError.value = formatError(error, '退出登录失败。')
  } finally {
    authBusy.value = false
    await loadAuthState()
  }
}

function handleUnauthorized() {
  authState.value = {
    ...(authState.value || {
      enabled: true,
      configured: true,
      requires_setup: false,
      authenticated: false,
    }),
    authenticated: false,
  }
  authError.value = '登录状态已失效，请重新登录。'
  workspaceKey.value += 1
}

function handleThemeChanged(theme: string) {
  if (!theme) return
  authState.value = {
    ...(authState.value || {
      enabled: true,
      configured: true,
      requires_setup: false,
      authenticated: true,
    }),
    webui_theme: theme,
  }
}

onMounted(async () => {
  await loadAuthState()
})
</script>

<template>
  <main class="page-shell" :class="{ 'is-workspace': authState?.authenticated }" :data-theme="activeTheme">
    <section v-if="checkingAuth" class="loading-card">
      <span class="eyebrow">Go-bot 管理后台</span>
      <h1>正在加载管理界面</h1>
      <p>正在检查登录状态并准备控制台。</p>
    </section>

    <AuthPanel
      v-else-if="!authState?.authenticated"
      :mode="authMode"
      :busy="authBusy"
      :error="authError"
      @submit="submitAuth"
    />

    <AdminWorkspace
      v-else
      :key="workspaceKey"
      :runtime-config="runtimeConfig"
      :auth-state="authState"
      @logout="logout"
      @theme-changed="handleThemeChanged"
      @unauthorized="handleUnauthorized"
    />
  </main>
</template>
