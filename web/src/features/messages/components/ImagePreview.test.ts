import { mount, flushPromises } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import ImagePreview from './ImagePreview.vue'
import { markdownImages } from '../composables/useMessageImages'

describe('image preview', () => {
  it('extracts safe inline images while rejecting active URLs', () => {
    expect(markdownImages('![one](https://example.test/1.png) ![bad](javascript:alert(1))')).toEqual([{ src: 'https://example.test/1.png', alt: 'one' }])
  })
  it('navigates, reports failures, retries, and closes with backdrop, button and Escape', async () => {
    const wrapper = mount(ImagePreview, { attachTo: document.body, props: { images: [{ src: '/a', alt: 'A' }, { src: '/b', alt: 'B' }], current: '/a' }, global: { stubs: { teleport: true } } })
    await flushPromises()
    await wrapper.get('img').trigger('error')
    expect(wrapper.get('[role="alert"]').text()).toContain('加载失败')
    await wrapper.findAll('button').find(b => b.text() === '重试')!.trigger('click')
    expect(wrapper.find('img').exists()).toBe(true)
    await wrapper.findAll('button').find(b => b.text() === '下一张')!.trigger('click')
    expect(wrapper.emitted('select')?.[0]).toEqual(['/b'])
    await wrapper.get('.image-preview').trigger('click')
    await wrapper.get('[aria-label="关闭图片预览"]').trigger('click')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(wrapper.emitted('close')).toHaveLength(3)
    wrapper.unmount()
    expect(document.body.style.overflow).toBe('')
  })
})
