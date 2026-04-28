import type { RuntimeConfig } from '../types/api'

declare global {
  interface Window {
    __GOBOT_WEBUI__?: {
      basePath?: string
      vueBasePath?: string
    }
  }
}

function normalizePath(input: string | undefined): string {
  let value = String(input || '').trim()
  if (!value) return '/'
  if (!value.startsWith('/')) {
    value = '/' + value
  }
  if (value !== '/') {
    value = value.replace(/\/+$/, '')
  }
  return value || '/'
}

function resolveAPIBasePrefix(pathname: string, configuredVueBasePath: string): string {
  const normalizedVueBasePath = normalizePath(configuredVueBasePath)
  let currentPath = String(pathname || '').trim()
  if (!currentPath) return ''
  if (!currentPath.startsWith('/')) {
    currentPath = '/' + currentPath
  }
  if (currentPath !== '/') {
    currentPath = currentPath.replace(/\/+$/, '')
  }
  if (currentPath === normalizedVueBasePath) {
    return ''
  }
  if (!currentPath.endsWith(normalizedVueBasePath)) {
    return ''
  }
  const prefix = currentPath.slice(0, currentPath.length - normalizedVueBasePath.length).replace(/\/+$/, '')
  return prefix === '/' ? '' : prefix
}

function deriveBasePath(pathname: string): string {
  let currentPath = String(pathname || '').trim()
  if (!currentPath) return '/'
  if (!currentPath.startsWith('/')) {
    currentPath = '/' + currentPath
  }
  if (currentPath !== '/') {
    currentPath = currentPath.replace(/\/+$/, '')
  }
  if (currentPath === '/' || !currentPath) {
    return '/'
  }
  const lastSegment = currentPath.split('/').pop() || ''
  if (lastSegment.includes('.')) {
    const parentPath = currentPath.slice(0, currentPath.length - lastSegment.length - 1)
    return normalizePath(parentPath || '/')
  }
  return normalizePath(currentPath)
}

export function readRuntimeConfig(): RuntimeConfig {
  const injected = window.__GOBOT_WEBUI__ || {}
  const basePath = normalizePath(injected.basePath || deriveBasePath(window.location.pathname))
  const vueBasePath = normalizePath(injected.vueBasePath || basePath)
  const apiBasePrefix = resolveAPIBasePrefix(window.location.pathname, vueBasePath)

  return {
    basePath,
    vueBasePath,
    apiBasePrefix,
    apiBasePath: apiBasePrefix + '/api/admin',
  }
}
