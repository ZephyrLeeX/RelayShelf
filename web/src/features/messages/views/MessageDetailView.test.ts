import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount, type VueWrapper } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { DefaultService, UploadStatus, type UploadSession } from '@/api/generated'
import { queryKeys } from '@/shared/api/queryKeys'
import { messageFixture } from '@/test/fixtures'
import { uploadManager } from '@/features/uploads/manager'
import { uploadState } from '@/features/uploads/store'
import type { UploadItem } from '@/features/uploads/types'
import MessageDetailView from './MessageDetailView.vue'

async function mountDetail(getMessage: (id: string) => ReturnType<typeof messageFixture>, id = 'message-1') {
  vi.spyOn(DefaultService, 'getMessage').mockImplementation((messageId) => Promise.resolve(getMessage(messageId)) as never)
  vi.spyOn(DefaultService, 'listTags').mockResolvedValue([])
  const client = new QueryClient({ defaultOptions: { queries: { retry:false }, mutations:{ retry:false } } })
  const router = createRouter({ history:createMemoryHistory(), routes:[{ path:'/messages/:id', component:{ template:'<div />' } }, { path:'/temporary', component:{ template:'<div />' } }] })
  await router.push(`/messages/${id}`); await router.isReady()
  const wrapper = mount(MessageDetailView, { props:{ id }, global:{ plugins:[router, [VueQueryPlugin, { queryClient:client }]], stubs:{ teleport:true } } })
  await flushPromises()
  return { wrapper, client }
}

describe('sensitive detail', () => {
  it('reveals only on explicit click and never puts plaintext in query cache', async () => {
    const reveal = vi.spyOn(DefaultService, 'revealSensitiveBody').mockResolvedValue({ body:'local-only-secret', version:1 })
    const { wrapper, client } = await mountDetail(() => messageFixture({ sensitive:true, body:null, bodyPreview:null }))
    expect(reveal).not.toHaveBeenCalled(); expect(wrapper.text()).not.toContain('local-only-secret')
    await wrapper.get('.sensitive .button.primary').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('local-only-secret')
    expect(client.getQueryCache().getAll().some((query) => JSON.stringify(query.state.data).includes('local-only-secret'))).toBe(false)
    wrapper.unmount()
  })
  it('discards a pending reveal after switching messages', async () => {
    let resolveA!: (value: { body:string; version:number }) => void
    vi.spyOn(DefaultService, 'revealSensitiveBody').mockImplementationOnce(() => new Promise((resolve) => { resolveA = resolve }) as never)
    const { wrapper } = await mountDetail((id) => messageFixture({ id, sensitive:true, body:null, bodyPreview:null }))
    await wrapper.get('.sensitive .button.primary').trigger('click')
    await wrapper.setProps({ id:'message-2' }); await flushPromises()
    resolveA({ body:'secret-from-a', version:1 }); await flushPromises()
    expect(wrapper.text()).not.toContain('secret-from-a')
    expect(wrapper.text()).toContain('Sensitive locked')
    wrapper.unmount()
  })
  it('clears v1 plaintext on a v2 refetch and requires a v2 reveal before editing', async () => {
    const reveal = vi.spyOn(DefaultService, 'revealSensitiveBody')
      .mockResolvedValueOnce({ body:'secret-v1', version:1 })
      .mockResolvedValueOnce({ body:'secret-v2', version:2 })
    const edit = vi.spyOn(DefaultService, 'editSensitiveBody').mockResolvedValue(messageFixture({ sensitive:true, body:null, version:3 }))
    const { wrapper, client } = await mountDetail(() => messageFixture({ sensitive:true, body:null, bodyPreview:null, version:1 }))
    await wrapper.get('.sensitive .button.primary').trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('secret-v1')
    await wrapper.findAll('.actions button')[1].trigger('click'); await flushPromises()
    expect(wrapper.find('form.edit').exists()).toBe(true)
    client.setQueryData(queryKeys.messages.detail('message-1'), messageFixture({ sensitive:true, body:null, bodyPreview:null, version:2 }))
    await wrapper.get('form.edit').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).not.toContain('secret-v1')
    expect(wrapper.find('form.edit').exists()).toBe(false)
    expect(edit).not.toHaveBeenCalled()
    await wrapper.findAll('.actions button')[1].trigger('click'); await flushPromises()
    expect(reveal).toHaveBeenCalledTimes(2)
    expect(wrapper.get('textarea').element.value).toBe('secret-v2')
    wrapper.unmount()
  })
  it('discards a reveal response whose version does not match the current message', async () => {
    vi.spyOn(DefaultService, 'revealSensitiveBody').mockResolvedValue({ body:'stale-secret', version:1 })
    const { wrapper } = await mountDetail(() => messageFixture({ sensitive:true, body:null, bodyPreview:null, version:2 }))
    await wrapper.get('.sensitive .button.primary').trigger('click'); await flushPromises()
    expect(wrapper.text()).not.toContain('stale-secret')
    expect(wrapper.text()).toContain('Sensitive locked')
    wrapper.unmount()
  })
})

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

