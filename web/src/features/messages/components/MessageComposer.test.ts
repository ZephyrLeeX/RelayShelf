import { VueQueryPlugin, QueryClient } from '@tanstack/vue-query'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { BodyFormat, DefaultService, Lifecycle } from '@/api/generated'
import { queryKeys } from '@/shared/api/queryKeys'
import { messageFixture } from '@/test/fixtures'
import MessageComposer from './MessageComposer.vue'

function mountComposer(lifecycle = Lifecycle.TEMPORARY) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return mount(MessageComposer, { props: { defaultLifecycle: lifecycle }, global: { plugins: [createPinia(), [VueQueryPlugin, { queryClient: client }]] } })
}

describe('MessageComposer', () => {
  beforeEach(() => {
    vi.spyOn(DefaultService, 'listTags').mockResolvedValue([])
    vi.stubGlobal('crypto', { randomUUID: vi.fn().mockReturnValueOnce('key-a').mockReturnValueOnce('key-b') })
  })
  it('keeps Enter as a newline and sends on Ctrl/Cmd+Enter', async () => {
    const create = vi.spyOn(DefaultService, 'createMessage').mockResolvedValue(messageFixture())
    const wrapper = mountComposer()
    const textarea = wrapper.get('textarea')
    await textarea.setValue('line one')
    await textarea.trigger('keydown', { key: 'Enter' })
    expect(create).not.toHaveBeenCalled()
    await textarea.trigger('keydown', { key: 'Enter', ctrlKey: true })
    await flushPromises()
    expect(create).toHaveBeenCalledWith('key-a', expect.objectContaining({ body: 'line one', bodyFormat: BodyFormat.TEXT }))
    await textarea.setValue('line two')
    await textarea.trigger('keydown', { key: 'Enter', metaKey: true })
    await flushPromises()
    expect(create).toHaveBeenLastCalledWith('key-b', expect.objectContaining({ body: 'line two' }))
  })
  it('blocks empty and over-1MiB UTF-8 bodies', async () => {
    const create = vi.spyOn(DefaultService, 'createMessage').mockResolvedValue(messageFixture())
    const wrapper = mountComposer()
    expect(wrapper.get('button.primary').attributes('disabled')).toBeDefined()
    await wrapper.get('textarea').setValue('界'.repeat(349_526))
    expect(wrapper.text()).toContain('正文 UTF-8 大小不能超过')
    expect(create).not.toHaveBeenCalled()
  })
  it('maps Markdown and Code UI to contract formats and allows untagged permanent content', async () => {
    const create = vi.spyOn(DefaultService, 'createMessage').mockResolvedValue(messageFixture())
    const wrapper = mountComposer(Lifecycle.PERMANENT)
    expect(wrapper.text()).toContain('长期内容尚未添加标签')
    await wrapper.get('textarea').setValue('# heading')
    await wrapper.findAll('.modes button')[1].trigger('click')
    await wrapper.get('textarea').trigger('keydown', { key: 'Enter', ctrlKey: true })
    await flushPromises()
    expect(create).toHaveBeenLastCalledWith('key-a', expect.objectContaining({ bodyFormat: BodyFormat.MARKDOWN, lifecycle: Lifecycle.PERMANENT }))
    await wrapper.get('textarea').setValue('echo ok')
    await wrapper.findAll('.modes button')[2].trigger('click')
    await wrapper.get('textarea').trigger('keydown', { key: 'Enter', ctrlKey: true })
    await flushPromises()
    expect(create).toHaveBeenLastCalledWith('key-b', expect.objectContaining({ bodyFormat: BodyFormat.TEXT }))
  })
  it('reuses one idempotency key for retry but resets it after draft changes', async () => {
    const create = vi.spyOn(DefaultService, 'createMessage').mockRejectedValueOnce(new TypeError('offline')).mockResolvedValueOnce(messageFixture()).mockResolvedValueOnce(messageFixture())
    const wrapper = mountComposer()
    await wrapper.get('textarea').setValue('uncertain send')
    await wrapper.get('textarea').trigger('keydown', { key: 'Enter', ctrlKey: true })
    await flushPromises()
    expect(create.mock.calls[0][0]).toBe('key-a')
    await wrapper.get('button.primary').trigger('click')
    await flushPromises()
    expect(create.mock.calls[1][0]).toBe('key-a')
    await wrapper.get('textarea').setValue('changed payload')
    await wrapper.get('textarea').trigger('keydown', { key: 'Enter', ctrlKey: true })
    await flushPromises()
    expect(create.mock.calls[2][0]).toBe('key-b')
  })
  it('binds a pending request to its payload snapshot and gives a changed draft a new key', async () => {
    let rejectFirst!: (reason: unknown) => void
    const pending = new Promise<never>((_resolve, reject) => { rejectFirst = reject })
    const create = vi.spyOn(DefaultService, 'createMessage').mockReturnValueOnce(pending as never).mockResolvedValueOnce(messageFixture())
    const wrapper = mountComposer()
    await wrapper.get('textarea').setValue('F1')
    await wrapper.get('textarea').trigger('keydown', { key: 'Enter', ctrlKey: true })
    await wrapper.get('textarea').setValue('F2')
    expect(create).toHaveBeenNthCalledWith(1, 'key-a', expect.objectContaining({ body:'F1' }))
    rejectFirst(new TypeError('offline'))
    await flushPromises()
    await wrapper.get('button.primary').trigger('click')
    await flushPromises()
    expect(create).toHaveBeenNthCalledWith(2, 'key-b', expect.objectContaining({ body:'F2' }))
  })
  it('blocks repeated Ctrl+Enter while an unchanged draft is pending', async () => {
    const pending = new Promise<ReturnType<typeof messageFixture>>(() => {})
    const create = vi.spyOn(DefaultService, 'createMessage').mockReturnValue(pending as never)
    const wrapper = mountComposer()
    const textarea = wrapper.get('textarea')
    await textarea.setValue('F1')
    await textarea.trigger('keydown', { key: 'Enter', ctrlKey: true })
    await textarea.trigger('keydown', { key: 'Enter', ctrlKey: true })
    expect(create).toHaveBeenCalledTimes(1)
    expect(create).toHaveBeenCalledWith('key-a', expect.objectContaining({ body: 'F1' }))
  })
  it('blocks Meta+Enter while a request is pending', async () => {
    const pending = new Promise<ReturnType<typeof messageFixture>>(() => {})
    const create = vi.spyOn(DefaultService, 'createMessage').mockReturnValue(pending as never)
    const wrapper = mountComposer()
    const textarea = wrapper.get('textarea')
    await textarea.setValue('F1')
    await textarea.trigger('keydown', { key: 'Enter', metaKey: true })
    await textarea.trigger('keydown', { key: 'Enter', metaKey: true })
    expect(create).toHaveBeenCalledTimes(1)
    expect(create).toHaveBeenCalledWith('key-a', expect.objectContaining({ body: 'F1' }))
  })
  it('clears an unchanged draft after its request succeeds', async () => {
    const create = vi.spyOn(DefaultService, 'createMessage').mockResolvedValue(messageFixture())
    const wrapper = mountComposer()
    await wrapper.get('textarea').setValue('F1')
    await wrapper.get('.toggle input').setValue(true)
    await wrapper.get('textarea').trigger('keydown', { key: 'Enter', ctrlKey: true })
    await flushPromises()
    expect(create).toHaveBeenCalledWith('key-a', expect.objectContaining({ body: 'F1', sensitive: true }))
    expect(wrapper.get<HTMLTextAreaElement>('textarea').element.value).toBe('')
    expect(wrapper.get<HTMLInputElement>('.toggle input').element.checked).toBe(false)
  })
  it('preserves a changed draft after the pending request succeeds and uses a new key for it', async () => {
    vi.mocked(DefaultService.listTags).mockResolvedValue([{
      id: 'tag-f2', name: 'F2 tag', color: '#3B8C6E',
      createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z',
    }])
    let resolveFirst!: (value: ReturnType<typeof messageFixture>) => void
    const pending = new Promise<ReturnType<typeof messageFixture>>((resolve) => { resolveFirst = resolve })
    const create = vi.spyOn(DefaultService, 'createMessage').mockReturnValueOnce(pending as never).mockResolvedValueOnce(messageFixture())
    const invalidate = vi.spyOn(QueryClient.prototype, 'invalidateQueries')
    const wrapper = mountComposer()
    await flushPromises()

    await wrapper.get('textarea').setValue('F1')
    await wrapper.get('textarea').trigger('keydown', { key: 'Enter', ctrlKey: true })
    expect(create).toHaveBeenNthCalledWith(1, 'key-a', expect.objectContaining({
      body: 'F1', bodyFormat: BodyFormat.TEXT, lifecycle: Lifecycle.TEMPORARY,
      sensitive: false, tagIds: [],
    }))

    await wrapper.get('textarea').setValue('F2')
    await wrapper.findAll('.modes button')[1].trigger('click')
    await wrapper.get('select').setValue(Lifecycle.PERMANENT)
    await wrapper.get('.toggle input').setValue(true)
    await wrapper.get('.tag-options input').setValue(true)
    await wrapper.get('textarea').trigger('keydown', { key: 'Enter', ctrlKey: true })
    expect(create).toHaveBeenCalledTimes(1)
    expect(create).not.toHaveBeenCalledWith('key-a', expect.objectContaining({ body: 'F2' }))
    resolveFirst(messageFixture())
    await flushPromises()

    expect(wrapper.get<HTMLTextAreaElement>('textarea').element.value).toBe('F2')
    expect(wrapper.findAll('.modes button')[1].classes()).toContain('active')
    expect(wrapper.get<HTMLSelectElement>('select').element.value).toBe(Lifecycle.PERMANENT)
    expect(wrapper.get<HTMLInputElement>('.toggle input').element.checked).toBe(true)
    expect(wrapper.get<HTMLInputElement>('.tag-options input').element.checked).toBe(true)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.messages.root() })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.search.root() })

    await wrapper.get('textarea').trigger('keydown', { key: 'Enter', ctrlKey: true })
    await flushPromises()
    expect(create).toHaveBeenNthCalledWith(2, 'key-b', expect.objectContaining({
      body: 'F2', bodyFormat: BodyFormat.MARKDOWN, lifecycle: Lifecycle.PERMANENT,
      sensitive: true, tagIds: ['tag-f2'],
    }))
    expect(create.mock.calls[1][0]).not.toBe(create.mock.calls[0][0])
  })
  it('includes the explicit sensitive selection', async () => {
    const create = vi.spyOn(DefaultService, 'createMessage').mockResolvedValue(messageFixture())
    const wrapper = mountComposer()
    await wrapper.get('textarea').setValue('secret')
    await wrapper.get('.toggle input').setValue(true)
    await wrapper.get('textarea').trigger('keydown', { key: 'Enter', ctrlKey: true })
    await flushPromises()
    expect(create).toHaveBeenCalledWith('key-a', expect.objectContaining({ sensitive: true }))
  })
})
