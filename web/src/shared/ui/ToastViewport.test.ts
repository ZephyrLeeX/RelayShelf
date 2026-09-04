import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ToastViewport from './ToastViewport.vue'
import { toast } from './toast'

describe('ToastViewport', () => {
  afterEach(() => {
    toast.clear()
    vi.useRealTimers()
  })

  it('announces, dismisses, and automatically removes notifications', async () => {
    vi.useFakeTimers()
    const wrapper = mount(ToastViewport)
    toast.success('正文已复制', 1_600)
    await wrapper.vm.$nextTick()

    expect(wrapper.attributes('aria-live')).toBe('polite')
    expect(wrapper.get('[role="status"]').text()).toContain('正文已复制')
    vi.advanceTimersByTime(1_600)
    await wrapper.vm.$nextTick()
    expect(wrapper.find('.toast-item').exists()).toBe(false)

    toast.error('操作失败', 0)
    await wrapper.vm.$nextTick()
    expect(wrapper.get('[role="alert"]').text()).toContain('操作失败')
    await wrapper.get('button').trigger('click')
    expect(wrapper.find('.toast-item').exists()).toBe(false)
  })
})
