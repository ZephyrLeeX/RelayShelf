import { mount, flushPromises } from '@vue/test-utils'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { createRouter, createMemoryHistory } from 'vue-router'
import { defineComponent, h, ref } from 'vue'
import { useBatchMessages } from '@/features/messages/composables/useBatchMessages'
import { messageFixture } from '@/test/fixtures'
import type { MessageFilters } from '@/shared/api/queryKeys'
import { savedSearchKey } from './savedQueries'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { CancelablePromise, DefaultService, type SavedSearch } from '@/api/generated'
import SearchView from './SearchView.vue'
import { readConditions } from './conditions'

vi.mock('@/features/tags/queries', () => ({ useTagsQuery: () => ({ data: ref([]), error: ref(null) }) }))
const Feed = defineComponent({
  props: { filters: { type: Object, required: true } },
  setup(props) {
    const batch = useBatchMessages(ref(Array.from({ length: 5 }, (_, i) => messageFixture({ id: String(i) }))), () => props.filters as MessageFilters)
    return { batch }
  },
  render() { return h('div', { 'data-test': 'feed' }, `${this.batch.active.value}:${this.batch.count.value}:${this.batch.busy.value}`) },
})
let items: SavedSearch[]
const id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
async function render(query: Record<string, string>, cached?: SavedSearch[]) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/search', name: 'search', component: SearchView }] })
  await router.push({ name: 'search', query })
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  if (cached) client.setQueryData(savedSearchKey, cached)
  const wrapper = mount(SearchView, { global: { plugins: [router, [VueQueryPlugin, { queryClient: client }]], stubs: { MessageFeed: Feed } } })
  await flushPromises()
  return { wrapper, router, client }
}
beforeEach(() => {
  vi.restoreAllMocks()
  items = [{ id, name: '本周', conditions: readConditions({ q: 'hello', time: '7d' }), invalidReason: '', updatedAt: '2026-09-30T00:00:00Z' }]
  vi.spyOn(DefaultService, 'listSavedSearches').mockImplementation(() => new CancelablePromise(resolve => resolve([...items])))
  vi.spyOn(DefaultService, 'updateSavedSearch').mockImplementation((key, request) => new CancelablePromise(resolve => {
    const item = { ...items.find(v => v.id === key)!, ...request }
    items = [item]; resolve(item)
  }))
  vi.spyOn(DefaultService, 'deleteSavedSearch').mockImplementation(() => new CancelablePromise(resolve => { items = []; resolve() }))
})

describe('SearchView saved views', () => {
  it('loads persisted filters, preserves them during rename, explicitly updates, and deletes', async () => {
    const { wrapper, router, client } = await render({ saved: id })
    expect(router.currentRoute.value.query.q).toBe('hello')
    expect(wrapper.findComponent(Feed).exists()).toBe(true)
    await wrapper.get('.main-search input').setValue('changed')
    await wrapper.get('input[maxlength="100"]').setValue('新名称')
    const click = async (text: string) => { await wrapper.findAll('button').find(v => v.text() === text)!.trigger('click'); await flushPromises() }
    await click('重命名')
    expect(DefaultService.updateSavedSearch).toHaveBeenLastCalledWith(id, expect.objectContaining({ name: '新名称', conditions: expect.objectContaining({ q: 'hello' }) }))
    await wrapper.get('.main-search input').setValue('changed')
    await wrapper.get('form').trigger('submit')
    await flushPromises()
    expect(items[0]!.conditions.q).toBe('hello')
    await click('更新当前视图条件')
    expect(items[0]!.conditions.q).toBe('changed')
    // Reopening the same view through its shortcut restores its saved filters.
    await router.push({ name: 'search', query: { saved: id } }); await flushPromises()
    expect(router.currentRoute.value.query.q).toBe('changed')
    await click('删除视图')
    expect(DefaultService.deleteSavedSearch).toHaveBeenCalledWith(id)
    expect(router.currentRoute.value.query.saved).toBeUndefined()
    wrapper.unmount(); client.clear()
  })
  it('blocks invalid views and deleted tags instead of widening results', async () => {
    items[0]!.invalidReason = '标签已删除，请修正'
    items[0]!.conditions.tagIds = ['bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb']
    const { wrapper, client } = await render({ saved: id })
    expect(wrapper.text()).toContain('标签已删除，请修正')
    expect(wrapper.findComponent(Feed).exists()).toBe(false)
    expect(wrapper.text()).toContain('不可用标签')
    wrapper.unmount(); client.clear()
    const result = await render({ type: 'UNSUPPORTED' })
    expect(result.wrapper.findComponent(Feed).exists()).toBe(false)
    result.wrapper.unmount(); result.client.clear()
  })
})

