import { mount, flushPromises } from '@vue/test-utils'
import { defineComponent, h, ref } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { AttachmentSummary } from '@/api/generated'
import { useMessageImages } from './useMessageImages'
import { toast } from '@/shared/ui/toast'

const one = 'https://image.test/one.png', two = 'https://image.test/two.png'
const markdown = (urls: string[]) => urls.map(src => `![image](${src})`).join('\n')
const wrappers: ReturnType<typeof mount>[] = []
afterEach(() => { wrappers.splice(0).forEach(wrapper => wrapper.unmount()); vi.restoreAllMocks() })
function setup(load?: () => Promise<{ source: string; files: AttachmentSummary[] }>) {
  const source = ref(markdown([one, two])), files = ref<AttachmentSummary[]>([]), boundary = ref('message-1:unlocked'), revision = ref(1)
  let gallery!: ReturnType<typeof useMessageImages>
  const wrapper = mount(defineComponent({ setup() {
    gallery = useMessageImages(() => source.value, () => files.value, load, () => boundary.value, () => revision.value)
    return () => h('div')
  } }))
  wrappers.push(wrapper)
  return { source, files, boundary, revision, gallery }
}
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
describe('message gallery content identity', () => {
  it('keeps the current preview across identical arrays and prose/alt changes', async () => {
    const load = vi.fn().mockResolvedValue({ source: markdown([one, two]), files: [] })
    const { gallery, files } = setup(load)
    await gallery.open(one)
    const complete = gallery.images.value
    files.value = [] // An SSE title/tags/favorite update replaces message metadata.
    await flushPromises()
    expect(gallery.current.value).toBe(one)
    expect(gallery.images.value).toBe(complete)
    expect(load).toHaveBeenCalledTimes(1)
  })
  it('updates a collection while preserving the current image and closes when it is removed', async () => {
    const { gallery, source } = setup()
    await gallery.open(two)
    source.value = `new prose\n${markdown([two])}`
    await flushPromises()
    expect(gallery.current.value).toBe(two)
    expect(gallery.images.value.map(image => image.src)).toEqual([two])
    source.value = markdown([one])
    await flushPromises()
    expect(gallery.current.value).toBeNull()
  })
  it.each(['message-2:unlocked', 'message-1:locked', 'message-1:unauthorized', 'message-1:deleted'])('clears the full gallery at boundary %s and ignores its pending load', async boundaryValue => {
    const pending = deferred<{ source: string; files: AttachmentSummary[] }>()
    const { gallery, boundary, source } = setup(() => pending.promise)
    const operation = gallery.open(one)
    boundary.value = boundaryValue
    source.value = ''
    pending.resolve({ source: markdown([one, two]), files: [] }); await operation
    expect(gallery.current.value).toBeNull()
    expect(gallery.images.value).toEqual([])
    await gallery.open(one) // A stale DOM/injected callback cannot reopen cleared content.
    expect(gallery.current.value).toBeNull()
  })
  it('loads the full collection on demand, refreshes it and rejects an older content response', async () => {
    const old = deferred<{ source: string; files: AttachmentSummary[] }>()
    const fresh = deferred<{ source: string; files: AttachmentSummary[] }>()
    const load = vi.fn().mockReturnValueOnce(old.promise).mockReturnValueOnce(fresh.promise)
    const { gallery, source } = setup(load)
    expect(load).not.toHaveBeenCalled()
    const operation = gallery.open(one)
    source.value = markdown([one])
    fresh.resolve({ source: markdown([one]), files: [] }); await flushPromises()
    old.resolve({ source: markdown([one, two]), files: [] }); await operation
    expect(gallery.images.value.map(image => image.src)).toEqual([one])
    expect(gallery.current.value).toBe(one)
  })
  it('retries failed loads on the next open', async () => {
    vi.spyOn(toast, 'warning').mockReturnValue(0)
    const load = vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce({ source: markdown([one, two]), files: [] })
    const { gallery } = setup(load)
    await gallery.open(one)
    expect(gallery.current.value).toBe(one)
    gallery.current.value = null
    await gallery.open(one)
    expect(load).toHaveBeenCalledTimes(2)
    expect(gallery.images.value).toHaveLength(2)
  })
  it.each([401, 403, 404, 410])('clears a preview when full-gallery authorization fails with %s', async status => {
    const { gallery } = setup(() => Promise.reject({ status, code: 'UNAVAILABLE' }))
    await gallery.open(one)
    expect(gallery.current.value).toBeNull()
  })
})

it('refreshes truncated tails on revision hints without closing identical content, and clears a removed full-only image', async () => {
  const load = vi.fn().mockResolvedValueOnce({ source: markdown([one, two]), files: [] })
    .mockResolvedValueOnce({ source: markdown([one, two]), files: [] })
    .mockResolvedValueOnce({ source: markdown([one]), files: [] })
  const { gallery, source, revision } = setup(load)
  source.value = markdown([one]) // The card cannot see the second image in its truncated preview.
  await gallery.open(one)
  await gallery.open(two)
  revision.value++ // A title/tag/favorite update is only a refresh hint.
  await flushPromises()
  expect(gallery.current.value).toBe(two)
  expect(gallery.images.value.map(image => image.src)).toEqual([one, two])
  revision.value++ // A body tail edit can remove an image without changing the preview.
  await flushPromises()
  expect(gallery.current.value).toBeNull()
  expect(gallery.images.value.map(image => image.src)).toEqual([one])
  expect(load).toHaveBeenCalledTimes(3)
  revision.value++
  await flushPromises()
  expect(load).toHaveBeenCalledTimes(3) // Closed galleries never fetch the stream's full bodies.
})
it('falls back to loaded images and retries after a full-gallery refresh error', async () => {
  vi.spyOn(toast, 'warning').mockReturnValue(0)
  const load = vi.fn().mockResolvedValueOnce({ source: markdown([one, two]), files: [] })
    .mockRejectedValueOnce(new Error('offline'))
    .mockResolvedValueOnce({ source: markdown([one, two]), files: [] })
  const { gallery, revision } = setup(load)
  await gallery.open(one)
  revision.value++
  await flushPromises()
  expect(gallery.current.value).toBe(one)
  gallery.current.value = null
  await gallery.open(one)
  expect(load).toHaveBeenCalledTimes(3)
})
