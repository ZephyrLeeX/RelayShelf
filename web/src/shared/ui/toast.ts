import { readonly, ref } from 'vue'

export type ToastType = 'success' | 'info' | 'warning' | 'error'

export interface ToastItem {
  id: number
  type: ToastType
  message: string
}

const items = ref<ToastItem[]>([])
const timers = new Map<number, number>()
let nextId = 0

function dismiss(id: number) {
  items.value = items.value.filter((item) => item.id !== id)
  const timer = timers.get(id)
  if (timer !== undefined) window.clearTimeout(timer)
  timers.delete(id)
}

function clear() {
  for (const timer of timers.values()) window.clearTimeout(timer)
  timers.clear()
  items.value = []
}

function show(type: ToastType, message: string, duration = 3_200) {
  const id = ++nextId
  items.value.push({ id, type, message })
  if (duration > 0) timers.set(id, window.setTimeout(() => dismiss(id), duration))
  return id
}

export const toast = {
  items: readonly(items),
  dismiss,
  clear,
  success: (message: string, duration?: number) => show('success', message, duration),
  info: (message: string, duration?: number) => show('info', message, duration),
  warning: (message: string, duration?: number) => show('warning', message, duration),
  error: (message: string, duration?: number) => show('error', message, duration),
}
