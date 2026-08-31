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
    expect(wrapper.get('.detail-sheet').attributes('aria-modal')).toBeUndefined()
    wrapper.unmount()
  })

  it('closes with Escape, preserves unrelated query state, and restores focus', async () => {
    vi.spyOn(DefaultService, 'getMessage').mockResolvedValue(messageFixture())
    vi.spyOn(DefaultService, 'listTags').mockResolvedValue([])
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/search', component: { template: '<div />' } }],
    })
    await router.push('/search?q=nginx')
    await router.isReady()
    const opener = document.createElement('button')
    document.body.append(opener)
    opener.focus()
    const wrapper = mount(MessageDetailSurface, {
      attachTo: document.body,
      global: {
        plugins: [router, [VueQueryPlugin, { queryClient: new QueryClient({ defaultOptions: { queries: { retry: false } } }) }]],
        stubs: { teleport: true },
      },
    })

    await router.push({ query: { q: 'nginx', detail: 'message-1' } })
    await flushPromises()
    expect(document.activeElement).toBe(wrapper.get('.detail-sheet').element)

    const nestedViewer = document.createElement('div')
    nestedViewer.className = 'viewer'
    document.body.append(nestedViewer)
    expect(document.querySelector('.viewer')).toBe(nestedViewer)
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(router.currentRoute.value.query.detail).toBe('message-1')
    nestedViewer.remove()

    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await flushPromises()
    expect(router.currentRoute.value.query).toEqual({ q: 'nginx' })
    expect(document.activeElement).toBe(opener)
    wrapper.unmount()
  })
})