it.each([null, undefined, 'invalid', [null]])('blocks malformed tagIds %s, then permits explicit repair or deletion', async tagIds => {
  items[0]!.conditions.tagIds = tagIds as never
  const { wrapper, router, client } = await render({ saved: id, q: 'attempted bypass', time: 'all' })
  expect(wrapper.text()).toContain('结构已失效')
  expect(wrapper.findComponent(Feed).exists()).toBe(false)
  expect(router.currentRoute.value.query.q).toBe('attempted bypass')
  await wrapper.get('.main-search input').setValue('repaired')
  await wrapper.get('form').trigger('submit'); await flushPromises()
  expect(wrapper.findComponent(Feed).exists()).toBe(false)
  await wrapper.findAll('button').find(v => v.text() === '更新当前视图条件')!.trigger('click'); await flushPromises()
  expect(DefaultService.updateSavedSearch).toHaveBeenCalledWith(id, expect.objectContaining({ conditions: expect.objectContaining({ q: 'repaired', tagIds: [] }) }))
  expect(wrapper.findComponent(Feed).exists()).toBe(true)
  await wrapper.findAll('button').find(v => v.text() === '删除视图')!.trigger('click'); await flushPromises()
  expect(DefaultService.deleteSavedSearch).toHaveBeenCalledWith(id)
  wrapper.unmount(); client.clear()
})

