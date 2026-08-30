import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it, vi } from 'vitest'
import { ApiError, DefaultService, Lifecycle } from '@/api/generated'
import { messageFixture } from '@/test/fixtures'
import MessageCard from './MessageCard.vue'

async function render(overrides = {}, trash = false) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }, { path: '/messages/:id', name: 'message-detail', component: { template: '<div />' } }] })
  await router.push('/'); await router.isReady()
  return { router, wrapper: mount(MessageCard, { props: { message: messageFixture(overrides), trash }, global: { plugins: [router, [VueQueryPlugin, { queryClient: new QueryClient() }]] } }) }
}

describe('MessageCard', () => {
  it('never renders a sensitive body preview', async () => {
    const { wrapper } = await render({ sensitive: true, bodyPreview: 'must-not-leak' })
    expect(wrapper.text()).toContain('Sensitive')
    expect(wrapper.text()).not.toContain('must-not-leak')
  })
  it('hides favorite for temporary messages and offers it for permanent messages', async () => {
    expect((await render({ lifecycle: Lifecycle.TEMPORARY })).wrapper.text()).not.toContain('收藏')
    expect((await render({ lifecycle: Lifecycle.PERMANENT })).wrapper.text()).toContain('收藏')
  })
  it('does not offer preview copying as full body when truncated', async () => {
    const { wrapper } = await render({ bodyTruncated: true })
    expect(wrapper.text()).toContain('打开并复制')
  })
  it('uses restore and permanent delete semantics in trash', async () => {
    const { wrapper } = await render({ trashedAt: '2026-01-02T00:00:00Z' }, true)
    expect(wrapper.text()).toContain('恢复')
    expect(wrapper.text()).toContain('永久删除')
    expect(wrapper.text()).not.toContain('收藏')
  })
  it('surfaces version conflict without retrying an overwrite', async () => {
    const favorite = vi.spyOn(DefaultService, 'setMessageFavorite').mockRejectedValue(new ApiError(
      { method:'POST', url:'/messages/{messageId}/favorite' },
      { url:'/api/v1/messages/m1/favorite', ok:false, status:409, statusText:'Conflict', body:{ code:'MESSAGE_VERSION_CONFLICT', message:'conflict', traceId:'t' } },
      'conflict',
    ))
    const { wrapper } = await render({ lifecycle:Lifecycle.PERMANENT })
    await wrapper.findAll('button').find((button) => button.text() === '收藏')!.trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(wrapper.text()).toContain('内容已在其他设备修改')
    expect(favorite).toHaveBeenCalledTimes(1)
  })
  it('opens the inspector through canonical URL selection', async () => {
    const { router, wrapper } = await render()
    await wrapper.get('.body-button').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(router.currentRoute.value.query.detail).toBe('message-1')
    expect(router.currentRoute.value.path).toBe('/')
  })
})
