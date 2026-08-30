import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it, vi } from 'vitest'
import { DefaultService } from '@/api/generated'
import { messageFixture } from '@/test/fixtures'
import MessageDetailSurface from './MessageDetailSurface.vue'

describe('MessageDetailSurface', () => {
  it('queries only the message selected by the detail URL', async () => {
    const getMessage = vi.spyOn(DefaultService, 'getMessage').mockResolvedValue(messageFixture())
    vi.spyOn(DefaultService, 'listTags').mockResolvedValue([])
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/temporary', component: { template: '<div />' } }],
    })
    await router.push('/temporary?q=nginx')
    await router.isReady()
    const wrapper = mount(MessageDetailSurface, {
      global: {
        plugins: [router, [VueQueryPlugin, { queryClient: new QueryClient({ defaultOptions: { queries: { retry: false } } }) }]],
        stubs: { teleport: true },
      },
    })

    expect(wrapper.text()).toContain('选择一条内容查看详情')
    expect(getMessage).not.toHaveBeenCalled()

    await router.push({ query: { q: 'nginx', detail: 'message-1' } })
    await flushPromises()
    expect(getMessage).toHaveBeenCalledTimes(1)
    expect(getMessage).toHaveBeenCalledWith('message-1')
    expect(wrapper.find('.detail-sheet').exists()).toBe(true)
    wrapper.unmount()
  })
})
