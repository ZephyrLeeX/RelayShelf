import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { DefaultService } from '@/api/generated'
import { messageFixture } from '@/test/fixtures'
import MessageTagPicker from './MessageTagPicker.vue'

describe('MessageTagPicker', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    vi.spyOn(DefaultService, 'listTags').mockResolvedValue([])
  })
  afterEach(() => document.body.replaceChildren())

  it('consumes Escape, closes the popover, and restores trigger focus', async () => {
    const parentKeydown = vi.fn()
    document.addEventListener('keydown', parentKeydown)
    const wrapper = mount(MessageTagPicker, {
      attachTo: document.body,
      props: { message: messageFixture() },
      global: { plugins: [[VueQueryPlugin, { queryClient: new QueryClient({ defaultOptions: { queries: { retry: false } } }) }]] },
    })
    await flushPromises()
    const trigger = wrapper.get<HTMLButtonElement>('.tag-trigger')
    await trigger.trigger('click')
    const input = wrapper.get<HTMLInputElement>('input[aria-label="新建标签名称"]')
    input.element.focus()

    await input.trigger('keydown', { key: 'Escape' })
    await flushPromises()

    expect(wrapper.find('.tag-popover').exists()).toBe(false)
    expect(document.activeElement).toBe(trigger.element)
    expect(parentKeydown).not.toHaveBeenCalled()

    await trigger.trigger('keydown', { key: 'Escape' })
    expect(parentKeydown).toHaveBeenCalledTimes(1)
    document.removeEventListener('keydown', parentKeydown)
    wrapper.unmount()
  })
})
