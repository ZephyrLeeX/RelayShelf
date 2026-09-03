import { VueQueryPlugin, QueryClient } from '@tanstack/vue-query'
import { mount, flushPromises, type VueWrapper } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { BodyFormat, DefaultService, Lifecycle, StorageRuntimeStatus, UploadStatus, type UploadSession } from '@/api/generated'
import { queryKeys } from '@/shared/api/queryKeys'
import { messageFixture } from '@/test/fixtures'
import MessageComposer from './MessageComposer.vue'
import { uploadManager } from '@/features/uploads/manager'
import { ResumeLedger } from '@/features/uploads/resumeLedger'
import { uploadState } from '@/features/uploads/store'
import type { UploadItem } from '@/features/uploads/types'
import { setStorageRuntimeStatus } from '@/features/storage/runtime'

const bob = { id: '11111111-1111-4111-8111-111111111111', username: 'bob', displayName: 'Bob' }
const carol = { id: '22222222-2222-4222-8222-222222222222', username: 'carol', displayName: 'Carol' }

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

async function pickContentType(wrapper: VueWrapper, label: string) {
  await wrapper.get('button[aria-label="内容类型"]').trigger('click')
  const option = wrapper.findAll('[role="option"]').find((candidate) => candidate.attributes('aria-label') === label)
  expect(option, `content type option ${label}`).toBeTruthy()
  await option!.trigger('click')
}

async function pickRecipient(wrapper: VueWrapper, username: string) {
  await wrapper.get('button[aria-label="接收人"]').trigger('click')
  await flushPromises()
  const option = wrapper.findAll('[role="option"]').find((candidate) => candidate.text().includes(`@${username}`))
  expect(option, `recipient option @${username}`).toBeTruthy()
  await option!.trigger('click')
}

async function pickSelf(wrapper: VueWrapper) {
  await wrapper.get('button[aria-label="接收人"]').trigger('click')
  const option = wrapper.findAll('[role="option"]').find((candidate) => candidate.text().includes('自己'))
  expect(option).toBeTruthy()
  await option!.trigger('click')
}

function sendByKeyboard(wrapper: VueWrapper) {
  return wrapper.get('textarea').trigger('keydown', { key: 'Enter', ctrlKey: true })
}

