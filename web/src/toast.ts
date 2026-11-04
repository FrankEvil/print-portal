import { reactive } from 'vue'

export interface ToastItem {
  id: number
  kind: 'success' | 'error' | 'info'
  message: string
}

export const toasts = reactive<ToastItem[]>([])

let nextId = 1

function push(kind: ToastItem['kind'], message: string) {
  const id = nextId++
  toasts.push({ id, kind, message })
  setTimeout(() => {
    const idx = toasts.findIndex((t) => t.id === id)
    if (idx >= 0) toasts.splice(idx, 1)
  }, 4500)
}

export const toast = {
  success: (message: string) => push('success', message),
  error: (message: string) => push('error', message),
  info: (message: string) => push('info', message),
}