it('shows the backend invalid reason for an unreadable view and permits immediate deletion', async () => {
  items[0]!.conditions = null as never
  items[0]!.invalidReason = '保存的条件无法读取，请删除或更新视图。'
  const { wrapper, client } = await render({ saved: id })
  expect(wrapper.text()).toContain(items[0]!.invalidReason)
  expect(wrapper.findComponent(Feed).exists()).toBe(false)
  await wrapper.findAll('button').find(v => v.text() === '删除视图')!.trigger('click'); await flushPromises()
  expect(DefaultService.deleteSavedSearch).toHaveBeenCalledWith(id)
  wrapper.unmount(); client.clear()
})

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(done => { resolve = done })
  return { promise, resolve }
}
it.each(['24h', '7d', '30d'])('preserves batch, relative baseline and draft through detail/attachment navigation for %s', async time => {
  const { wrapper, router, client } = await render({ time })
  const feed = wrapper.findComponent(Feed)
  const batch = feed.vm.batch
  batch.active.value = true; batch.selectLoaded()
  const originalFilters = JSON.stringify(feed.props('filters'))
  await wrapper.get('.main-search input').setValue('draft keyword')
  const clock = vi.spyOn(Date, 'now').mockReturnValue(Date.now() + 60000)
  await router.push({ query: { time, detail: 'message', attachment: 'file' } }); await flushPromises()
  await router.push({ query: { time } }); await flushPromises()
  expect(batch.count.value).toBe(5)
  expect(batch.active.value).toBe(true)
  expect(JSON.stringify(feed.props('filters'))).toBe(originalFilters)
  expect(wrapper.get('.main-search input').element).toHaveProperty('value', 'draft keyword')
  const pending = deferred<ReturnType<typeof messageFixture>>()
  const trash = vi.spyOn(DefaultService, 'trashMessage').mockImplementation(() => new CancelablePromise(resolve => { pending.promise.then(resolve) }))
  const operation = batch.run('trash')
  await router.push({ query: { time, detail: 'message' } }); await flushPromises()
  expect(batch.busy.value).toBe(true)
  pending.resolve(messageFixture()); await operation
  expect(trash).toHaveBeenCalledTimes(5)
  batch.selectLoaded()
  await router.push({ query: { time, q: 'new keyword' } }); await flushPromises()
  expect(batch.count.value).toBe(0)
  expect(batch.active.value).toBe(false)
  clock.mockRestore(); wrapper.unmount(); client.clear()
})
it('uses fresh server conditions on cached reopen, including a new relative baseline', async () => {
  const cache = structuredClone(items)
  items = [{ ...items[0]!, name: '最新', conditions: readConditions({ q: 'server keyword', time: '30d' }) }]
  const { wrapper, router, client } = await render({ saved: id }, cache)
  expect(router.currentRoute.value.query.q).toBe('server keyword')
  expect(wrapper.get('.main-search input').element).toHaveProperty('value', 'server keyword')
  const before = wrapper.findComponent(Feed).props('filters').search.createdAfter
  const clock = vi.spyOn(Date, 'now').mockReturnValue(Date.now() + 60000)
  await router.push({ query: { saved: id } }); await flushPromises()
  expect(wrapper.findComponent(Feed).props('filters').search.createdAfter).not.toBe(before)
  clock.mockRestore(); wrapper.unmount(); client.clear()
})
it('does not route an old refresh into a subsequently opened view', async () => {
  const other = { ...items[0]!, id: 'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb', name: 'Other', conditions: readConditions({ q: 'other keyword', time: '24h' }) }
  const pending = deferred<SavedSearch[]>()
  vi.mocked(DefaultService.listSavedSearches).mockImplementation(() => new CancelablePromise(resolve => { pending.promise.then(resolve) }))
  const { wrapper, router, client } = await render({ saved: id }, [...items, other])
  await router.push({ query: { saved: other.id } }); await flushPromises()
  pending.resolve([...items, other]); await flushPromises()
  expect(router.currentRoute.value.query.saved).toBe(other.id)
  expect(router.currentRoute.value.query.q).toBe('other keyword')
  wrapper.unmount(); client.clear()
})
it.each(['form', 'name'])('preserves unsaved %s during background refresh and supports explicit adoption', async field => {
  const { wrapper, router, client } = await render({ saved: id })
  await wrapper.get(field === 'form' ? '.main-search input' : 'input[maxlength="100"]').setValue('unsaved draft')
  // A submitted filter remains an unsaved view change as well.
  if (field === 'form') { await wrapper.get('form').trigger('submit'); await flushPromises() }
  client.setQueryData(savedSearchKey, [{ ...items[0]!, name: 'Server name', conditions: readConditions({ q: 'server change', time: '30d' }) }])
  await flushPromises()
  expect(wrapper.get(field === 'form' ? '.main-search input' : 'input[maxlength="100"]').element).toHaveProperty('value', 'unsaved draft')
  expect(router.currentRoute.value.query.q).toBe(field === 'form' ? 'unsaved draft' : 'hello')
  expect(wrapper.text()).toContain('保留了未保存的编辑')
  await wrapper.findAll('button').find(v => v.text() === '采用服务端最新条件')!.trigger('click'); await flushPromises()
  expect(router.currentRoute.value.query.q).toBe('server change')
  expect(wrapper.get('input[maxlength="100"]').element).toHaveProperty('value', 'Server name')
  wrapper.unmount(); client.clear()
})
it('preserves drafts made while a cached reopen is fetching', async () => {
  const pending = deferred<SavedSearch[]>()
  vi.mocked(DefaultService.listSavedSearches).mockImplementation(() => new CancelablePromise(resolve => { pending.promise.then(resolve) }))
  const { wrapper, router, client } = await render({ saved: id }, items)
  await wrapper.get('.main-search input').setValue('pending draft')
  pending.resolve([{ ...items[0]!, conditions: readConditions({ q: 'fresh response', time: '7d' }) }]); await flushPromises()
  expect(wrapper.get('.main-search input').element).toHaveProperty('value', 'pending draft')
  expect(wrapper.findComponent(Feed).exists()).toBe(false)
  expect(router.currentRoute.value.query.time).toBeUndefined()
  expect(wrapper.text()).toContain('保留了未保存的编辑')
  wrapper.unmount(); client.clear()
})

