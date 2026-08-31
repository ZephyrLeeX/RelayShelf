import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { AttachmentSummary } from '@/api/generated'
import TextPreview from './TextPreview.vue'

const file = (sizeBytes: number) => ({ id: 'attach-1', originalFilename: 'notes.txt', clientMime: 'text/plain', detectedMime: 'text/plain', sizeBytes, displayOrder: 0 }) as AttachmentSummary

function response(status: number, text: string) {
  const bytes = new TextEncoder().encode(text)
  return { ok: true, status, arrayBuffer: () => Promise.resolve(bytes.buffer) }
}

afterEach(() => vi.unstubAllGlobals())

describe('TextPreview', () => {
  it('requests a bounded 1 MiB range with credentials and renders the decoded text', async () => {
    const fetchMock = vi.fn().mockResolvedValue(response(200, 'hello preview'))
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = mount(TextPreview, { props: { file: file(12) } })
    await flushPromises()
    expect(fetchMock).toHaveBeenCalledWith(expect.any(String), expect.objectContaining({
      credentials: 'include',
      headers: { Range: 'bytes=0-1048575' },
    }))
    expect(wrapper.text()).toContain('hello preview')
    expect(wrapper.find('.notice').exists()).toBe(false)
    wrapper.unmount()
  })

  it('marks a 206 partial response as truncated', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(response(206, 'partial')))
    const wrapper = mount(TextPreview, { props: { file: file(3_000_000) } })
    await flushPromises()
    expect(wrapper.text()).toContain('仅显示前 1 MB')
    wrapper.unmount()
  })

  it('shows a download hint when the bounded fetch fails', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('network')))
    const wrapper = mount(TextPreview, { props: { file: file(10) } })
    await flushPromises()
    expect(wrapper.text()).toContain('文本预览暂不可用')
    wrapper.unmount()
  })
})
