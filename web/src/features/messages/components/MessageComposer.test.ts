import { VueQueryPlugin, QueryClient } from '@tanstack/vue-query'
import { mount, flushPromises } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { BodyFormat, DefaultService, Lifecycle, UploadStatus, type UploadSession } from '@/api/generated'
import { queryKeys } from '@/shared/api/queryKeys'
import { messageFixture } from '@/test/fixtures'
import MessageComposer from './MessageComposer.vue'
import { uploadManager } from '@/features/uploads/manager'
import { ResumeLedger } from '@/features/uploads/resumeLedger'
import { uploadState } from '@/features/uploads/store'
import type { UploadItem } from '@/features/uploads/types'

function uploadSession(overrides: Partial<UploadSession> = {}): UploadSession {
  return { id: 'upload-a', originalFilename: 'photo.png', expectedSize: 4, clientMime: 'image/png', chunkSize: 4, partCount: 1, status: UploadStatus.COMPLETED, expiresAt: '2026-09-01T00:00:00Z', completedParts: [0], createdAt: '2026-08-28T00:00:00Z', updatedAt: '2026-08-28T00:00:00Z', ...overrides }
}
function uploadItem(clientId: string, status: UploadItem['status'], session = uploadSession()): UploadItem {
  const completed = status === 'COMPLETED'
  return {
    clientId, serverUploadId: session.id, session, filename: session.originalFilename, size: session.expectedSize,
    lastModified: 1, completedParts: [...session.completedParts], activeParts: [], transferredByPart: {},
    sentBytes: completed ? session.expectedSize : 0, progress: completed ? 1 : 0,
    createdAt: '2026-08-28T00:00:00Z', selected: true, status,
    ...(status === 'FAILED' ? { error: '上传失败。', errorCode: 'NETWORK_ERROR', retryable: true } : {}),
  } as UploadItem
}

function mountComposer(lifecycle = Lifecycle.TEMPORARY) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  return mount(MessageComposer, { props: { defaultLifecycle: lifecycle }, global: { plugins: [createPinia(), [VueQueryPlugin, { queryClient: client }]] } })
}

