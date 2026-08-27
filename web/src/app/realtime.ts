import type { QueryClient } from '@tanstack/vue-query'
import { DefaultService } from '@/api/generated'
import { isAuthExpired } from '@/shared/api/errors'
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

export function startRealtime(client: QueryClient, deviceId: string, onExpired: () => void) {
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
        if (event.originDeviceId === deviceId) return
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
    void DefaultService.getAuthSession()
      .catch((error: unknown) => { if (isAuthExpired(error)) onExpired() })
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
