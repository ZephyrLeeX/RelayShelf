import { VueQueryPlugin, QueryClient } from '@tanstack/vue-query'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { BodyFormat, DefaultService, Lifecycle } from '@/api/generated'
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
