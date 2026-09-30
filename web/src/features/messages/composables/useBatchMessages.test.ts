import { mount } from '@vue/test-utils'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { defineComponent, nextTick, ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { CancelablePromise } from '@/api/generated/core/CancelablePromise'
import { DefaultService, Lifecycle } from '@/api/generated'
import { queryKeys, type MessageFilters } from '@/shared/api/queryKeys'
import { messageFixture } from '@/test/fixtures'
import { useBatchMessages } from './useBatchMessages'

function setup(items = [messageFixture()]) {
  let batch!: ReturnType<typeof useBatchMessages>
  const loaded = ref(items)
  const filters = ref<MessageFilters>({})
  const client = new QueryClient()
  const wrapper = mount(defineComponent({ setup() { batch = useBatchMessages(loaded, () => filters.value); return () => null } }), {
    global: { plugins: [[VueQueryPlugin, { queryClient: client }]] },
  })
  batch.active.value = true
  batch.selectLoaded()
  return { batch, wrapper, filters, client, loaded }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
const tag = (id: string) => ({ id, name: id, color: '#123456', createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z' })

describe('batch commands', () => {
  it('appends and deduplicates tags using selected expectedVersion without reading sensitive bodies', async () => {
    const message = messageFixture({ sensitive: true, version: 4, tags: [tag('existing')] })
    const replace = vi.spyOn(DefaultService, 'replaceMessageTags').mockResolvedValue(message)
    const get = vi.spyOn(DefaultService, 'getMessage')
    const { batch, wrapper } = setup([message])
    await batch.run('tags', ['existing', 'new'])
    expect(replace).toHaveBeenCalledWith(message.id, { expectedVersion: 4, tagIds: ['existing', 'new'] })
    expect(get).not.toHaveBeenCalled()
    expect(batch.count.value).toBe(0)
    wrapper.unmount()
  })

  it('retains partial failures and explicitly retries only failures with latest tags and version', async () => {
    const a = messageFixture({ id: 'a' })
    const b = messageFixture({ id: 'b' })
    const replace = vi.spyOn(DefaultService, 'replaceMessageTags').mockResolvedValue(a)
    replace.mockImplementationOnce(() => new CancelablePromise(resolve => resolve(a))).mockRejectedValueOnce({ code: 'MESSAGE_VERSION_CONFLICT' })
    const get = vi.spyOn(DefaultService, 'getMessage').mockResolvedValue({ ...b, version: 9, tags: [tag('concurrent')] })
    const { batch, wrapper } = setup([a, b])
    await batch.run('tags', ['added'])
    expect(batch.succeeded.value).toBe(1)
    expect(batch.completed.value).toBe(2)
    expect(batch.failures.value[0]?.error).toContain('其他设备修改')
    expect([...batch.selected.value.keys()]).toEqual(['b'])
    await batch.retryFailed()
    expect(get).toHaveBeenCalledExactlyOnceWith('b')
    expect(replace).toHaveBeenCalledTimes(3)
    expect(replace).toHaveBeenLastCalledWith('b', { expectedVersion: 9, tagIds: ['concurrent', 'added'] })
    expect(batch.failures.value).toHaveLength(0)
    wrapper.unmount()
  })

  it('limits concurrency to three, reports progress, and restores through the single-item API', async () => {
    const items = Array.from({ length: 7 }, (_, i) => messageFixture({ id: String(i), version: i + 1 }))
    let running = 0
    let peak = 0
    const restore = vi.spyOn(DefaultService, 'restoreMessage').mockImplementation(() => new CancelablePromise(resolve => {
      peak = Math.max(peak, ++running)
      setTimeout(() => { running--; resolve(items[0]!) }, 5)
    }))
    const { batch, wrapper } = setup(items)
    const operation = batch.run('restore')
    expect(batch.busy.value).toBe(true)
    expect(restore).toHaveBeenCalledTimes(3)
    await operation
    expect(peak).toBe(3)
    expect(batch.completed.value).toBe(7)
    expect(batch.count.value).toBe(0)
    expect(restore).toHaveBeenCalledWith('6', { expectedVersion: 7 })
    wrapper.unmount()
  })

  it('stops queued requests after the feed is left', async () => {
    let resolve!: (value: ReturnType<typeof messageFixture>) => void
    const pending = new Promise<ReturnType<typeof messageFixture>>(done => { resolve = done })
    const trash = vi.spyOn(DefaultService, 'trashMessage').mockReturnValue(pending as never)
    const { batch, wrapper } = setup(Array.from({ length: 5 }, (_, i) => messageFixture({ id: String(i) })))
    const operation = batch.run('trash')
    wrapper.unmount()
    resolve(messageFixture())
    await operation
    expect(trash).toHaveBeenCalledTimes(3)
  })

  it.each(['filter', 'unmount'] as const)('does not write after a retry metadata read is cancelled by %s', async (cancel) => {
    const message = messageFixture()
    const trash = vi.spyOn(DefaultService, 'trashMessage').mockRejectedValue({ code: 'MESSAGE_VERSION_CONFLICT' })
    const metadata = deferred<ReturnType<typeof messageFixture>>()
    const get = vi.spyOn(DefaultService, 'getMessage').mockReturnValue(metadata.promise as never)
    const { batch, wrapper, filters } = setup([message])
    await batch.run('trash')
    const retry = batch.retryFailed()
    expect(get).toHaveBeenCalledExactlyOnceWith(message.id)
    if (cancel === 'filter') {
      filters.value = { lifecycle: Lifecycle.PERMANENT }
      await nextTick()
    } else wrapper.unmount()
    metadata.resolve(messageFixture({ version: 2 }))
    await retry
    expect(trash).toHaveBeenCalledTimes(1)
    expect(batch.busy.value).toBe(false)
    expect(batch.failures.value).toEqual([])
    if (cancel === 'filter') wrapper.unmount()
  })

  it('waits for delayed list refresh before selecting and writing the next batch', async () => {
    const original = messageFixture({ version: 4 })
    const updated = messageFixture({ version: 5, lifecycle: Lifecycle.PERMANENT })
    const permanent = vi.spyOn(DefaultService, 'makeMessagePermanent').mockResolvedValue(updated)
    const trash = vi.spyOn(DefaultService, 'trashMessage').mockResolvedValue({ ...updated, version: 6 })
    const { batch, wrapper, client, loaded } = setup([original])
    const refresh = deferred<void>()
    const invalidate = vi.spyOn(client, 'invalidateQueries').mockImplementation((options) => {
      if (JSON.stringify(options?.queryKey) === JSON.stringify(queryKeys.messages.lists())) return refresh.promise
      return Promise.resolve()
    })
    const operation = batch.run('permanent')
    await vi.waitFor(() => expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.messages.lists() }))
    expect(batch.busy.value).toBe(true)
    batch.selectLoaded()
    await batch.run('trash')
    expect(batch.count.value).toBe(0)
    expect(trash).not.toHaveBeenCalled()
    loaded.value = [updated]
    refresh.resolve()
    await operation
    expect(batch.busy.value).toBe(false)
    batch.selectLoaded()
    await batch.run('trash')
    expect(permanent).toHaveBeenCalledExactlyOnceWith(original.id, { expectedVersion: 4 })
    expect(trash).toHaveBeenCalledExactlyOnceWith(original.id, { expectedVersion: 5 })
    wrapper.unmount()
  })

  it.each(['loaded', 'toggle'] as const)('uses successful metadata for %s selection when refresh fails and still reports remote conflicts', async (selection) => {
    const original = messageFixture({ version: 4 })
    const updated = messageFixture({ version: 5, lifecycle: Lifecycle.PERMANENT, tags: [tag('latest')] })
    vi.spyOn(DefaultService, 'makeMessagePermanent').mockResolvedValue(updated)
    const replace = vi.spyOn(DefaultService, 'replaceMessageTags').mockRejectedValue({ code: 'MESSAGE_VERSION_CONFLICT' })
    const { batch, wrapper, client } = setup([original])
    vi.spyOn(client, 'invalidateQueries').mockRejectedValue(new Error('offline'))
    await batch.run('permanent')
    expect(batch.busy.value).toBe(false)
    if (selection === 'loaded') batch.selectLoaded()
    else batch.toggle(original)
    await batch.run('tags', ['added'])
    expect(replace).toHaveBeenCalledExactlyOnceWith(original.id, { expectedVersion: 5, tagIds: ['latest', 'added'] })
    expect(batch.failures.value[0]?.error).toContain('其他设备修改')
    expect(batch.count.value).toBe(1)
    expect(batch.busy.value).toBe(false)
    wrapper.unmount()
  })

  it('prefers newer feed metadata over a previous successful response', async () => {
    const original = messageFixture({ version: 4 })
    vi.spyOn(DefaultService, 'makeMessagePermanent').mockResolvedValue({ ...original, version: 5 })
    const trash = vi.spyOn(DefaultService, 'trashMessage').mockResolvedValue({ ...original, version: 8 })
    const { batch, wrapper, loaded } = setup([original])
    await batch.run('permanent')
    loaded.value = [messageFixture({ version: 7 })]
    batch.selectLoaded()
    await batch.run('trash')
    expect(trash).toHaveBeenCalledExactlyOnceWith(original.id, { expectedVersion: 7 })
    wrapper.unmount()
  })
})
