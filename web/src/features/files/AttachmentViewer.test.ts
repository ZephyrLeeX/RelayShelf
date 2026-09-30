import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import type { AttachmentSummary } from '@/api/generated'
import AttachmentViewer from './AttachmentViewer.vue'
import { setStorageRuntimeStatus } from '@/features/storage/runtime'
import { queryClient } from '@/app/queryClient'

const file = (detectedMime: string): AttachmentSummary =>
  ({ id: 'attach-1', originalFilename: 'file.bin', clientMime: 'text/plain', detectedMime, sizeBytes: 10, displayOrder: 0 })

function mountViewer(detectedMime: string) {
  const wrapper = mount(AttachmentViewer, { props: { files: [file(detectedMime)], currentId: 'attach-1' }, global: { stubs: { teleport: true } } })
  return wrapper
}

describe('AttachmentViewer', () => {
  beforeEach(() => { vi.spyOn(window, 'scrollTo').mockImplementation(() => {}) })
  const partialResponse = () => new Response(new Uint8Array([0]), { status: 206 })

  afterEach(() => {
    setStorageRuntimeStatus(undefined)
    vi.restoreAllMocks()
    vi.unstubAllGlobals()
  })
  it('delegates raster images to the shared viewer using the authorized preview endpoint', async () => {
    const fetch = vi.fn().mockResolvedValue(partialResponse())
    vi.stubGlobal('fetch', fetch)
    const wrapper = mountViewer('image/png')
    await flushPromises()
    const image = wrapper.get('img.original')
    expect(image.attributes('src')).toBe('/api/v1/attachments/attach-1/preview')
    expect(wrapper.find('[aria-label="图片预览"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('正在加载图片')
    expect(wrapper.text()).not.toContain('此文件仅支持下载')
    expect(wrapper.get('a').attributes('href')).toBe('/api/v1/attachments/attach-1/download')
    expect(fetch).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('renders PDF, audio, and video via the inline preview endpoint after a range preflight', async () => {
    vi.stubGlobal('fetch', vi.fn().mockImplementation(() => Promise.resolve(partialResponse())))
    for (const mime of ['application/pdf', 'audio/mpeg', 'video/mp4']) {
      const wrapper = mountViewer(mime)
      expect(wrapper.text()).toContain('正在加载文件')
      expect(wrapper.text()).not.toContain('此文件仅支持下载')
      await flushPromises()
      expect(wrapper.find('[src="/api/v1/attachments/attach-1/preview"]').exists()).toBe(true)
      wrapper.unmount()
    }
  })


  it('ignores a late preflight after navigation and retries the current media', async () => {
    let resolveOld!: (response: Response) => void
    const fetch = vi.fn().mockImplementationOnce(() => new Promise<Response>(resolve => { resolveOld = resolve }))
      .mockResolvedValueOnce(new Response('', { status: 500 }))
      .mockResolvedValueOnce(partialResponse())
    vi.stubGlobal('fetch', fetch)
    const second = { ...file('application/pdf'), id: 'attach-2' }
    const wrapper = mount(AttachmentViewer, { props: { files: [file('application/pdf'), second], currentId: 'attach-1' }, global: { stubs: { teleport: true } } })
    await wrapper.setProps({ currentId: second.id })
    await flushPromises()
    resolveOld(partialResponse())
    await flushPromises()
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(wrapper.text()).toContain('文件预览暂不可用')
    await wrapper.findAll('button').find(button => button.text() === '重试')!.trigger('click')
    await flushPromises()
    expect(wrapper.get('iframe').attributes('src')).toBe('/api/v1/attachments/attach-2/preview')
    wrapper.unmount()
  })

  it('keeps unsafe active content download-only with no same-origin render', () => {
    for (const mime of ['text/html', 'application/xhtml+xml', 'image/svg+xml', 'application/xml', 'text/xml']) {
      const wrapper = mountViewer(mime)
      expect(wrapper.text()).toContain('此文件仅支持下载')
      expect(wrapper.find('img').exists()).toBe(false)
      expect(wrapper.find('iframe').exists()).toBe(false)
      expect(wrapper.find('[src^="/api/v1/attachments/attach-1/preview"]').exists()).toBe(false)
      expect(wrapper.get('a.button').attributes('href')).toBe('/api/v1/attachments/attach-1/download')
      wrapper.unmount()
    }
  })

  it('shows a friendly inline error when download reports storage unavailable', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ code: 'STORAGE_UNAVAILABLE', message: 'storage is unavailable', traceId: 'abc' }), { status: 503, headers: { 'Content-Type': 'application/json' } })))
    const wrapper = mountViewer('application/octet-stream')
    await wrapper.get('a.download').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('文件暂时无法读取')
    expect(wrapper.text()).toContain('存储服务当前不可用')
    wrapper.unmount()
  })

  it('does not mount a PDF or expose raw JSON when preview storage is unavailable', async () => {
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ code: 'STORAGE_UNAVAILABLE', message: 'raw storage error', traceId: 'abc' }), { status: 503, headers: { 'Content-Type': 'application/json' } })))
    const wrapper = mountViewer('application/pdf')
    await flushPromises()
    expect(wrapper.find('iframe').exists()).toBe(false)
    expect(wrapper.text()).toContain('文件暂时无法读取')
    expect(wrapper.text()).toContain('存储服务当前不可用')
    expect(wrapper.text()).not.toContain('raw storage error')
    expect(invalidate).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('keeps generic media failures generic and does not signal a storage outage', async () => {
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('', { status: 500 })))
    const wrapper = mountViewer('video/mp4')
    await flushPromises()
    expect(wrapper.text()).toContain('文件预览暂不可用')
    expect(invalidate).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
