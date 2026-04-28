import type { APIErrorPayload } from '../types/api'

export class APIError extends Error {
  status: number
  payload: unknown

  constructor(status: number, message: string, payload: unknown) {
    super(message)
    this.name = 'APIError'
    this.status = status
    this.payload = payload
  }
}

function pickMessage(payload: unknown, fallback: string): string {
  if (!payload || typeof payload !== 'object') {
    return fallback
  }
  const value = payload as APIErrorPayload
  return String(value.detail || value.message || value.error || fallback)
}

async function decodeBody(response: Response): Promise<unknown> {
  const text = await response.text()
  if (!text) return null
  try {
    return JSON.parse(text)
  } catch {
    return text
  }
}

export async function requestJSON<T>(input: string, init?: RequestInit): Promise<T> {
  const response = await fetch(input, {
    credentials: 'same-origin',
    ...init,
    headers: {
      Accept: 'application/json',
      ...(init?.headers || {}),
    },
  })

  const payload = await decodeBody(response)
  if (!response.ok) {
    throw new APIError(response.status, pickMessage(payload, `HTTP ${response.status}`), payload)
  }
  return payload as T
}
