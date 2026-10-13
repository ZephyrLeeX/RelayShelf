import { VueQueryPlugin, QueryClient } from '@tanstack/vue-query'
import { mount } from '@vue/test-utils'
import { nextTick, ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { Lifecycle } from '@/api/generated'
import { messageFixture } from '@/test/fixtures'
import { toast } from '@/shared/ui/toast'
import { useMessageFeed } from '../queries'
import MessageFeed from './MessageFeed.vue'

vi.mock('../queries', () => ({ useMessageFeed: vi.fn() }))

function queryState() {
  return {
    data: ref({ pages: [{ items: [messageFixture({ id: 'message-1' })], nextCursor: null }], pageParams: [undefined] }),
    isFetching: ref(false),
    isPending: ref(false),
    isFetchingNextPage: ref(false),
    isError: ref(false),
    isFetchNextPageError: ref(false),
    isRefetchError: ref(false),
    errorUpdateCount: ref(0),
    hasNextPage: ref(false),
    fetchNextPage: vi.fn(),
    refetch: vi.fn(),
  }
}

function renderFeed(query: ReturnType<typeof queryState>) {
  vi.mocked(useMessageFeed).mockReturnValue(query as never)
  return mount(MessageFeed, {
    props: { filters: { lifecycle: Lifecycle.TEMPORARY }, emptyText: '暂无内容' },
    global: { plugins: [[VueQueryPlugin, { queryClient: new QueryClient() }]], stubs: { MessageCard: true, BatchTagOptions: true } },
  })
}

describe('MessageFeed background refresh feedback', () => {
  beforeEach(() => toast.clear())

  it('uses one in-place status across consecutive refetches without creating toasts', async () => {
    const query = queryState()
    const wrapper = renderFeed(query)

    for (let attempt = 0; attempt < 3; attempt += 1) {
      query.isFetching.value = true
      await nextTick()
      expect(wrapper.get('[role="status"]').text()).toBe('正在同步最新内容…')
      expect(toast.items.value).toHaveLength(0)
      query.isFetching.value = false
      await nextTick()
    }

    expect(wrapper.get('[role="status"]').text()).toBe('当前分区的全部内容')
  })

  it('shows an error toast when a refetch fails while preserving existing content', async () => {
    const query = queryState()
    const wrapper = renderFeed(query)

    query.isFetching.value = true
    await nextTick()
    query.isFetching.value = false
    query.isRefetchError.value = true
    query.errorUpdateCount.value += 1
    await nextTick()

    expect(wrapper.findAllComponents({ name: 'MessageCard' })).toHaveLength(1)
    expect(toast.items.value).toEqual([
      expect.objectContaining({ type: 'error', message: '同步失败，当前显示的是已有内容' }),
    ])
  })
})


describe('feed selection', () => {
  it('selects only loaded items, preserves selection on equivalent filters, and clears on filter changes', async () => {
    const query = queryState()
    query.hasNextPage.value = true
    const wrapper = renderFeed(query)
    const click = async (text: string) => { await wrapper.findAll('button').find(button => button.text() === text)!.trigger('click') }
    await click('多选')
    await click('全选已加载内容')
    expect(wrapper.text()).toContain('已选 1 项')
    query.data.value.pages.push({ items: [messageFixture({ id: 'message-2' })], nextCursor: null })
    await nextTick()
    expect(wrapper.findAll('input:checked')).toHaveLength(1)
    await wrapper.setProps({ filters: { lifecycle: Lifecycle.TEMPORARY } })
    expect(wrapper.text()).toContain('已选 1 项')
    await click('取消选择')
    expect(wrapper.text()).toContain('已选 0 项')
    await click('全选已加载内容')
    expect(wrapper.text()).toContain('已选 2 项')
    await wrapper.setProps({ filters: { favorite: true } })
    expect(wrapper.find('[aria-label="批量操作"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