describe('MessageComposer', () => {
  beforeEach(() => {
    uploadState.items = []
    setStorageRuntimeStatus(undefined)
    vi.spyOn(DefaultService, 'listTags').mockResolvedValue([])
    vi.spyOn(DefaultService, 'listRecipientUsers').mockImplementation((query?: string) => Promise.resolve({
      items: [bob, carol].filter((user) => !query || user.username.includes(query) || user.displayName.toLowerCase().includes(query.toLowerCase())),
    }) as never)
    vi.stubGlobal('crypto', { randomUUID: vi.fn().mockReturnValueOnce('key-a').mockReturnValueOnce('key-b') })
  })
  it('disables only attachments while degraded and still sends text', async () => {
    uploadState.items.push(uploadItem('completed', 'COMPLETED', uploadSession({ id: 'upload-completed', status: UploadStatus.COMPLETED })))
    setStorageRuntimeStatus({ healthy: false, reason: StorageRuntimeStatus.reason.NAS_TIMEOUT, lastCheckedAt: null, changedAt: '2026-09-03T00:00:00Z' })
    const create = vi.spyOn(DefaultService, 'createMessage').mockResolvedValue(messageFixture())
    const wrapper = mountComposer()
    expect(wrapper.get('button[aria-label="附件"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('.restored-menu').exists()).toBe(false)
    await wrapper.get('textarea').setValue('text still works')
    await sendByKeyboard(wrapper)
    await flushPromises()
    expect(create).toHaveBeenCalledWith('key-a', expect.objectContaining({ body: 'text still works', uploadIds: [] }))
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
  it('exposes no UUID input and no More menu anywhere in the composer', () => {
    const wrapper = mountComposer()
    expect(wrapper.html()).not.toContain('UUID')
    expect(wrapper.html()).not.toContain('接收者用户 ID')
    expect(wrapper.find('summary[aria-label="高级选项"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('Ctrl/⌘')
    expect(wrapper.text()).toContain('自己')
  })
  it('serializes content types onto the TEXT/MARKDOWN contract', async () => {
    let sequence = 0
    vi.stubGlobal('crypto', { randomUUID: () => `key-${sequence++}` })
    const create = vi.spyOn(DefaultService, 'createMessage').mockResolvedValue(messageFixture())
    const wrapper = mountComposer(Lifecycle.PERMANENT)
    expect(wrapper.text()).toContain('长期内容尚未添加标签')

    await wrapper.get('textarea').setValue('# heading')
    await pickContentType(wrapper, 'Markdown')
    await sendByKeyboard(wrapper)
    await flushPromises()
    expect(create).toHaveBeenLastCalledWith(`key-${sequence - 1}`, expect.objectContaining({ body: '# heading', bodyFormat: BodyFormat.MARKDOWN, lifecycle: Lifecycle.PERMANENT }))

    await wrapper.get('textarea').setValue('docker compose ps')
    await pickContentType(wrapper, 'Shell')
    expect(wrapper.get('button[aria-label="内容类型"]').text()).toContain('Shell')
    await sendByKeyboard(wrapper)
    await flushPromises()
    expect(create).toHaveBeenLastCalledWith(`key-${sequence - 1}`, expect.objectContaining({ body: '```bash\ndocker compose ps\n```', bodyFormat: BodyFormat.MARKDOWN }))

    await wrapper.get('textarea').setValue('print("x")')
    await pickContentType(wrapper, 'Python')
    await sendByKeyboard(wrapper)
    await flushPromises()
    expect(create).toHaveBeenLastCalledWith(`key-${sequence - 1}`, expect.objectContaining({ body: '```python\nprint("x")\n```', bodyFormat: BodyFormat.MARKDOWN }))

    await wrapper.get('textarea').setValue('class A {}')
    await pickContentType(wrapper, 'Java')
    // The textarea itself never shows the fence.
    expect(wrapper.get<HTMLTextAreaElement>('textarea').element.value).toBe('class A {}')
    await sendByKeyboard(wrapper)
    await flushPromises()
    expect(create).toHaveBeenLastCalledWith(`key-${sequence - 1}`, expect.objectContaining({ body: '```java\nclass A {}\n```', bodyFormat: BodyFormat.MARKDOWN }))
  })
  it('uses the direct-send contract for a picked recipient on Ctrl+Enter', async () => {
    const create = vi.spyOn(DefaultService, 'createMessage').mockResolvedValue(messageFixture())
    const direct = vi.spyOn(DefaultService, 'directSendMessage').mockResolvedValue({
      messageId: 'message-direct', createdAt: '2026-08-31T00:00:00Z', expiresAt: '2026-09-01T00:00:00Z',
    })
    const wrapper = mountComposer(Lifecycle.PERMANENT)
    await pickRecipient(wrapper, 'bob')
    expect(wrapper.get('button[aria-label="接收人"]').text()).toContain('@bob')
    await wrapper.get('textarea').setValue('send once')
    await sendByKeyboard(wrapper)
    await flushPromises()
    expect(create).not.toHaveBeenCalled()
    expect(direct).toHaveBeenCalledWith('key-a', {
      recipientUserId: bob.id,
      body: 'send once', bodyFormat: BodyFormat.TEXT, sensitive: false, uploadIds: [],
    })
    expect(wrapper.get<HTMLTextAreaElement>('textarea').element.value).toBe('')
    // A successful direct send falls back to sending to myself.
    expect(wrapper.get('button[aria-label="接收人"]').text()).toContain('自己')
  })
  it('lists recipient users through the shared picker endpoint', async () => {
    const wrapper = mountComposer()
    await pickRecipient(wrapper, 'carol')
    expect(wrapper.get('button[aria-label="接收人"]').text()).toContain('@carol')

    await wrapper.get('button[aria-label="接收人"]').trigger('click')
    await flushPromises()
    expect(DefaultService.listRecipientUsers).toHaveBeenCalledWith(undefined, 8)
  })
  it('restores send-to-myself and a normal createMessage payload', async () => {
    const create = vi.spyOn(DefaultService, 'createMessage').mockResolvedValue(messageFixture())
    const direct = vi.spyOn(DefaultService, 'directSendMessage')
    const wrapper = mountComposer()
    await pickRecipient(wrapper, 'bob')
    await pickSelf(wrapper)
    expect(wrapper.get('button[aria-label="接收人"]').text()).toContain('自己')
    await wrapper.get('textarea').setValue('normal again')
    await sendByKeyboard(wrapper)
    await flushPromises()
    expect(direct).not.toHaveBeenCalled()
    expect(create).toHaveBeenCalledWith('key-a', expect.objectContaining({ body: 'normal again', bodyFormat: BodyFormat.TEXT, lifecycle: Lifecycle.TEMPORARY }))
  })
  it('truly disables tag editing while a direct recipient is selected and keeps the tag draft', async () => {
    vi.mocked(DefaultService.listTags).mockResolvedValue([{
      id: 'tag-ops', name: 'Ops', color: '#3B8C6E',
      createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z',
    }])
    const createTag = vi.spyOn(DefaultService, 'createTag')
    const direct = vi.spyOn(DefaultService, 'directSendMessage').mockResolvedValue({
      messageId: 'message-direct', createdAt: '2026-08-31T00:00:00Z', expiresAt: '2026-09-01T00:00:00Z',
    })
    const wrapper = mountComposer()
    await flushPromises()
    expect(wrapper.get('.lifecycle-control select').attributes('disabled')).toBeUndefined()
    // Draft a tag selection while still sending to myself.
    await wrapper.get<HTMLInputElement>('.tag-options input').setValue(true)

    await pickRecipient(wrapper, 'bob')
    expect(wrapper.get('.lifecycle-control select').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('直发为独立临时副本')
    // The interactive picker is gone: a truly disabled button replaces it, so
    // pointer, keyboard, and programmatic invocation cannot open tag editing
    // and no inner checkboxes or create-tag controls exist.
    const control = wrapper.get('button[aria-label="标签"]')
    expect(control.attributes('disabled')).toBeDefined()
    expect(wrapper.find('details.tag-picker').exists()).toBe(false)
    expect(wrapper.find('.tag-options').exists()).toBe(false)
    expect(wrapper.find('input[aria-label="新建标签名称"]').exists()).toBe(false)
    // The disabled control still shows the kept draft count.
    expect(control.text()).toContain('1')

    // Switching back to myself restores the interactive picker with the
    // untouched tag draft — direct mode disables the UI but never wipes it.
    await pickSelf(wrapper)
    expect(wrapper.get('.lifecycle-control select').attributes('disabled')).toBeUndefined()
    expect(wrapper.find('details.tag-picker').exists()).toBe(true)
    expect(wrapper.get<HTMLInputElement>('.tag-options input').element.checked).toBe(true)

    // A direct send still never carries tagIds, even with a drafted tag.
    await pickRecipient(wrapper, 'bob')
    await wrapper.get('textarea').setValue('direct with drafted tag')
    await sendByKeyboard(wrapper)
    await flushPromises()
    expect(createTag).not.toHaveBeenCalled()
    expect(direct).toHaveBeenCalledWith('key-a', {
      recipientUserId: bob.id,
      body: 'direct with drafted tag', bodyFormat: BodyFormat.TEXT, sensitive: false, uploadIds: [],
    })
  })
  it('reuses the direct-send idempotency key after a network failure and rotates it when the recipient changes', async () => {
    const direct = vi.spyOn(DefaultService, 'directSendMessage')
      .mockRejectedValueOnce(new TypeError('offline'))
      .mockRejectedValueOnce(new TypeError('still offline'))
      .mockResolvedValueOnce({ messageId: 'message-b', createdAt: '2026-08-31T00:00:00Z', expiresAt: '2026-09-01T00:00:00Z' })
    const wrapper = mountComposer()
    await pickRecipient(wrapper, 'bob')
    await wrapper.get('textarea').setValue('uncertain direct send')
    await wrapper.get('button.primary').trigger('click')
    await flushPromises()
    expect(direct.mock.calls[0][0]).toBe('key-a')

    await wrapper.get('button.primary').trigger('click')
    await flushPromises()
    expect(direct.mock.calls[1][0]).toBe('key-a')

    await wrapper.get('textarea').setValue('second direct send')
    await pickRecipient(wrapper, 'carol')
    await wrapper.get('button.primary').trigger('click')
    await flushPromises()
    expect(direct.mock.calls[2][0]).toBe('key-b')
  })
  it('preserves a new recipient draft when the pending direct send succeeds', async () => {
    let resolveFirst!: (value: { messageId: string; createdAt: string; expiresAt: string }) => void
    const pending = new Promise<{ messageId: string; createdAt: string; expiresAt: string }>((resolve) => { resolveFirst = resolve })
    const direct = vi.spyOn(DefaultService, 'directSendMessage').mockReturnValueOnce(pending as never)
    const wrapper = mountComposer()
    await pickRecipient(wrapper, 'bob')
    await wrapper.get('textarea').setValue('keep this draft')
    await wrapper.get('button.primary').trigger('click')
    expect(direct).toHaveBeenCalledWith('key-a', expect.objectContaining({
      recipientUserId: bob.id,
    }))

    await pickRecipient(wrapper, 'carol')
    resolveFirst({ messageId: 'message-a', createdAt: '2026-08-31T00:00:00Z', expiresAt: '2026-09-01T00:00:00Z' })
    await flushPromises()

    expect(wrapper.get('button[aria-label="接收人"]').text()).toContain('@carol')
    expect(wrapper.get<HTMLTextAreaElement>('textarea').element.value).toBe('keep this draft')
  })
  it('blocks normal and direct sends from running concurrently in either direction', async () => {
    const pendingMessage = new Promise<ReturnType<typeof messageFixture>>(() => {})
    const pendingDirect = new Promise<{ messageId: string; createdAt: string; expiresAt: string }>(() => {})
    const create = vi.spyOn(DefaultService, 'createMessage').mockReturnValue(pendingMessage as never)
    const direct = vi.spyOn(DefaultService, 'directSendMessage').mockReturnValue(pendingDirect as never)

    const normalFirst = mountComposer()
    await normalFirst.get('textarea').setValue('normal first')
    await normalFirst.get('button.primary').trigger('click')
    await pickRecipient(normalFirst, 'bob')
    expect(normalFirst.get('button.primary').attributes('disabled')).toBeDefined()
    await sendByKeyboard(normalFirst)
    expect(direct).not.toHaveBeenCalled()

    const directFirst = mountComposer()
    await directFirst.get('textarea').setValue('direct first')
    await pickRecipient(directFirst, 'bob')
    await directFirst.get('button.primary').trigger('click')
    await pickSelf(directFirst)
    expect(directFirst.get('button.primary').attributes('disabled')).toBeDefined()
    await sendByKeyboard(directFirst)
    expect(create).toHaveBeenCalledTimes(1)
  })
  it('selects pasted images through UploadManager without inserting clipboard text', async () => {
    uploadState.items.push(uploadItem('client-paste', 'COMPLETED', uploadSession({ id: 'upload-paste', originalFilename: 'pasted.png' })))
    const addFiles = vi.spyOn(uploadManager, 'addFiles').mockResolvedValue(['client-paste'])
    const wrapper = mountComposer()
    const pasted = new File(['image'], 'pasted.png', { type: 'image/png' })
    const preventDefault = vi.fn()
    wrapper.get('textarea').element.dispatchEvent(Object.assign(new Event('paste', { bubbles: true }), {
      clipboardData: { items: [{ kind: 'file', type: 'image/png', getAsFile: () => pasted }] },
      preventDefault,
    }))
    await flushPromises()
    expect(addFiles).toHaveBeenCalledWith([pasted])
    expect(wrapper.text()).toContain('pasted.png')
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
  it('uses PERMANENT for a new send after navigating from temporary to permanent', async () => {
    const create = vi.spyOn(DefaultService, 'createMessage').mockResolvedValue(messageFixture())
    const wrapper = mountComposer(Lifecycle.TEMPORARY)

    await wrapper.setProps({ defaultLifecycle: Lifecycle.PERMANENT })
    await wrapper.get('textarea').setValue('keep permanently')
    await wrapper.get('button.primary').trigger('click')
    await flushPromises()

    expect(create).toHaveBeenCalledWith('key-a', expect.objectContaining({
      body: 'keep permanently', lifecycle: Lifecycle.PERMANENT,
    }))
  })
  it('uses TEMPORARY for a new send after navigating from permanent to temporary', async () => {
    const create = vi.spyOn(DefaultService, 'createMessage').mockResolvedValue(messageFixture())
    const wrapper = mountComposer(Lifecycle.PERMANENT)

    await wrapper.setProps({ defaultLifecycle: Lifecycle.TEMPORARY })
    await wrapper.get('textarea').setValue('expire later')
    await wrapper.get('button.primary').trigger('click')
    await flushPromises()

    expect(create).toHaveBeenCalledWith('key-a', expect.objectContaining({
      body: 'expire later', lifecycle: Lifecycle.TEMPORARY,
    }))
  })
  it('preserves an explicitly selected lifecycle when the route default changes', async () => {
    const create = vi.spyOn(DefaultService, 'createMessage').mockResolvedValue(messageFixture())
    const wrapper = mountComposer(Lifecycle.TEMPORARY)
    await wrapper.get('select').setValue(Lifecycle.PERMANENT)

    await wrapper.setProps({ defaultLifecycle: Lifecycle.PERMANENT })
    await wrapper.setProps({ defaultLifecycle: Lifecycle.TEMPORARY })
    await wrapper.get('textarea').setValue('edited draft')
    await wrapper.get('button.primary').trigger('click')
    await flushPromises()

    expect(create).toHaveBeenCalledWith('key-a', expect.objectContaining({
      body: 'edited draft', lifecycle: Lifecycle.PERMANENT,
    }))
  })
  it('reuses one idempotency key for retry but resets it after draft changes', async () => {
    const create = vi.spyOn(DefaultService, 'createMessage').mockRejectedValueOnce(new TypeError('offline')).mockResolvedValueOnce(messageFixture()).mockResolvedValueOnce(messageFixture())
    const wrapper = mountComposer()
    await wrapper.get('textarea').setValue('uncertain send')
    await sendByKeyboard(wrapper)
    await flushPromises()
    expect(create.mock.calls[0][0]).toBe('key-a')
    await wrapper.get('button.primary').trigger('click')
    await flushPromises()
    expect(create.mock.calls[1][0]).toBe('key-a')
    await wrapper.get('textarea').setValue('changed payload')
    await sendByKeyboard(wrapper)
    await flushPromises()
    expect(create.mock.calls[2][0]).toBe('key-b')
  })
  it('binds a pending request to its payload snapshot and gives a changed draft a new key', async () => {
    let rejectFirst!: (reason: unknown) => void
    const pending = new Promise<never>((_resolve, reject) => { rejectFirst = reject })
    const create = vi.spyOn(DefaultService, 'createMessage').mockReturnValueOnce(pending as never).mockResolvedValueOnce(messageFixture())
    const wrapper = mountComposer()
    await wrapper.get('textarea').setValue('F1')
    await sendByKeyboard(wrapper)
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
    await wrapper.get('button[aria-label="敏感内容"]').trigger('click')
    expect(wrapper.get('button[aria-label="敏感内容"]').attributes('aria-pressed')).toBe('true')
    await sendByKeyboard(wrapper)
    await flushPromises()
    expect(create).toHaveBeenCalledWith('key-a', expect.objectContaining({ body: 'F1', sensitive: true }))
    expect(wrapper.get<HTMLTextAreaElement>('textarea').element.value).toBe('')
    expect(wrapper.get('button[aria-label="敏感内容"]').attributes('aria-pressed')).toBe('false')
  })
  it('preserves a changed draft after the pending request succeeds and uses a new key for it', async () => {
    vi.mocked(DefaultService.listTags).mockResolvedValue([{
      id: 'tag-f2', name: 'F2 tag', color: '#3B8C6E',
      createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z',
    }])
    let sequence = 0
    vi.stubGlobal('crypto', { randomUUID: () => `key-${sequence++}` })
    let resolveFirst!: (value: ReturnType<typeof messageFixture>) => void
    const pending = new Promise<ReturnType<typeof messageFixture>>((resolve) => { resolveFirst = resolve })
    const create = vi.spyOn(DefaultService, 'createMessage').mockReturnValueOnce(pending as never).mockResolvedValueOnce(messageFixture())
    const invalidate = vi.spyOn(QueryClient.prototype, 'invalidateQueries')
    const wrapper = mountComposer()
    await flushPromises()

    await wrapper.get('textarea').setValue('F1')
    await sendByKeyboard(wrapper)
    expect(create).toHaveBeenNthCalledWith(1, 'key-0', expect.objectContaining({
      body: 'F1', bodyFormat: BodyFormat.TEXT, lifecycle: Lifecycle.TEMPORARY,
      sensitive: false, tagIds: [],
    }))

    await wrapper.get('textarea').setValue('F2')
    await pickContentType(wrapper, 'Markdown')
    await wrapper.get('select').setValue(Lifecycle.PERMANENT)
    await wrapper.get('button[aria-label="敏感内容"]').trigger('click')
    await wrapper.get('.tag-options input').setValue(true)
    await sendByKeyboard(wrapper)
    expect(create).toHaveBeenCalledTimes(1)
    expect(create).not.toHaveBeenCalledWith('key-0', expect.objectContaining({ body: 'F2' }))
    resolveFirst(messageFixture())
    await flushPromises()

    expect(wrapper.get<HTMLTextAreaElement>('textarea').element.value).toBe('F2')
    expect(wrapper.get('button[aria-label="内容类型"]').text()).toContain('Markdown')
    expect(wrapper.get<HTMLSelectElement>('select').element.value).toBe(Lifecycle.PERMANENT)
    expect(wrapper.get('button[aria-label="敏感内容"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.get<HTMLInputElement>('.tag-options input').element.checked).toBe(true)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.messages.root() })
    expect(invalidate).toHaveBeenCalledWith({ queryKey: queryKeys.search.root() })

    await sendByKeyboard(wrapper)
    await flushPromises()
    expect(create).toHaveBeenNthCalledWith(2, 'key-1', expect.objectContaining({
      body: 'F2', bodyFormat: BodyFormat.MARKDOWN, lifecycle: Lifecycle.PERMANENT,
      sensitive: true, tagIds: ['tag-f2'],
    }))
    expect(create.mock.calls[1][0]).not.toBe(create.mock.calls[0][0])
  })
  it('includes the explicit sensitive selection', async () => {
    const create = vi.spyOn(DefaultService, 'createMessage').mockResolvedValue(messageFixture())
    const wrapper = mountComposer()
    await wrapper.get('textarea').setValue('secret')
    await wrapper.get('button[aria-label="敏感内容"]').trigger('click')
    await sendByKeyboard(wrapper)
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
    await wrapper.get('.restored-menu li button').trigger('click')
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
    await wrapper.get('.restored-menu li button').trigger('click')
    await wrapper.get('button.primary').trigger('click')
    await flushPromises()
    expect(uploadState.items).toHaveLength(1)
    expect(new ResumeLedger().read('user-1')).toHaveLength(1)
    expect(wrapper.find('.restored-menu li').exists()).toBe(false)
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
    await sendByKeyboard(wrapper)
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
    await sendByKeyboard(wrapper)
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
    await sendByKeyboard(wrapper)
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
    await sendByKeyboard(wrapper)
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
    await sendByKeyboard(wrapper)
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
