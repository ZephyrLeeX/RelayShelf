import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it, vi } from 'vitest'
import { DefaultService } from '@/api/generated'
import { messageFixture } from '@/test/fixtures'
import MessageDetailView from './MessageDetailView.vue'

describe('sensitive detail', () => {
  it('reveals only on explicit click and never puts plaintext in query cache', async () => {
    vi.spyOn(DefaultService, 'getMessage').mockResolvedValue(messageFixture({ sensitive:true, body:null, bodyPreview:null }))
    const reveal = vi.spyOn(DefaultService, 'revealSensitiveBody').mockResolvedValue({ body:'local-only-secret', version:1 })
    vi.spyOn(DefaultService, 'listTags').mockResolvedValue([])
    const client = new QueryClient({ defaultOptions: { queries: { retry:false } } })
    const router = createRouter({ history:createMemoryHistory(), routes:[{ path:'/messages/:id', component:{ template:'<div />' } }, { path:'/temporary', component:{ template:'<div />' } }] })
    await router.push('/messages/message-1'); await router.isReady()
    const wrapper = mount(MessageDetailView, { props:{ id:'message-1' }, global:{ plugins:[router, [VueQueryPlugin, { queryClient:client }]], stubs:{ teleport:true } } })
    await flushPromises()
    expect(reveal).not.toHaveBeenCalled(); expect(wrapper.text()).not.toContain('local-only-secret')
    await wrapper.get('.sensitive .button.primary').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('local-only-secret')
    expect(client.getQueryCache().getAll().some((query) => JSON.stringify(query.state.data).includes('local-only-secret'))).toBe(false)
    wrapper.unmount()
  })
})
