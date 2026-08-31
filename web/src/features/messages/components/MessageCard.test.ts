import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, BodyFormat, DefaultService, Lifecycle } from '@/api/generated'
import { messageFixture } from '@/test/fixtures'
import MessageCard from './MessageCard.vue'

async function render(overrides = {}, trash = false) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: { template: '<div />' } }, { path: '/messages/:id', name: 'message-detail', component: { template: '<div />' } }] })
  await router.push('/'); await router.isReady()
  return { router, wrapper: mount(MessageCard, { props: { message: messageFixture(overrides), trash }, global: { plugins: [router, [VueQueryPlugin, { queryClient: new QueryClient() }]] } }) }
}

describe('MessageCard', () => {
  const writeText = vi.fn().mockResolvedValue(undefined)

  beforeEach(() => {
    writeText.mockClear()
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
  })

  it('never renders a sensitive body preview', async () => {
    const { wrapper } = await render({ sensitive: true, bodyPreview: 'must-not-leak' })
    expect(wrapper.text()).toContain('敏感内容已锁定')
    expect(wrapper.text()).not.toContain('must-not-leak')
  })
  it('hides favorite for temporary messages and offers it for permanent messages', async () => {
    expect((await render({ lifecycle: Lifecycle.TEMPORARY })).wrapper.text()).not.toContain('收藏')
    expect((await render({ lifecycle: Lifecycle.PERMANENT })).wrapper.text()).toContain('收藏')
  })
  it('does not offer preview copying as full body when truncated', async () => {
    const { router, wrapper } = await render({ bodyTruncated: true })
    expect(wrapper.text()).toContain('打开并复制')
    await wrapper.get('[aria-label="打开详情后复制正文"]').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(writeText).not.toHaveBeenCalled()
    expect(router.currentRoute.value.query.detail).toBe('message-1')
  })
  it('copies a complete ordinary preview without opening detail', async () => {
    const { router, wrapper } = await render({ bodyPreview: 'copy this' })
    await wrapper.get('[aria-label="复制正文"]').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(writeText).toHaveBeenCalledWith('copy this')
    expect(router.currentRoute.value.query.detail).toBeUndefined()
  })
  it('opens detail instead of copying sensitive content', async () => {
    const { router, wrapper } = await render({ sensitive: true, bodyPreview: null })
    await wrapper.get('[aria-label="打开详情后复制正文"]').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(writeText).not.toHaveBeenCalled()
    expect(router.currentRoute.value.query.detail).toBe('message-1')
  })
  it('does not render body copy for an attachment-only message', async () => {
    const attachment = { id:'a1', originalFilename:'notes.txt', clientMime:'text/plain', detectedMime:'text/plain', sizeBytes:12, displayOrder:0 }
    const { wrapper } = await render({ body:null, bodyPreview:null, attachments:[attachment], attachmentCount:1 })
    expect(wrapper.find('[aria-label="复制正文"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('仅附件内容')
  })
  it('uses detected MIME, not the client MIME, as image thumbnail authority', async () => {
    const disguised = { id:'unsafe', originalFilename:'unsafe.svg', clientMime:'image/png', detectedMime:'image/svg+xml', sizeBytes:12, displayOrder:0 }
    const safe = { id:'safe', originalFilename:'photo.jpg', clientMime:null, detectedMime:'image/jpeg', sizeBytes:24, displayOrder:1 }
    const { wrapper } = await render({ attachments:[disguised, safe], attachmentCount:2 })
    const cards = wrapper.findAll('.attachment-card')
    expect(cards[0].find('img').exists()).toBe(false)
    expect(cards[0].text()).toContain('FILE')
    expect(cards[1].get('img').attributes('src')).toContain('/api/v1/attachments/safe/thumbnail')
    expect(cards[1].get('img').attributes('loading')).toBe('lazy')
  })
  it('downloads feed attachments through the authenticated path without opening detail', async () => {
    const attachment = { id:'file/id', originalFilename:'report.pdf', clientMime:'application/pdf', detectedMime:'application/pdf', sizeBytes:24, displayOrder:0 }
    const { router, wrapper } = await render({ attachments:[attachment], attachmentCount:1 })
    const download = wrapper.get<HTMLAnchorElement>('[aria-label="下载 report.pdf"]')

    expect(download.attributes('href')).toBe('/api/v1/attachments/file%2Fid/download')
    download.element.addEventListener('click', (event) => event.preventDefault())
    await download.trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(router.currentRoute.value.query.detail).toBeUndefined()
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
    await wrapper.get('.body-content').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(router.currentRoute.value.query.detail).toBe('message-1')
    expect(router.currentRoute.value.path).toBe('/')
    expect(wrapper.classes()).toContain('selected')
  })
  it('renders markdown bodies through the safe renderer with a fence-language badge', async () => {
    const { wrapper } = await render({ bodyFormat: BodyFormat.MARKDOWN, bodyPreview: '```python\nprint("x")\n```' })
    await vi.waitFor(() => expect(wrapper.find('.feed-markdown pre code').exists()).toBe(true))
    expect(wrapper.find('pre').text()).toContain('print("x")')
    expect(wrapper.get('.type-badge').text()).toBe('PY')
  })
  it('shows the MD badge for ordinary markdown bodies', async () => {
    const { wrapper } = await render({ bodyFormat: BodyFormat.MARKDOWN, bodyPreview: '# title' })
    await flushPromises()
    expect(wrapper.get('.type-badge').text()).toBe('MD')
    expect(wrapper.find('pre.code').exists()).toBe(false)
  })
  it('keeps the legacy TEXT+detectedLanguage rendering and badge', async () => {
    const { wrapper } = await render({ bodyFormat: BodyFormat.TEXT, detectedType: 'CODE', detectedLanguage: 'shell', bodyPreview: 'ls -la' })
    expect(wrapper.get('pre.code').text()).toBe('ls -la')
    expect(wrapper.get('.type-badge').text()).toBe('SHELL')
  })
  it('linkifies URLs in plain text with safe hrefs and no detail opening', async () => {
    const { router, wrapper } = await render({ bodyPreview: '下载：https://example.com/file.zip 结束 javascript:no' })
    const link = wrapper.get('a[href="https://example.com/file.zip"]')
    expect(link.attributes('target')).toBe('_blank')
    expect(link.attributes('rel')).toBe('noopener noreferrer')
    expect(wrapper.findAll('a')).toHaveLength(1)

    link.element.addEventListener('click', (event) => event.preventDefault())
    await link.trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(router.currentRoute.value.query.detail).toBeUndefined()
  })
  it('opens detail when clicking the body outside links', async () => {
    const { router, wrapper } = await render({ bodyPreview: '见 https://example.com/a 说明' })
    await wrapper.get('.body-content').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(router.currentRoute.value.query.detail).toBe('message-1')
  })
  it('keeps an explicit keyboard-accessible detail button and no full-size overlay', async () => {
    const { router, wrapper } = await render({ bodyPreview: 'select this text' })
    // The body stays plain selectable text: no transparent button covers it.
    expect(wrapper.find('.body-region button').exists()).toBe(false)
    const detail = wrapper.get('button[aria-label="打开内容 message-1"]')
    expect(detail.attributes('title')).toBe('打开内容')
    await detail.trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(router.currentRoute.value.query.detail).toBe('message-1')
  })
  it('does not open detail when a click ends an active text selection', async () => {
    const { router, wrapper } = await render({ bodyPreview: 'select some of this text' })
    const getSelection = vi.spyOn(window, 'getSelection')
      .mockReturnValue({ rangeCount: 1, isCollapsed: false } as unknown as Selection)
    await wrapper.get('.body-content').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(router.currentRoute.value.query.detail).toBeUndefined()

    // An ordinary click (no selection) still opens the detail.
    getSelection.mockReturnValue({ rangeCount: 1, isCollapsed: true } as unknown as Selection)
    await wrapper.get('.body-content').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(router.currentRoute.value.query.detail).toBe('message-1')
    getSelection.mockRestore()
  })
  it('copies the bare code for a body that is exactly one fenced block', async () => {
    const { wrapper } = await render({ bodyFormat: BodyFormat.MARKDOWN, bodyPreview: '```bash\ndocker compose up -d\n```' })
    await wrapper.get('[aria-label="复制正文"]').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(writeText).toHaveBeenCalledWith('docker compose up -d')
  })
  it('keeps full copy semantics for prose markdown with embedded fences', async () => {
    const body = '# title\n\n```bash\ncmd\n```\n\noutro'
    const { wrapper } = await render({ bodyFormat: BodyFormat.MARKDOWN, bodyPreview: body })
    await flushPromises()
    await wrapper.get('[aria-label="复制正文"]').trigger('click')
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(writeText).toHaveBeenCalledWith(body)
  })
})
