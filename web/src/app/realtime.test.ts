import { QueryClient, QueryObserver } from '@tanstack/vue-query'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { DefaultService } from '@/api/generated'
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
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 200 })))
  })
  it('creates exactly one connection and closes it', () => {
    startRealtime(client, 'device-a', vi.fn()); startRealtime(client, 'device-a', vi.fn())
    expect(sources).toHaveLength(1); expect(hasRealtimeConnection()).toBe(true)
    stopRealtime(); expect(sources[0].close).toHaveBeenCalled()
  })
  it('invalidates and refetches a second logical tab for an event from the same device', async () => {
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    const queryFn = vi.fn().mockResolvedValue({ id:'m1' })
    const observer = new QueryObserver(client, { queryKey:['messages', 'detail', 'm1'], queryFn })
    const unsubscribe = observer.subscribe(() => undefined)
    await vi.waitFor(() => expect(queryFn).toHaveBeenCalledTimes(1))
    startRealtime(client, 'device-a', vi.fn())
    // This client represents tab B; tab A emitted the mutation with their shared device ID.
    sources[0].emit('message.updated', JSON.stringify({ type: 'message.updated', resourceId: 'm1', originDeviceId: 'device-a' }))
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['messages', 'detail', 'm1'] })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['messages'] })
    await vi.waitFor(() => expect(queryFn.mock.calls.length).toBeGreaterThan(1))
    unsubscribe()
  })
  it('ignores malformed events', () => {
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    startRealtime(client, 'device-a', vi.fn())
    const count = invalidate.mock.calls.length
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
  it('uses a short-lived no-touch events probe and only expires auth on 401', async () => {
    const expired = vi.fn()
    const getAuthSession = vi.spyOn(DefaultService, 'getAuthSession')
    const aborts: AbortSignal[] = []
    vi.mocked(fetch).mockImplementationOnce(async (_input, init) => {
      aborts.push(init!.signal!); return new Response(null, { status: 401 })
    })
    startRealtime(client, 'device-a', expired); sources[0].emit('error'); sources[0].emit('error')
    await Promise.resolve(); await Promise.resolve()
    expect(expired).toHaveBeenCalledTimes(1)
    expect(fetch).toHaveBeenCalledWith('/api/v1/events', expect.objectContaining({ credentials:'include', signal:expect.any(AbortSignal) }))
    expect(aborts[0].aborted).toBe(true)
    stopRealtime(); vi.mocked(fetch).mockRejectedValueOnce(new TypeError('offline'))
    startRealtime(client, 'device-a', expired); sources[1].emit('error')
    await Promise.resolve(); await Promise.resolve()
    expect(expired).toHaveBeenCalledTimes(1)
    expect(getAuthSession).not.toHaveBeenCalled()
  })
})
