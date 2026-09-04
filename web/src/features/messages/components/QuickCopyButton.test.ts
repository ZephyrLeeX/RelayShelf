import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { toast } from '@/shared/ui/toast'
import QuickCopyButton from './QuickCopyButton.vue'

describe('QuickCopyButton', () => {
  afterEach(() => {
    toast.clear()
    vi.useRealTimers()
  })

  it('shows shared success feedback for 1.6 seconds', async () => {
    vi.useFakeTimers()
    const wrapper = mount(QuickCopyButton, { props: { body: 'hello' } })
    await wrapper.trigger('click')

    expect(navigator.clipboard.writeText).toHaveBeenCalledWith('hello')
    expect(wrapper.text()).toBe('已复制')
    expect(toast.items.value[0]?.type).toBe('success')

    vi.advanceTimersByTime(1_600)
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toBe('复制')
  })

  it('uses the global error notification when clipboard access fails', async () => {
    vi.mocked(navigator.clipboard.writeText).mockRejectedValueOnce(new Error('denied'))
    const wrapper = mount(QuickCopyButton, { props: { body: 'hello' } })
    await wrapper.trigger('click')

    expect(wrapper.text()).toBe('复制')
    expect(toast.items.value[0]).toMatchObject({ type: 'error', message: expect.stringContaining('剪贴板权限') })
  })
})