it('ignores a delayed rename response after another view is opened', async () => {
  const other = { ...items[0]!, id: 'bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb', name: 'Other', conditions: readConditions({ q: 'other keyword', time: '24h' }) }
  items.push(other)
  const { wrapper, router, client } = await render({ saved: id })
  const pending = deferred<SavedSearch>()
  vi.mocked(DefaultService.updateSavedSearch).mockImplementation(() => new CancelablePromise(resolve => { pending.promise.then(resolve) }))
  await wrapper.get('input[maxlength="100"]').setValue('Delayed name')
  await wrapper.findAll('button').find(v => v.text() === '重命名')!.trigger('click')
  await router.push({ query: { saved: other.id } }); await flushPromises()
  pending.resolve({ ...items[0]!, name: 'Delayed name' }); await flushPromises()
  expect(router.currentRoute.value.query.saved).toBe(other.id)
  expect(router.currentRoute.value.query.q).toBe('other keyword')
  expect(wrapper.get('input[maxlength="100"]').element).toHaveProperty('value', 'Other')
  wrapper.unmount(); client.clear()
})
it.each(['deleted', 'invalid', 'tag'])('pauses an open view when a background update is %s even with a draft', async kind => {
  const { wrapper, client } = await render({ saved: id })
  await wrapper.get('.main-search input').setValue('unsaved draft')
  const item = { ...items[0]!, conditions: readConditions({ q: 'remote keyword', time: 'all' }) }
  if (kind === 'invalid') item.conditions = null as never
  if (kind === 'tag') item.conditions.tagIds = ['bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb']
  client.setQueryData(savedSearchKey, kind === 'deleted' ? [] : [item]); await flushPromises()
  expect(wrapper.findComponent(Feed).exists()).toBe(false)
  expect(wrapper.get('.main-search input').element).toHaveProperty('value', 'unsaved draft')
  wrapper.unmount(); client.clear()
})
it('can retry a failed cached open and then apply the latest server conditions', async () => {
  const cache = structuredClone(items)
  vi.mocked(DefaultService.listSavedSearches).mockRejectedValue(new Error('offline'))
  const { wrapper, router, client } = await render({ saved: id }, cache)
  expect(wrapper.findComponent(Feed).exists()).toBe(false)
  items = [{ ...items[0]!, conditions: readConditions({ q: 'retry keyword', time: '30d' }) }]
  vi.mocked(DefaultService.listSavedSearches).mockImplementation(() => new CancelablePromise(resolve => resolve(items)))
  await wrapper.findAll('button').find(v => v.text() === '重试')!.trigger('click'); await flushPromises()
  expect(router.currentRoute.value.query.q).toBe('retry keyword')
  expect(wrapper.findComponent(Feed).exists()).toBe(true)
  wrapper.unmount(); client.clear()
})

it('prompts instead of silently discarding drafts on a same-view shortcut reopen', async () => {
  const { wrapper, router, client } = await render({ saved: id })
  await wrapper.get('.main-search input').setValue('filter draft')
  await wrapper.get('input[maxlength="100"]').setValue('name draft')
  items = [{ ...items[0]!, conditions: readConditions({ q: 'latest conditions', time: '30d' }) }]
  await router.push({ query: { saved: id } }); await flushPromises()
  expect(wrapper.get('.main-search input').element).toHaveProperty('value', 'filter draft')
  expect(wrapper.get('input[maxlength="100"]').element).toHaveProperty('value', 'name draft')
  expect(wrapper.text()).toContain('保留了未保存的编辑')
  await wrapper.findAll('button').find(v => v.text() === '采用服务端最新条件')!.trigger('click'); await flushPromises()
  expect(router.currentRoute.value.query.q).toBe('latest conditions')
  wrapper.unmount(); client.clear()
})
