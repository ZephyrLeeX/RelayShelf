import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it, vi } from 'vitest'
import { DefaultService } from '@/api/generated'
import { queryKeys } from '@/shared/api/queryKeys'
import { messageFixture } from '@/test/fixtures'
import MessageDetailView from './MessageDetailView.vue'

async function mountDetail(getMessage: (id: string) => ReturnType<typeof messageFixture>, id = 'message-1') {
  vi.spyOn(DefaultService, 'getMessage').mockImplementation((messageId) => Promise.resolve(getMessage(messageId)) as never)
  vi.spyOn(DefaultService, 'listTags').mockResolvedValue([])
  const client = new QueryClient({ defaultOptions: { queries: { retry:false }, mutations:{ retry:false } } })
  const router = createRouter({ history:createMemoryHistory(), routes:[{ path:'/messages/:id', component:{ template:'<div />' } }, { path:'/temporary', component:{ template:'<div />' } }] })
  await router.push(`/messages/${id}`); await router.isReady()
  const wrapper = mount(MessageDetailView, { props:{ id }, global:{ plugins:[router, [VueQueryPlugin, { queryClient:client }]], stubs:{ teleport:true } } })
  await flushPromises()
  return { wrapper, client }
}

describe('sensitive detail', () => {
  it('reveals only on explicit click and never puts plaintext in query cache', async () => {
    const reveal = vi.spyOn(DefaultService, 'revealSensitiveBody').mockResolvedValue({ body:'local-only-secret', version:1 })
    const { wrapper, client } = await mountDetail(() => messageFixture({ sensitive:true, body:null, bodyPreview:null }))
    expect(reveal).not.toHaveBeenCalled(); expect(wrapper.text()).not.toContain('local-only-secret')
    await wrapper.get('.sensitive .button.primary').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('local-only-secret')
    expect(client.getQueryCache().getAll().some((query) => JSON.stringify(query.state.data).includes('local-only-secret'))).toBe(false)
    wrapper.unmount()
  })
  it('discards a pending reveal after switching messages', async () => {
    let resolveA!: (value: { body:string; version:number }) => void
    vi.spyOn(DefaultService, 'revealSensitiveBody').mockImplementationOnce(() => new Promise((resolve) => { resolveA = resolve }) as never)
    const { wrapper } = await mountDetail((id) => messageFixture({ id, sensitive:true, body:null, bodyPreview:null }))
    await wrapper.get('.sensitive .button.primary').trigger('click')
    await wrapper.setProps({ id:'message-2' }); await flushPromises()
    resolveA({ body:'secret-from-a', version:1 }); await flushPromises()
    expect(wrapper.text()).not.toContain('secret-from-a')
    expect(wrapper.text()).toContain('Sensitive locked')
    wrapper.unmount()
  })
  it('clears v1 plaintext on a v2 refetch and requires a v2 reveal before editing', async () => {
    const reveal = vi.spyOn(DefaultService, 'revealSensitiveBody')
      .mockResolvedValueOnce({ body:'secret-v1', version:1 })
      .mockResolvedValueOnce({ body:'secret-v2', version:2 })
    const edit = vi.spyOn(DefaultService, 'editSensitiveBody').mockResolvedValue(messageFixture({ sensitive:true, body:null, version:3 }))
    const { wrapper, client } = await mountDetail(() => messageFixture({ sensitive:true, body:null, bodyPreview:null, version:1 }))
    await wrapper.get('.sensitive .button.primary').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('secret-v1')
    await wrapper.findAll('.actions button')[1].trigger('click'); await flushPromises()
    expect(wrapper.find('form.edit').exists()).toBe(true)
    client.setQueryData(queryKeys.messages.detail('message-1'), messageFixture({ sensitive:true, body:null, bodyPreview:null, version:2 }))
    await wrapper.get('form.edit').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).not.toContain('secret-v1')
    expect(wrapper.find('form.edit').exists()).toBe(false)
    expect(edit).not.toHaveBeenCalled()
    await wrapper.findAll('.actions button')[1].trigger('click'); await flushPromises()
    expect(reveal).toHaveBeenCalledTimes(2)
    expect(wrapper.get('textarea').element.value).toBe('secret-v2')
    wrapper.unmount()
  })
  it('discards a reveal response whose version does not match the current message', async () => {
    vi.spyOn(DefaultService, 'revealSensitiveBody').mockResolvedValue({ body:'stale-secret', version:1 })
    const { wrapper } = await mountDetail(() => messageFixture({ sensitive:true, body:null, bodyPreview:null, version:2 }))
    await wrapper.get('.sensitive .button.primary').trigger('click'); await flushPromises()
    expect(wrapper.text()).not.toContain('stale-secret')
    expect(wrapper.text()).toContain('Sensitive locked')
    wrapper.unmount()
  })
})
