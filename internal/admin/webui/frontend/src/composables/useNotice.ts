import { ref } from 'vue'

export type NoticeKind = 'success' | 'error' | 'info'

export interface NoticeState {
  kind: NoticeKind
  title: string
  text: string
}

export function useNotice() {
  const notice = ref<NoticeState | null>(null)

  function showNotice(kind: NoticeKind, title: string, text: string) {
    notice.value = { kind, title, text }
  }

  function clearNotice() {
    notice.value = null
  }

  return {
    notice,
    showNotice,
    clearNotice,
  }
}
