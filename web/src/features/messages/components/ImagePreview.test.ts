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
  function displayBounds(wrapper: ReturnType<typeof create>, left = 200) {
    // jsdom has no layout; represent a transformed image with blank space around it.
    vi.spyOn(wrapper.get('img').element, 'getBoundingClientRect').mockReturnValue({ left, top: 100, right: left + 400, bottom: 500, width: 400, height: 400, x: left, y: 100, toJSON() {} })
  }
  it('hit-tests the displayed image even though clicks target the stage', async () => {
    const wrapper = create()
    await load(wrapper)
    displayBounds(wrapper)
    const stage = wrapper.get('.image-stage')
    await stage.trigger('click', { clientX: 400, clientY: 300 })
    await stage.trigger('click', { clientX: 400, clientY: 300 })
    await stage.trigger('dblclick', { clientX: 400, clientY: 300 })
    expect(wrapper.get('output').text()).toBe('100%')
    // After zoom/pan the hit-test must use the new bounds, not the fit rectangle.
    displayBounds(wrapper, 0)
    await stage.trigger('click', { clientX: 100, clientY: 300 })
    await wrapper.get('footer').trigger('click')
    await wrapper.get('[aria-label="放大"]').trigger('click')
    expect(wrapper.emitted('close')).toBeUndefined()
    await stage.trigger('click', { clientX: 700, clientY: 300 })
    expect(wrapper.emitted('close')).toHaveLength(1)
  })
  it.each(['drag', 'swipe', 'pinch', 'pointercancel', 'lostpointercapture'])('does not close after %s, but allows the next blank click', async gesture => {
    const wrapper = create()
    await load(wrapper)
    displayBounds(wrapper)
    const stage = wrapper.get('.image-stage')
    if (gesture === 'drag') await stage.trigger('dblclick')
    await pointer(wrapper, 'pointerdown', 1, 300, 300)
    if (gesture === 'pinch') {
      await pointer(wrapper, 'pointerdown', 2, 400, 300)
      await pointer(wrapper, 'pointerup', 2, 400, 300)
    }
    if (gesture === 'drag') {
      await pointer(wrapper, 'pointermove', 1, 100, 300)
      await pointer(wrapper, 'pointermove', 1, 300, 300)
    }
    const end = gesture.startsWith('pointer') || gesture === 'lostpointercapture' ? gesture : 'pointerup'
    await pointer(wrapper, end, 1, gesture === 'swipe' ? 100 : 300, 300)
    if (gesture === 'swipe') {
      expect(wrapper.emitted('select')).toEqual([['/b']])
      await wrapper.setProps({ current: '/b' })
    }
    // Compatibility clicks can arrive after all pointers and navigation state reset.
    await stage.trigger('click', { clientX: 50, clientY: 50 })
    await wrapper.get('.image-preview').trigger('click', { clientX: 50, clientY: 50 })
    expect(wrapper.emitted('close')).toBeUndefined()
    await wrapper.get('.image-preview').trigger('pointerdown')
    await wrapper.get('.image-preview').trigger('click', { clientX: 50, clientY: 50 })
    expect(wrapper.emitted('close')).toHaveLength(1)
    await pointer(wrapper, 'pointerdown', 1, 50, 50)
    await pointer(wrapper, 'pointerup', 1, 52, 51)
    await stage.trigger('click', { clientX: 52, clientY: 51 })
    expect(wrapper.emitted('close')).toHaveLength(2)
  })
  it.each(['loading', 'failed'])('closes on stage blank space while %s', async state => {
    const wrapper = create()
    if (state === 'failed') await wrapper.get('img').trigger('error')
    await wrapper.get('.image-stage').trigger('click', { clientX: 50, clientY: 50 })
    expect(wrapper.emitted('close')).toHaveLength(1)
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
    expect(wrapper.emitted('close')).toBeUndefined()
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
