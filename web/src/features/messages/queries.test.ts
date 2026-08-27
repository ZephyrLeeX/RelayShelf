import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { DefaultService, Lifecycle } from '@/api/generated'
import { messageFixture } from '@/test/fixtures'
import { useMessageFeed } from './queries'

function harness(client: QueryClient) {
  return mount(defineComponent({
    setup() { const query = useMessageFeed({ lifecycle: Lifecycle.TEMPORARY }); return { query } },
    render() { return h('div') },
  }), { global: { plugins: [[VueQueryPlugin, { queryClient: client }]] } })
}

describe('infinite message query', () => {
  beforeEach(() => vi.restoreAllMocks())
  it('passes the opaque next cursor, preserves order, and stops at null', async () => {
    const list = vi.spyOn(DefaultService, 'listMessages')
      .mockResolvedValueOnce({ items: [messageFixture({ id: 'm1' })], nextCursor: 'cursor-A' })
      .mockResolvedValueOnce({ items: [messageFixture({ id: 'm2' })], nextCursor: null })
    const wrapper = harness(new QueryClient({ defaultOptions: { queries: { retry: false } } }))
    await flushPromises()
    await wrapper.vm.query.fetchNextPage(); await flushPromises()
    expect(list.mock.calls[1][3]).toBe('cursor-A')
    expect(wrapper.vm.query.data.value?.pages.flatMap((page) => page.items.map((item) => item.id))).toEqual(['m1', 'm2'])
    expect(wrapper.vm.query.hasNextPage.value).toBe(false)
  })
  it('keeps page one when page two fails', async () => {
    vi.spyOn(DefaultService, 'listMessages').mockResolvedValueOnce({ items: [messageFixture({ id:'m1' })], nextCursor:'cursor-A' }).mockRejectedValueOnce(new Error('page two failed'))
    const wrapper = harness(new QueryClient({ defaultOptions: { queries: { retry: false } } }))
    await flushPromises(); await wrapper.vm.query.fetchNextPage(); await flushPromises()
    expect(wrapper.vm.query.data.value?.pages[0].items[0].id).toBe('m1')
    expect(wrapper.vm.query.isFetchNextPageError.value).toBe(true)
  })
})
