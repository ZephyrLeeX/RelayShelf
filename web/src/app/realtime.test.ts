import { QueryClient } from '@tanstack/vue-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, DefaultService } from '@/api/generated'
import { hasRealtimeConnection, setEventSourceFactory, startRealtime, stopRealtime } from './realtime'

class FakeEventSource {
  listeners = new Map<string, EventListener[]>()
  close = vi.fn()
  addEventListener(type: string, listener: EventListenerOrEventListenerObject) {
    const callback = typeof listener === 'function' ? listener : listener.handleEvent.bind(listener)
    this.listeners.set(type, [...(this.listeners.get(type) ?? []), callback])
  }
  emit(type: string, data = '') {
    for (const listener of this.listeners.get(type) ?? []) listener(type === 'open' || type === 'error' ? new Event(type) : new MessageEvent(type, { data }))
  }
}

describe('realtime singleton', () => {
  let sources: FakeEventSource[]
  let client: QueryClient
  beforeEach(() => {
    stopRealtime(); sources = []; client = new QueryClient()
    setEventSourceFactory(() => { const source = new FakeEventSource(); sources.push(source); return source as unknown as EventSource })
    vi.spyOn(DefaultService, 'getAuthSession').mockResolvedValue({} as never)
  })
  it('creates exactly one connection and closes it', () => {
    startRealtime(client, 'device-a', vi.fn()); startRealtime(client, 'device-a', vi.fn())
    expect(sources).toHaveLength(1); expect(hasRealtimeConnection()).toBe(true)
    stopRealtime(); expect(sources[0].close).toHaveBeenCalled()
  })
  it('invalidates message and tag truth, ignoring own-origin and malformed events', () => {
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    startRealtime(client, 'device-a', vi.fn())
    sources[0].emit('message.updated', JSON.stringify({ type: 'message.updated', resourceId: 'm1', originDeviceId: 'device-b' }))
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['messages', 'detail', 'm1'] })
    const count = invalidate.mock.calls.length
    sources[0].emit('message.updated', JSON.stringify({ type: 'message.updated', resourceId: 'm1', originDeviceId: 'device-a' }))
    sources[0].emit('tag.updated', '{bad-json')
    expect(invalidate).toHaveBeenCalledTimes(count)
  })
  it('selectively refreshes after reconnect and on visible', () => {
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    startRealtime(client, 'device-a', vi.fn())
    sources[0].emit('error'); sources[0].emit('open')
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['messages'] })
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' })
    document.dispatchEvent(new Event('visibilitychange'))
    expect(invalidate.mock.calls.length).toBeGreaterThan(4)
  })
  it('expires auth on a session 401 but keeps auth on network failure', async () => {
    const expired = vi.fn()
    vi.mocked(DefaultService.getAuthSession).mockRejectedValueOnce(new ApiError(
      { method:'GET', url:'/auth/session' },
      { url:'/api/v1/auth/session', ok:false, status:401, statusText:'Unauthorized', body:{ code:'AUTH_SESSION_EXPIRED', message:'expired', traceId:'t' } },
      'expired',
    ))
    startRealtime(client, 'device-a', expired); sources[0].emit('error')
    await Promise.resolve(); await Promise.resolve()
    expect(expired).toHaveBeenCalledTimes(1)
    stopRealtime(); vi.mocked(DefaultService.getAuthSession).mockRejectedValueOnce(new TypeError('offline'))
    startRealtime(client, 'device-a', expired); sources[1].emit('error')
    await Promise.resolve(); await Promise.resolve()
    expect(expired).toHaveBeenCalledTimes(1)
  })
})
