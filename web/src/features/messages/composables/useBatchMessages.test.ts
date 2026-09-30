import { mount } from '@vue/test-utils'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { defineComponent, ref } from 'vue'
import { describe, expect, it, vi } from 'vitest'
import { CancelablePromise } from '@/api/generated/core/CancelablePromise'
import { DefaultService } from '@/api/generated'
import { messageFixture } from '@/test/fixtures'
import { useBatchMessages } from './useBatchMessages'

function setup(items = [messageFixture()]) {
  let batch!: ReturnType<typeof useBatchMessages>
  const wrapper = mount(defineComponent({ setup() { batch = useBatchMessages(ref(items), () => ({})); return () => null } }), {
    global: { plugins: [[VueQueryPlugin, { queryClient: new QueryClient() }]] },
  })
  batch.active.value = true
  batch.selectLoaded()
  return { batch, wrapper }
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
})
