import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { describe, expect, it, vi } from 'vitest'
import { BodyFormat, DefaultService } from '@/api/generated'
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

  it('re-assigns a historical TEXT body to Shell and re-edits it as code', async () => {
    vi.spyOn(DefaultService, 'getMessage').mockResolvedValue(messageFixture({ body: 'sudo systemctl restart nginx' }))
    vi.spyOn(DefaultService, 'listTags').mockResolvedValue([])
    const edit = vi.spyOn(DefaultService, 'editMessage').mockResolvedValue(messageFixture())
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/temporary', component: { template: '<div />' } }],
    })
    await router.push('/temporary')
    await router.isReady()
    const wrapper = mount(MessageDetailSurface, {
      global: {
        plugins: [router, [VueQueryPlugin, { queryClient: new QueryClient({ defaultOptions: { queries: { retry: false } } }) }]],
        stubs: { teleport: true },
      },
    })

    await router.push({ query: { detail: 'message-1' } })
    await flushPromises()

    // Plain TEXT edits as 纯文本 even though the server may flag hints.
    const editButton = wrapper.findAll('button').find((button) => button.text() === '编辑')
    expect(editButton).toBeTruthy()
    await editButton!.trigger('click')
    await flushPromises()
    expect(wrapper.get('textarea').element.value).toBe('sudo systemctl restart nginx')

    await wrapper.get('button[aria-label="内容类型"]').trigger('click')
    const shell = wrapper.findAll('[role="option"]').find((option) => option.attributes('aria-label') === 'Shell')
    expect(shell).toBeTruthy()
    await shell!.trigger('click')
    // jsdom does not run submit activation on button clicks.
    await wrapper.get('form.edit').trigger('submit')
    await flushPromises()

    expect(edit).toHaveBeenCalledWith('message-1', expect.objectContaining({
      body: '```bash\nsudo systemctl restart nginx\n```',
      bodyFormat: BodyFormat.MARKDOWN,
    }))
    wrapper.unmount()
  })

  it('edits a stored fenced block as its language without double fencing', async () => {
    const stored = '```python\nprint("x")\n```'
    vi.spyOn(DefaultService, 'getMessage').mockResolvedValue(messageFixture({ body: stored, bodyFormat: BodyFormat.MARKDOWN }))
    vi.spyOn(DefaultService, 'listTags').mockResolvedValue([])
    const edit = vi.spyOn(DefaultService, 'editMessage').mockResolvedValue(messageFixture())
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/temporary', component: { template: '<div />' } }],
    })
    await router.push('/temporary')
    await router.isReady()
    const wrapper = mount(MessageDetailSurface, {
      global: {
        plugins: [router, [VueQueryPlugin, { queryClient: new QueryClient({ defaultOptions: { queries: { retry: false } } }) }]],
        stubs: { teleport: true },
      },
    })

    await router.push({ query: { detail: 'message-1' } })
    await flushPromises()
    await wrapper.findAll('button').find((button) => button.text() === '编辑')!.trigger('click')
    await flushPromises()
    // Recognized single fenced block edits as Python with the fence stripped.
    expect(wrapper.get('textarea').element.value).toBe('print("x")')
    expect(wrapper.get('button[aria-label="内容类型"]').text()).toContain('Python')

    await wrapper.get('form.edit').trigger('submit')
    await flushPromises()
    expect(edit).toHaveBeenCalledWith('message-1', expect.objectContaining({ body: stored, bodyFormat: BodyFormat.MARKDOWN }))
    wrapper.unmount()
  })
})
