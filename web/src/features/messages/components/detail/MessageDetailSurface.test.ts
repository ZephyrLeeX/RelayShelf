import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
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

  it('displays, edits, and clears the optional title together with the body', async () => {
    vi.spyOn(DefaultService, 'getMessage').mockResolvedValue(messageFixture({ title: 'Old title', body: 'detail body' }))
    vi.spyOn(DefaultService, 'listTags').mockResolvedValue([])
    const edit = vi.spyOn(DefaultService, 'editMessage').mockResolvedValue(messageFixture({ title: 'New title' }))
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/temporary', component: { template: '<div />' } }] })
    await router.push('/temporary?detail=message-1')
    await router.isReady()
    const wrapper = mount(MessageDetailSurface, {
      global: { plugins: [router, [VueQueryPlugin, { queryClient: new QueryClient({ defaultOptions: { queries: { retry: false } } }) }]], stubs: { teleport: true } },
    })
    await flushPromises()
    expect(wrapper.get('.message-title').text()).toBe('Old title')
    await wrapper.findAll('button').find((button) => button.text() === '编辑')!.trigger('click')
    const title = wrapper.get<HTMLInputElement>('form.edit input[placeholder="标题（可选）"]')
    await title.setValue('  New title  ')
    await wrapper.get('form.edit').trigger('submit')
    await flushPromises()
    expect(edit).toHaveBeenCalledWith('message-1', expect.objectContaining({ title: 'New title', body: 'detail body' }))

    await wrapper.findAll('button').find((button) => button.text() === '编辑')!.trigger('click')
    const clearTitle = wrapper.get<HTMLInputElement>('form.edit input[placeholder="标题（可选）"]')
    await clearTitle.setValue('   ')
    await wrapper.get('form.edit').trigger('submit')
    await flushPromises()
    expect(edit).toHaveBeenLastCalledWith('message-1', expect.objectContaining({ title: '' }))
    wrapper.unmount()
  })

  it('creates a tag in the shared detail picker, auto-selects it, and saves the replacement', async () => {
    const existing = { id: 'tag-existing', name: '工作', color: '#112233', createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z' }
    const created = { id: 'tag-created', name: '服务器', color: '#3B8C6E', createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z' }
    vi.spyOn(DefaultService, 'getMessage').mockResolvedValue(messageFixture({ tags: [existing] }))
    vi.spyOn(DefaultService, 'listTags').mockResolvedValueOnce([existing]).mockResolvedValue([existing, created])
    vi.spyOn(DefaultService, 'createTag').mockResolvedValue(created)
    const replace = vi.spyOn(DefaultService, 'replaceMessageTags').mockResolvedValue(messageFixture({ tags: [existing, created], version: 2 }))
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/temporary', component: { template: '<div />' } }] })
    await router.push('/temporary?detail=message-1')
    await router.isReady()
    const wrapper = mount(MessageDetailSurface, {
      global: { plugins: [router, [VueQueryPlugin, { queryClient: new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } }) }]], stubs: { teleport: true } },
    })
    await flushPromises()
    await wrapper.get('button[aria-label="编辑标签"]').trigger('click')
    expect(wrapper.get<HTMLInputElement>('input[value="tag-existing"]').element.checked).toBe(true)
    await wrapper.get('input[aria-label="新建标签名称"]').setValue(' 服务器 ')
    await wrapper.findAll('.new-tag button').at(0)!.trigger('click')
    await flushPromises()
    expect(DefaultService.createTag).toHaveBeenCalledWith({ name: '服务器', color: '#3B8C6E' })
    expect(wrapper.get<HTMLInputElement>('input[value="tag-created"]').element.checked).toBe(true)
    await wrapper.findAll('.save-tags').at(0)!.trigger('click')
    await flushPromises()
    expect(replace).toHaveBeenCalledWith('message-1', { expectedVersion: 1, tagIds: ['tag-existing', 'tag-created'] })
    wrapper.unmount()
  })

  describe('message editing validation', () => {
    const attachment = { id:'a1', originalFilename:'notes.txt', clientMime:'text/plain', detectedMime:'text/plain', sizeBytes:12, displayOrder:0 }

    beforeEach(() => vi.restoreAllMocks())

    async function renderEditor(overrides = {}) {
      vi.spyOn(DefaultService, 'getMessage').mockResolvedValue(messageFixture({ attachments: [attachment], attachmentCount: 1, ...overrides }))
      vi.spyOn(DefaultService, 'listTags').mockResolvedValue([])
      const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/temporary', component: { template: '<div />' } }] })
      await router.push('/temporary?detail=message-1')
      await router.isReady()
      const wrapper = mount(MessageDetailSurface, {
        attachTo: document.body,
        global: { plugins: [router, [VueQueryPlugin, { queryClient: new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } }) }]], stubs: { teleport: true } },
      })
      await flushPromises()
      return { router, wrapper }
    }

    it('allows an ordinary message with attachments to clear its body', async () => {
      const edit = vi.spyOn(DefaultService, 'editMessage').mockResolvedValue(messageFixture({ body: null, bodyPreview: null, attachments: [attachment], attachmentCount: 1 }))
      const { wrapper } = await renderEditor()
      await wrapper.get('button[title="编辑正文"]').trigger('click')
      const textarea = wrapper.get<HTMLTextAreaElement>('form.edit textarea')
      expect(textarea.attributes('required')).toBeUndefined()
      await textarea.setValue('   ')
      await wrapper.get('form.edit').trigger('submit')
      await flushPromises()
      expect(edit).toHaveBeenCalledWith('message-1', expect.objectContaining({ body: null }))
      wrapper.unmount()
    })

    it('rejects an empty sensitive body even when attachments exist', async () => {
      vi.spyOn(DefaultService, 'revealSensitiveBody').mockResolvedValue({ body: 'secret', version: 1 })
      const edit = vi.spyOn(DefaultService, 'editSensitiveBody').mockResolvedValue(messageFixture({ sensitive: true, body: null, attachments: [attachment], attachmentCount: 1 }))
      const { wrapper } = await renderEditor({ sensitive: true, body: null, bodyPreview: null })
      await wrapper.get('.sensitive .button.primary').trigger('click')
      await flushPromises()
      await wrapper.get('button[title="编辑正文"]').trigger('click')
      const textarea = wrapper.get<HTMLTextAreaElement>('form.edit textarea')
      expect(textarea.attributes('required')).toBeDefined()
      await textarea.setValue('   ')
      await wrapper.get('form.edit').trigger('submit')
      await flushPromises()
      expect(edit).not.toHaveBeenCalled()
      wrapper.unmount()
    })

    it('submits a non-empty sensitive body when attachments exist', async () => {
      vi.spyOn(DefaultService, 'revealSensitiveBody').mockResolvedValue({ body: 'secret', version: 1 })
      const edit = vi.spyOn(DefaultService, 'editSensitiveBody').mockResolvedValue(messageFixture({ sensitive: true, body: null, version: 2, attachments: [attachment], attachmentCount: 1 }))
      const { wrapper } = await renderEditor({ sensitive: true, body: null, bodyPreview: null })
      await wrapper.get('.sensitive .button.primary').trigger('click')
      await flushPromises()
      await wrapper.get('button[title="编辑正文"]').trigger('click')
      await wrapper.get<HTMLTextAreaElement>('form.edit textarea').setValue('updated secret')
      await wrapper.get('form.edit').trigger('submit')
      await flushPromises()
      expect(edit).toHaveBeenCalledWith('message-1', { expectedVersion: 1, title: '', body: 'updated secret' })
      wrapper.unmount()
    })

    it('keeps the detail open when Escape closes the tag picker', async () => {
      const { router, wrapper } = await renderEditor()
      const trigger = wrapper.get<HTMLButtonElement>('button[aria-label="编辑标签"]')
      await trigger.trigger('click')
      const input = wrapper.get<HTMLInputElement>('input[aria-label="新建标签名称"]')
      input.element.focus()
      await input.trigger('keydown', { key: 'Escape' })
      await flushPromises()
      expect(wrapper.find('[role="dialog"][aria-label="选择消息标签"]').exists()).toBe(false)
      expect(router.currentRoute.value.query.detail).toBe('message-1')
      expect(document.activeElement).toBe(trigger.element)

      await trigger.trigger('keydown', { key: 'Escape' })
      await flushPromises()
      expect(router.currentRoute.value.query.detail).toBeUndefined()
      wrapper.unmount()
    })
  })
})
