export type WebUIThemeOption = {
  value: string
  label: string
  note: string
  preview: string
}

export const DEFAULT_WEBUI_THEME = 'blue-light'

export const webUIThemeOptions: WebUIThemeOption[] = [
  {
    value: 'blue-light',
    label: '蓝白',
    note: '清爽稳定',
    preview: 'linear-gradient(135deg, #2563eb 0%, #60a5fa 46%, #ffffff 100%)',
  },
  {
    value: 'pink-light',
    label: '粉白',
    note: '日系春樱',
    preview: 'linear-gradient(135deg, #eab1c8 0%, #fce9f0 46%, #ffffff 100%)',
  },
  {
    value: 'emerald-light',
    label: '绿白',
    note: '通透自然',
    preview: 'linear-gradient(135deg, #10b981 0%, #6ee7b7 46%, #ffffff 100%)',
  },
  {
    value: 'violet-light',
    label: '紫白',
    note: '冷静科技',
    preview: 'linear-gradient(135deg, #8b5cf6 0%, #c4b5fd 46%, #ffffff 100%)',
  },
  {
    value: 'amber-light',
    label: '橙白',
    note: '暖色醒目',
    preview: 'linear-gradient(135deg, #f59e0b 0%, #fcd34d 46%, #ffffff 100%)',
  },
  {
    value: 'neutral-light',
    label: '灰白',
    note: '中性克制',
    preview: 'linear-gradient(135deg, #64748b 0%, #cbd5e1 46%, #ffffff 100%)',
  },
]

export function formatWebUIThemeLabel(value?: string): string {
  const theme = String(value || '').trim()
  return webUIThemeOptions.find((item) => item.value === theme)?.label || theme || '未设置'
}

export function getWebUIThemeOption(value?: string): WebUIThemeOption | null {
  const theme = String(value || '').trim()
  return webUIThemeOptions.find((item) => item.value === theme) || null
}
