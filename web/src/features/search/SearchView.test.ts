import { mount, flushPromises } from '@vue/test-utils'
import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { createRouter, createMemoryHistory } from 'vue-router'
import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { CancelablePromise, DefaultService, type SavedSearch } from '@/api/generated'
import SearchView from './SearchView.vue'
import { readConditions } from './conditions'

vi.mock('@/features/tags/queries', () => ({ useTagsQuery: () => ({ data: ref([]), error: ref(null) }) }))
const Feed = { props: ['filters'], template: '<div data-test="feed" />' }
let items: SavedSearch[]
const id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
async function render(query: Record<string, string>) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/search', name: 'search', component: SearchView }] })
  await router.push({ name: 'search', query })
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
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
