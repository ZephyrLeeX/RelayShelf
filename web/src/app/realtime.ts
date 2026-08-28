import type { QueryClient } from '@tanstack/vue-query'
import { queryKeys } from '@/shared/api/queryKeys'

interface RealtimeEvent {
  type: string
  resourceId?: string
  originDeviceId?: string
}

type EventSourceFactory = (url: string, init: EventSourceInit) => EventSource

let source: EventSource | undefined
let factory: EventSourceFactory = (url, init) => new EventSource(url, init)
let wasDisconnected = false
let authCheckPending = false
let visibilityHandler: (() => void) | undefined

export function setEventSourceFactory(next: EventSourceFactory) {
  factory = next
}

function invalidateServerTruth(client: QueryClient) {
  void client.invalidateQueries({ queryKey: queryKeys.messages.root() })
  void client.invalidateQueries({ queryKey: queryKeys.tags.all() })
  void client.invalidateQueries({ queryKey: queryKeys.search.root() })
  void client.invalidateQueries({ queryKey: queryKeys.trash.list() })
}

async function probeRealtimeAuth(onExpired: () => void) {
  const controller = new AbortController()
  try {
    const response = await fetch('/api/v1/events', {
      credentials: 'include',
      signal: controller.signal,
    })
    if (response.status === 401) onExpired()
  } catch {
    // A network failure does not prove that the authenticated session expired.
  } finally {
    // The SSE endpoint flushes its status immediately. Do not leave the probe open.
    controller.abort()
  }
}

export function startRealtime(client: QueryClient, _deviceId: string, onExpired: () => void) {
  if (source) return
  const next = factory('/api/v1/events', { withCredentials: true })
  source = next
  next.addEventListener('open', () => {
    if (wasDisconnected) invalidateServerTruth(client)
    wasDisconnected = false
  })
  const eventTypes = ['message.created', 'message.updated', 'message.deleted', 'tag.created', 'tag.updated', 'tag.deleted']
  for (const eventType of eventTypes) {
    next.addEventListener(eventType, (frame) => {
      try {
        const event = JSON.parse((frame as MessageEvent<string>).data) as RealtimeEvent
        if (event.type.startsWith('tag.')) {
          void client.invalidateQueries({ queryKey: queryKeys.tags.all() })
        }
        if (event.type.startsWith('message.') && event.resourceId) {
          void client.invalidateQueries({ queryKey: queryKeys.messages.detail(event.resourceId) })
        }
        invalidateServerTruth(client)
      } catch {
        // A malformed hint is safely ignored; the API remains the source of truth.
      }
    })
  }
  next.addEventListener('error', () => {
    wasDisconnected = true
    if (authCheckPending) return
    authCheckPending = true
    void probeRealtimeAuth(onExpired)
      .finally(() => { authCheckPending = false })
  })
  visibilityHandler = () => {
    if (document.visibilityState === 'visible') invalidateServerTruth(client)
  }
  document.addEventListener('visibilitychange', visibilityHandler)
}

export function stopRealtime() {
  source?.close()
  source = undefined
  wasDisconnected = false
  authCheckPending = false
  if (visibilityHandler) document.removeEventListener('visibilitychange', visibilityHandler)
  visibilityHandler = undefined
}

export function hasRealtimeConnection() {
  return Boolean(source)
}
