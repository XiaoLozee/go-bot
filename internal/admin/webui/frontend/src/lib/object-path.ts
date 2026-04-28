export function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === 'object' && !Array.isArray(value)
}

export function cloneRecord(value: unknown): Record<string, unknown> {
  if (!isRecord(value)) return {}
  return JSON.parse(JSON.stringify(value)) as Record<string, unknown>
}

export function ensureRecord(parent: Record<string, unknown>, key: string): Record<string, unknown> {
  const current = parent[key]
  if (isRecord(current)) return current
  const next: Record<string, unknown> = {}
  parent[key] = next
  return next
}

export function getPath(value: Record<string, unknown>, path: string, fallback: unknown = ''): unknown {
  const parts = path.split('.').filter(Boolean)
  let cursor: unknown = value
  for (const part of parts) {
    if (!isRecord(cursor) || !Object.prototype.hasOwnProperty.call(cursor, part)) return fallback
    cursor = cursor[part]
  }
  return cursor ?? fallback
}

export function setPath(value: Record<string, unknown>, path: string, nextValue: unknown) {
  const parts = path.split('.').filter(Boolean)
  if (!parts.length) return
  let cursor = value
  parts.slice(0, -1).forEach((part) => {
    cursor = ensureRecord(cursor, part)
  })
  cursor[parts[parts.length - 1]] = nextValue
}

export function getString(value: Record<string, unknown>, path: string): string {
  const raw = getPath(value, path, '')
  return raw == null ? '' : String(raw)
}

export function getNumber(value: Record<string, unknown>, path: string): number {
  const raw = getPath(value, path, 0)
  const number = Number(raw)
  return Number.isFinite(number) ? number : 0
}

export function getBoolean(value: Record<string, unknown>, path: string): boolean {
  return !!getPath(value, path, false)
}