describe('attachment reconciliation', () => {
  beforeEach(() => {
    uploadState.items = []
    vi.restoreAllMocks()
  })

  async function chooseFiles(wrapper: VueWrapper, name: string) {
    const input = wrapper.get<HTMLInputElement>('input[type="file"]')
    Object.defineProperty(input.element, 'files', { configurable: true, value: [new File(['data'], name)] })
    await input.trigger('change')
    await flushPromises()
  }

  it('retires only the consumed upload and keeps a newer selection added while the request was pending', async () => {
    uploadState.items.push(uploadItem('client-a', 'COMPLETED', uploadSession({ id: 'upload-a' })))
    let resolveAdd!: (value: ReturnType<typeof messageFixture>) => void
    const add = vi.spyOn(DefaultService, 'addMessageAttachments')
      .mockImplementationOnce(() => new Promise((resolve) => { resolveAdd = resolve }) as never)
      .mockResolvedValueOnce(messageFixture() as never)
    vi.spyOn(uploadManager, 'addFiles')
      .mockResolvedValueOnce(['client-a'])
      .mockResolvedValueOnce(['client-b'])
    const { wrapper } = await mountDetail(() => messageFixture())
    await chooseFiles(wrapper, 'photo.png')
    await wrapper.get('.add-files .button.primary').trigger('click')
    expect(add).toHaveBeenCalledWith('message-1', { expectedVersion: 1, uploadIds: ['upload-a'] })

    // While Add is pending, the user starts another upload in the same view.
    uploadState.items.push(uploadItem('client-b', 'UPLOADING', uploadSession({ id: 'upload-b', originalFilename: 'second.png', status: UploadStatus.UPLOADING, completedParts: [] })))
    await chooseFiles(wrapper, 'second.png')
    uploadState.items[1] = uploadItem('client-b', 'COMPLETED', uploadSession({ id: 'upload-b', originalFilename: 'second.png' }))
    await flushPromises()

    resolveAdd(messageFixture())
    await flushPromises()

    expect(uploadState.items.map((item) => item.serverUploadId)).toEqual(['upload-b'])
    expect(wrapper.text()).toContain('1 个文件')
    await wrapper.get('.add-files .button.primary').trigger('click')
    await flushPromises()
    expect(add).toHaveBeenLastCalledWith('message-1', { expectedVersion: 1, uploadIds: ['upload-b'] })
    wrapper.unmount()
  })

  it('offers a globally completed upload after reopening the detail without re-uploading', async () => {
    const add = vi.spyOn(DefaultService, 'addMessageAttachments').mockResolvedValue(messageFixture() as never)
    vi.spyOn(uploadManager, 'addFiles').mockResolvedValue(['client-b'])
    uploadState.items.push(uploadItem('client-b', 'UPLOADING', uploadSession({ id: 'upload-b', originalFilename: 'started.png', status: UploadStatus.UPLOADING, completedParts: [] })))
    const first = await mountDetail(() => messageFixture())
    await chooseFiles(first.wrapper, 'started.png')
    first.wrapper.unmount()

    // The upload finishes globally while the detail is closed.
    uploadState.items[0] = uploadItem('client-b', 'COMPLETED', uploadSession({ id: 'upload-b', originalFilename: 'started.png' }))
    const second = await mountDetail(() => messageFixture())
    expect(second.wrapper.text()).toContain('已完成的上传')
    expect(second.wrapper.text()).toContain('started.png')
    await second.wrapper.get('.restored li .button').trigger('click')
    await flushPromises()
    await second.wrapper.get('.add-files .button.primary').trigger('click')
    await flushPromises()
    expect(add).toHaveBeenCalledWith('message-1', { expectedVersion: 1, uploadIds: ['upload-b'] })
    expect(uploadState.items).toHaveLength(0)
    second.wrapper.unmount()
  })
})