describe('MessageComposer', () => {
  beforeEach(() => {
    uploadState.items = []
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
  it('allows a file-only message only after its selected upload is completed', async () => {
    const completed = {
      clientId: 'client-upload', serverUploadId: 'upload-1', filename: 'photo.png', size: 4, lastModified: 1,
      completedParts: [0], activeParts: [], transferredByPart: {}, sentBytes: 4, progress: 1, createdAt: '2026-01-01T00:00:00Z', selected: true,
      status: 'COMPLETED' as const,
      session: { id: 'upload-1', originalFilename: 'photo.png', expectedSize: 4, clientMime: 'image/png', chunkSize: 4, partCount: 1, status: UploadStatus.COMPLETED, expiresAt: '2026-01-02T00:00:00Z', completedParts: [0], createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z' },
    }
    uploadState.items.push(completed)
    vi.spyOn(uploadManager, 'addFiles').mockResolvedValue(['client-upload'])
    const create = vi.spyOn(DefaultService, 'createMessage').mockResolvedValue(messageFixture())
    const wrapper = mountComposer()
    const input = wrapper.get<HTMLInputElement>('input[type="file"]')
    Object.defineProperty(input.element, 'files', { configurable: true, value: [new File(['data'], 'photo.png')] })
    await input.trigger('change')
    await flushPromises()
    expect(wrapper.get('button.primary').attributes('disabled')).toBeUndefined()
    await wrapper.get('button.primary').trigger('click')
    await flushPromises()
    expect(create).toHaveBeenCalledWith('key-a', expect.objectContaining({ body: null, uploadIds: ['upload-1'] }))
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

  it('blocks Send while a selected attachment is uploading or failed', async () => {
    uploadState.items.push(uploadItem('client-uploading', 'UPLOADING', uploadSession({ status: UploadStatus.UPLOADING, id: 'upload-up' })))
    vi.spyOn(uploadManager, 'addFiles').mockResolvedValue(['client-uploading'])
    const create = vi.spyOn(DefaultService, 'createMessage').mockResolvedValue(messageFixture())
    const wrapper = mountComposer()
    const input = wrapper.get<HTMLInputElement>('input[type="file"]')
    Object.defineProperty(input.element, 'files', { configurable: true, value: [new File(['data'], 'photo.png')] })
    await input.trigger('change')
    await flushPromises()
    expect(wrapper.get('button.primary').attributes('disabled')).toBeDefined()
    const uploading = uploadState.items[0]
    uploadState.items[0] = { ...uploading, status: 'FAILED', error: 'network', errorCode: 'NETWORK_ERROR', retryable: true } as UploadItem
    await flushPromises()
    expect(wrapper.get('button.primary').attributes('disabled')).toBeDefined()
    expect(create).not.toHaveBeenCalled()
  })

  it('sends uploadIds in selection order', async () => {
    uploadState.items.push(uploadItem('client-a', 'COMPLETED', uploadSession({ id: 'upload-a' })), uploadItem('client-b', 'COMPLETED', uploadSession({ id: 'upload-b', originalFilename: 'second.png' })))
    vi.spyOn(uploadManager, 'addFiles').mockResolvedValue(['client-b', 'client-a'])
    const create = vi.spyOn(DefaultService, 'createMessage').mockResolvedValue(messageFixture())
    const wrapper = mountComposer()
    const input = wrapper.get<HTMLInputElement>('input[type="file"]')
    Object.defineProperty(input.element, 'files', { configurable: true, value: [new File(['data'], 'second.png'), new File(['data'], 'photo.png')] })
    await input.trigger('change')
    await flushPromises()
    await wrapper.get('button.primary').trigger('click')
    await flushPromises()
    expect(create).toHaveBeenCalledWith('key-a', expect.objectContaining({ uploadIds: ['upload-b', 'upload-a'] }))
  })

  it('restores a COMPLETED upload after reload, lets the draft consume it, and retires it only on success', async () => {
    // reconcile consumes one randomUUID for the restored clientId, so the key
    // sequence is restubbed instead of relying on the beforeEach chain.
    vi.stubGlobal('crypto', { randomUUID: () => 'fixed-key' })
    new ResumeLedger().upsert('user-1', { uploadId: 'upload-done', lastModified: 1, createdAt: '2026-08-28T00:00:00Z' })
    vi.spyOn(DefaultService, 'getUpload').mockResolvedValue(uploadSession({ id: 'upload-done', status: UploadStatus.COMPLETED }))
    await uploadManager.reconcile('user-1')
    expect(uploadState.items[0].status).toBe('COMPLETED')
    const create = vi.spyOn(DefaultService, 'createMessage').mockResolvedValue(messageFixture())
    const wrapper = mountComposer()
    await flushPromises()
    expect(wrapper.text()).toContain('已完成的上传')
    await wrapper.get('.restored li button').trigger('click')
    await wrapper.get('button.primary').trigger('click')
    await flushPromises()
    expect(create).toHaveBeenCalledWith('fixed-key', expect.objectContaining({ body: null, uploadIds: ['upload-done'] }))
    expect(uploadState.items).toHaveLength(0)
    expect(new ResumeLedger().read('user-1')).toEqual([])
  })

  it('keeps a restored COMPLETED upload available when CreateMessage fails', async () => {
    vi.stubGlobal('crypto', { randomUUID: () => 'fixed-key' })
    new ResumeLedger().upsert('user-1', { uploadId: 'upload-done', lastModified: 1, createdAt: '2026-08-28T00:00:00Z' })
    vi.spyOn(DefaultService, 'getUpload').mockResolvedValue(uploadSession({ id: 'upload-done', status: UploadStatus.COMPLETED }))
    await uploadManager.reconcile('user-1')
    vi.spyOn(DefaultService, 'createMessage').mockRejectedValue(new TypeError('offline'))
    const wrapper = mountComposer()
    await flushPromises()
    await wrapper.get('.restored li button').trigger('click')
    await wrapper.get('button.primary').trigger('click')
    await flushPromises()
    expect(uploadState.items).toHaveLength(1)
    expect(new ResumeLedger().read('user-1')).toHaveLength(1)
    expect(wrapper.find('.restored li').exists()).toBe(false)
    expect(wrapper.text()).toContain('photo.png')
  })

  it('clears an unchanged draft with a completed attachment normally (F1 + A)', async () => {
    uploadState.items.push(uploadItem('client-a', 'COMPLETED', uploadSession({ id: 'upload-a' })))
    vi.spyOn(uploadManager, 'addFiles').mockResolvedValue(['client-a'])
    const create = vi.spyOn(DefaultService, 'createMessage').mockResolvedValue(messageFixture())
    const wrapper = mountComposer()
    const input = wrapper.get<HTMLInputElement>('input[type="file"]')
    Object.defineProperty(input.element, 'files', { configurable: true, value: [new File(['data'], 'photo.png')] })
    await input.trigger('change')
    await wrapper.get('textarea').setValue('F1')
    await wrapper.get('textarea').trigger('keydown', { key: 'Enter', ctrlKey: true })
    await flushPromises()
    expect(create).toHaveBeenCalledWith('key-a', expect.objectContaining({ body: 'F1', uploadIds: ['upload-a'] }))
    expect(wrapper.get<HTMLTextAreaElement>('textarea').element.value).toBe('')
    expect(wrapper.find('.selected-files li').exists()).toBe(false)
    expect(uploadState.items).toHaveLength(0)
  })

  it('preserves a newer pending attachment when the submitted request succeeds (F1 + A pending, B uploading)', async () => {
    uploadState.items.push(uploadItem('client-a', 'COMPLETED', uploadSession({ id: 'upload-a' })))
    uploadState.items.push(uploadItem('client-b', 'UPLOADING', uploadSession({ id: 'upload-b', originalFilename: 'second.png', status: UploadStatus.UPLOADING, completedParts: [] })))
    vi.spyOn(uploadManager, 'addFiles')
      .mockResolvedValueOnce(['client-a'])
      .mockResolvedValueOnce(['client-b'])
    let resolveFirst!: (value: ReturnType<typeof messageFixture>) => void
    const pending = new Promise<ReturnType<typeof messageFixture>>((resolve) => { resolveFirst = resolve })
    const create = vi.spyOn(DefaultService, 'createMessage').mockReturnValueOnce(pending as never).mockResolvedValueOnce(messageFixture())
    const wrapper = mountComposer()
    const input = wrapper.get<HTMLInputElement>('input[type="file"]')
    Object.defineProperty(input.element, 'files', { configurable: true, value: [new File(['data'], 'photo.png')] })
    await input.trigger('change')
    await wrapper.get('textarea').setValue('F1')
    await wrapper.get('textarea').trigger('keydown', { key: 'Enter', ctrlKey: true })
    expect(create).toHaveBeenNthCalledWith(1, 'key-a', expect.objectContaining({ body: 'F1', uploadIds: ['upload-a'] }))

    const second = wrapper.get<HTMLInputElement>('input[type="file"]')
    Object.defineProperty(second.element, 'files', { configurable: true, value: [new File(['data'], 'second.png')] })
    await second.trigger('change')
    await wrapper.get('textarea').setValue('F2')
    resolveFirst(messageFixture())
    await flushPromises()

    expect(uploadState.items.map((item) => item.serverUploadId)).toEqual(['upload-b'])
    expect(wrapper.get<HTMLTextAreaElement>('textarea').element.value).toBe('F2')
    expect(wrapper.text()).toContain('second.png')

    uploadState.items[0] = uploadItem('client-b', 'COMPLETED', uploadSession({ id: 'upload-b', originalFilename: 'second.png' }))
    await flushPromises()
    await wrapper.get('textarea').trigger('keydown', { key: 'Enter', ctrlKey: true })
    await flushPromises()
    expect(create).toHaveBeenNthCalledWith(2, 'key-b', expect.objectContaining({ body: 'F2', uploadIds: ['upload-b'] }))
  })

  it('resets the idempotency key when a completed upload is removed from the global queue after a failure', async () => {
    uploadState.items.push(uploadItem('client-a', 'COMPLETED', uploadSession({ id: 'upload-a' })))
    vi.spyOn(uploadManager, 'addFiles').mockResolvedValue(['client-a'])
    const create = vi.spyOn(DefaultService, 'createMessage').mockRejectedValueOnce(new TypeError('offline')).mockResolvedValueOnce(messageFixture())
    const wrapper = mountComposer()
    const input = wrapper.get<HTMLInputElement>('input[type="file"]')
    Object.defineProperty(input.element, 'files', { configurable: true, value: [new File(['data'], 'photo.png')] })
    await input.trigger('change')
    await wrapper.get('textarea').setValue('F1')
    await wrapper.get('textarea').trigger('keydown', { key: 'Enter', ctrlKey: true })
    await flushPromises()
    expect(create).toHaveBeenNthCalledWith(1, 'key-a', expect.objectContaining({ body: 'F1', uploadIds: ['upload-a'] }))
    // Removing A through the GLOBAL Upload Queue keeps the Composer selection
    // client ID, but the request payload no longer contains upload-a — so the
    // failed key must not be replayed against a changed payload.
    uploadManager.remove('client-a')
    await flushPromises()
    await wrapper.get('button.primary').trigger('click')
    await flushPromises()
    expect(create).toHaveBeenNthCalledWith(2, 'key-b', expect.objectContaining({ body: 'F1', uploadIds: [] }))
    expect(create.mock.calls[1][0]).not.toBe(create.mock.calls[0][0])
  })

  it('resets the idempotency key when a pending attachment selection changes after a failure', async () => {
    uploadState.items.push(uploadItem('client-b', 'UPLOADING', uploadSession({ id: 'upload-b', originalFilename: 'second.png', status: UploadStatus.UPLOADING, completedParts: [] })))
    vi.spyOn(uploadManager, 'addFiles').mockResolvedValue(['client-b'])
    const create = vi.spyOn(DefaultService, 'createMessage').mockRejectedValueOnce(new TypeError('offline')).mockResolvedValueOnce(messageFixture())
    const wrapper = mountComposer()
    await wrapper.get('textarea').setValue('F1')
    await wrapper.get('textarea').trigger('keydown', { key: 'Enter', ctrlKey: true })
    await flushPromises()
    expect(create.mock.calls[0][0]).toBe('key-a')
    const input = wrapper.get<HTMLInputElement>('input[type="file"]')
    Object.defineProperty(input.element, 'files', { configurable: true, value: [new File(['data'], 'second.png')] })
    await input.trigger('change')
    await flushPromises()
    // Deselecting the still-uploading file unblocks the retry; the selection
    // round-trip already proved the draft identity changed.
    await wrapper.get('.selected-files li .button').trigger('click')
    await flushPromises()
    expect(uploadState.items).toHaveLength(0)
    await wrapper.get('button.primary').trigger('click')
    await flushPromises()
    expect(create.mock.calls[1][0]).toBe('key-b')
    expect(create).toHaveBeenNthCalledWith(2, 'key-b', expect.objectContaining({ body: 'F1', uploadIds: [] }))
  })
})
