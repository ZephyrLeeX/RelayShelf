import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import AttachmentThumbnail from './AttachmentThumbnail.vue'

describe('AttachmentThumbnail', () => {
  afterEach(() => vi.useRealTimers())
  it('only loads lazy authorized thumbnails and clears retries when the attachment changes', async () => {
    vi.useFakeTimers()
    const wrapper = mount(AttachmentThumbnail, { props: { id: 'first', mime: 'image/png' } })
    expect(wrapper.get('img').attributes('loading')).toBe('lazy')
    expect(wrapper.get('img').attributes('src')).toBe('/api/v1/attachments/first/thumbnail?r=0')
    await wrapper.get('img').trigger('error')
    await wrapper.setProps({ id: 'second' })
    await vi.advanceTimersByTimeAsync(1000)
    expect(wrapper.get('img').attributes('src')).toBe('/api/v1/attachments/second/thumbnail?r=0')
    for (const delay of [1000, 3000, 10000]) {
      await wrapper.get('img').trigger('error')
      await vi.advanceTimersByTimeAsync(delay)
    }
    await wrapper.get('img').trigger('error')
    expect(wrapper.find('img').exists()).toBe(false)
    await wrapper.setProps({ id: 'third' })
    expect(wrapper.get('img').attributes('src')).toBe('/api/v1/attachments/third/thumbnail?r=0')
    wrapper.unmount()
    expect(vi.getTimerCount()).toBe(0)
  })
})
