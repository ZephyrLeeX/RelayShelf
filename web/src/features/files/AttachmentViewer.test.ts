import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { AttachmentSummary } from '@/api/generated'
import AttachmentViewer from './AttachmentViewer.vue'
import { setStorageRuntimeStatus } from '@/features/storage/runtime'

const file = (detectedMime: string): AttachmentSummary =>
  ({ id: 'attach-1', originalFilename: 'file.bin', clientMime: 'text/plain', detectedMime, sizeBytes: 10, displayOrder: 0 })

function mountViewer(detectedMime: string) {
  const wrapper = mount(AttachmentViewer, { props: { files: [file(detectedMime)], currentId: 'attach-1' }, global: { stubs: { teleport: true } } })
  return wrapper
}

describe('AttachmentViewer', () => {
  afterEach(() => {
    setStorageRuntimeStatus(undefined)
    vi.unstubAllGlobals()
  })
  it('renders the safe raster original through the preview endpoint', () => {
    const wrapper = mountViewer('image/png')
    const image = wrapper.get('img.original')
    expect(image.attributes('src')).toBe('/api/v1/attachments/attach-1/preview')
    wrapper.unmount()
  })

  it('renders PDF, audio, and video via the inline preview endpoint', () => {
    for (const mime of ['application/pdf', 'audio/mpeg', 'video/mp4']) {
      const wrapper = mountViewer(mime)
      expect(wrapper.find('[src="/api/v1/attachments/attach-1/preview"]').exists()).toBe(true)
      wrapper.unmount()
    }
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
})
