import { mount, flushPromises, enableAutoUnmount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import ImagePreview from './ImagePreview.vue'
import { markdownImages } from '../composables/useMessageImages'

enableAutoUnmount(afterEach)
const images = [{ src: '/api/v1/attachments/a/preview', alt: 'A' }, { src: '/b', alt: 'B' }]
function create() {
  return mount(ImagePreview, { attachTo: document.body, props: { images, current: images[0].src }, global: { stubs: { teleport: true } } })
}
async function load(wrapper: ReturnType<typeof create>) {
  await flushPromises()
  const stage = wrapper.get('.image-stage').element
  Object.defineProperties(stage, { clientWidth: { configurable: true, value: 800 }, clientHeight: { configurable: true, value: 600 } })
  const image = wrapper.get('img')
  Object.defineProperties(image.element, { naturalWidth: { value: 1600 }, naturalHeight: { value: 1200 } })
  await image.trigger('load')
}
async function pointer(wrapper: ReturnType<typeof create>, type: string, id: number, x: number, y: number) {
  await wrapper.get('.image-stage').trigger(type, { pointerId: id, pointerType: 'touch', clientX: x, clientY: y })
}

describe('image preview', () => {
  beforeEach(() => { vi.spyOn(window, 'scrollTo').mockImplementation(() => {}) })
  it('extracts safe inline images while rejecting active URLs', () => {
    expect(markdownImages('![one](https://example.test/1.png) ![bad](javascript:alert(1))')).toEqual([{ src: 'https://example.test/1.png', alt: 'one' }])
  })
  it('uses natural dimensions for fit/100%, zooms, pans, and refits on resize', async () => {
    const wrapper = create()
    await load(wrapper)
    expect(wrapper.get('output').text()).toBe('50%')
    await wrapper.get('.image-stage').trigger('dblclick')
    expect(wrapper.get('output').text()).toBe('100%')
    await pointer(wrapper, 'pointerdown', 1, 200, 200)
    await pointer(wrapper, 'pointermove', 1, 300, 250)
    expect(wrapper.get('img').attributes('style')).toContain('translate(100px, 50px)')
    await pointer(wrapper, 'pointerup', 1, 300, 250)
    expect(wrapper.emitted('select')).toBeUndefined()
    wrapper.get('.image-stage').element.dispatchEvent(new WheelEvent('wheel', { deltaY: -100, clientX: 0, clientY: 0 }))
    await flushPromises()
    expect(parseFloat(wrapper.get('output').text())).toBeGreaterThan(100)
    await wrapper.get('.image-stage').trigger('dblclick')
    expect(wrapper.get('output').text()).toBe('50%')
    Object.defineProperties(wrapper.get('.image-stage').element, { clientWidth: { value: 400 }, clientHeight: { value: 600 } })
    window.dispatchEvent(new Event('resize'))
    await flushPromises()
    expect(wrapper.get('output').text()).toBe('25%')
    wrapper.unmount()
  })
  it('swipes only at fit and never treats a pinch or cancellation as a swipe', async () => {
    const wrapper = create()
    await load(wrapper)
    await pointer(wrapper, 'pointerdown', 1, 300, 200)
    await pointer(wrapper, 'pointerup', 1, 100, 200)
    expect(wrapper.emitted('select')).toEqual([['/b']])
    await pointer(wrapper, 'pointerdown', 1, 100, 200)
    await pointer(wrapper, 'pointerdown', 2, 200, 200)
    await pointer(wrapper, 'pointermove', 2, 300, 200)
    expect(wrapper.get('output').text()).toBe('100%')
    await pointer(wrapper, 'pointerup', 2, 300, 200)
    await pointer(wrapper, 'pointerup', 1, 0, 200)
    expect(wrapper.emitted('select')).toHaveLength(1)
    await wrapper.get('.image-stage').trigger('dblclick')
    await pointer(wrapper, 'pointerdown', 1, 300, 200)
    await pointer(wrapper, 'pointercancel', 1, 100, 200)
    expect(wrapper.emitted('select')).toHaveLength(1)
    wrapper.unmount()
  })
  it('resets on navigation, ignores detached requests, and retries the authorized original', async () => {
    const wrapper = create()
    const old = wrapper.get('img')
    expect(wrapper.get('[role="status"]').text()).toContain('正在加载')
    expect(wrapper.get('a').attributes('href')).toBe('/api/v1/attachments/a/download')
    await wrapper.setProps({ current: '/b' })
    await old.trigger('error')
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    await wrapper.get('img').trigger('error')
    expect(wrapper.get('[role="alert"]').text()).toContain('加载失败')
    await wrapper.findAll('button').find(b => b.text() === '重试')!.trigger('click')
    expect(wrapper.get('img').attributes('src')).toBe('/b')
    await load(wrapper)
    expect(wrapper.find('[role="status"]').exists()).toBe(false)
    expect(wrapper.get('output').text()).toBe('50%')
    wrapper.unmount()
  })
  it('traps focus, navigates with keys, closes, and restores focus and scroll styles', async () => {
    const opener = document.createElement('button'); document.body.append(opener); opener.focus()
    document.body.style.overflow = 'auto'
    const wrapper = create()
    await flushPromises()
    expect(document.body.style.position).toBe('fixed')
    expect(document.activeElement).toBe(wrapper.get('[aria-label="关闭图片预览"]').element)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', shiftKey: true, bubbles: true }))
    expect(document.activeElement?.textContent?.trim()).toBe('下一张')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }))
    expect(wrapper.emitted('select')?.[0]).toEqual(['/b'])
    await wrapper.get('.image-preview').trigger('click')
    await wrapper.get('[aria-label="关闭图片预览"]').trigger('click')
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    expect(wrapper.emitted('close')).toHaveLength(3)
    wrapper.unmount()
    expect(document.body.style.overflow).toBe('auto')
    expect(document.body.style.position).toBe('')
    expect(document.activeElement).toBe(opener)
    expect(window.scrollTo).toHaveBeenCalledWith(0, 0)
    document.body.style.overflow = ''
  })
})
