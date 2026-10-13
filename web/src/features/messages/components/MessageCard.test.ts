import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { BodyFormat, DefaultService, Lifecycle } from '@/api/generated'
import { messageFixture } from '@/test/fixtures'
import MessageCard from './MessageCard.vue'

async function render(overrides = {}, batchMode = false) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/temporary', component: { template: '<div />' } }] })
  await router.push('/temporary?q=keep'); await router.isReady()
  const wrapper = mount(MessageCard, {
    props: { message: messageFixture(overrides), batchMode },
    global: { plugins: [router, [VueQueryPlugin, { queryClient: new QueryClient({ defaultOptions: { queries: { retry: false } } }) }]], stubs: { teleport: true } },
  })
  await flushPromises()
  return { router, wrapper }
}

describe('MessageCard inline content and actions', () => {
  const writeText = vi.fn().mockResolvedValue(undefined)
  beforeEach(() => {
    vi.restoreAllMocks()
    writeText.mockClear()
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    vi.spyOn(DefaultService, 'listTags').mockResolvedValue([])
  })

  it('shows the full ordinary body without a detail screen or extra request', async () => {
    const getMessage = vi.spyOn(DefaultService, 'getMessage')
    const body = 'long line\n'.repeat(80)
    const { router, wrapper } = await render({ title: 'Title', bodyPreview: body })
    expect(wrapper.get('.message-title').text()).toBe('Title')
    expect(wrapper.get('.plain-body').text()).toContain(body.trim())
    expect(wrapper.find('.collapsed').exists()).toBe(false)
    expect(wrapper.find('.back-button').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('内容详情')
    await wrapper.get('.body-section').trigger('click')
    expect(router.currentRoute.value.fullPath).toBe('/temporary?q=keep')
    expect(getMessage).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('fetches and copies a truncated body in the same card', async () => {
    const full = 'complete body beyond preview'
    const getMessage = vi.spyOn(DefaultService, 'getMessage').mockResolvedValue(messageFixture({ body: full }))
    const { router, wrapper } = await render({ bodyPreview: 'complete', bodyTruncated: true })
    expect(getMessage).toHaveBeenCalledWith('message-1')
    expect(wrapper.get('.plain-body').text()).toBe(full)
    await wrapper.get('[aria-label="复制正文"]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith(full)
    expect(router.currentRoute.value.query.detail).toBeUndefined()
    wrapper.unmount()
  })

  it('does not expose sensitive previews and reveals them inline', async () => {
    vi.spyOn(DefaultService, 'revealSensitiveBody').mockResolvedValue({ body: 'secret', version: 1 })
    const { wrapper } = await render({ sensitive: true, bodyPreview: 'must-not-leak' })
    expect(wrapper.text()).not.toContain('must-not-leak')
    expect(wrapper.text()).toContain('敏感内容已锁定')
    await wrapper.get('.sensitive .button.primary').trigger('click')
    await flushPromises()
    expect(wrapper.get('.sensitive pre').text()).toBe('secret')
    await wrapper.findAll('button').find(button => button.text() === '隐藏')!.trigger('click')
    expect(wrapper.text()).not.toContain('secret')
    wrapper.unmount()
  })

  it('edits and saves in place with the version and full body', async () => {
    const edit = vi.spyOn(DefaultService, 'editMessage').mockResolvedValue(messageFixture({ version: 2 }))
    const { router, wrapper } = await render({ bodyPreview: 'old body' })
    await wrapper.get('button[title="编辑正文"]').trigger('click')
    expect(wrapper.get<HTMLTextAreaElement>('textarea').element.value).toBe('old body')
    await wrapper.get('textarea').setValue('new body')
    await wrapper.get('form.edit').trigger('submit')
    await flushPromises()
    expect(edit).toHaveBeenCalledWith('message-1', expect.objectContaining({ body: 'new body', expectedVersion: 1 }))
    expect(router.currentRoute.value.query.detail).toBeUndefined()
    wrapper.unmount()
  })

  it('refreshes the displayed body and edit version when the feed changes', async () => {
    const { wrapper } = await render({ bodyPreview: 'old' })
    await wrapper.setProps({ message: messageFixture({ version: 2, bodyPreview: 'updated' }) })
    expect(wrapper.get('.plain-body').text()).toBe('updated')
    await wrapper.get('button[title="编辑正文"]').trigger('click')
    expect(wrapper.get<HTMLTextAreaElement>('textarea').element.value).toBe('updated')
    wrapper.unmount()
  })

  it('preserves an inline draft during a same-version feed refresh', async () => {
    const { wrapper } = await render({ bodyPreview: 'stored' })
    await wrapper.get('button[title="编辑正文"]').trigger('click')
    await wrapper.get('textarea').setValue('unsaved draft')
    await wrapper.setProps({ message: messageFixture({ bodyPreview: 'stored' }) })
    expect(wrapper.get<HTMLTextAreaElement>('textarea').element.value).toBe('unsaved draft')
    wrapper.unmount()
  })

  it('keeps markdown rendering and bare code copy semantics', async () => {
    const { wrapper } = await render({ bodyFormat: BodyFormat.MARKDOWN, bodyPreview: '```bash\ndocker compose ps\n```' })
    await vi.waitFor(() => expect(wrapper.get('.safe-markdown pre code').text()).toBe('docker compose ps'))
    await wrapper.get('[aria-label="复制正文"]').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith('docker compose ps')
    wrapper.unmount()
  })

  it('shows all attachments with download and remove controls', async () => {
    const attachments = Array.from({ length: 4 }, (_, i) => ({ id: `file-${i}`, originalFilename: `file-${i}.txt`, clientMime: 'text/plain', detectedMime: 'text/plain', sizeBytes: 12, displayOrder: i }))
    const getMessage = vi.spyOn(DefaultService, 'getMessage').mockResolvedValue(messageFixture({ attachments, attachmentCount: 4 }))
    const { wrapper } = await render({ attachments: attachments.slice(0, 3), attachmentCount: 4 })
    expect(getMessage).toHaveBeenCalledWith('message-1')
    expect(wrapper.findAll('.attachment-list .row')).toHaveLength(4)
    expect(wrapper.get('[aria-label="下载 file-3.txt"]').attributes('href')).toBe('/api/v1/attachments/file-3/download')
    expect(wrapper.findAll('.remove')).toHaveLength(4)
    expect(wrapper.find('.archive-download').exists()).toBe(true)
    wrapper.unmount()
  })

  it('offers tag, attachment, forward, extend and sensitive controls inline', async () => {
    const { router, wrapper } = await render()
    expect(wrapper.find('[aria-label="添加标签"]').exists()).toBe(true)
    expect(wrapper.find('[aria-label="添加附件"]').exists()).toBe(true)
    await wrapper.findAll('button').find(button => button.text() === '转发')!.trigger('click')
    expect(wrapper.find('.forward').exists()).toBe(true)
    await wrapper.get('button[title="延长有效期"]').trigger('click')
    expect(wrapper.text()).toContain('+7 天')
    await wrapper.get('[aria-label="更多操作"]').trigger('click')
    expect(wrapper.text()).toContain('设为敏感')
    expect(router.currentRoute.value.query.detail).toBeUndefined()
    wrapper.unmount()
  })

  it('uses trash semantics and hides modifications', async () => {
    const { wrapper } = await render({ trashedAt: '2026-01-02T00:00:00Z' })
    expect(wrapper.text()).toContain('恢复')
    expect(wrapper.text()).toContain('永久删除')
    expect(wrapper.find('[title="编辑正文"]').exists()).toBe(false)
    expect(wrapper.find('[aria-label="添加附件"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('hides individual actions while batch selection is active', async () => {
    const { wrapper } = await render({}, true)
    expect(wrapper.find('.actions').exists()).toBe(false)
    expect(wrapper.find('[aria-label="添加附件"]').exists()).toBe(false)
    expect(wrapper.find('[aria-label="编辑标签"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('offers favorites only on permanent messages', async () => {
    const { wrapper } = await render({ lifecycle: Lifecycle.PERMANENT })
    expect(wrapper.get('button[title="收藏"]').text()).toContain('收藏')
    expect(wrapper.find('[title="延长有效期"]').exists()).toBe(false)
    wrapper.unmount()
  })
})
