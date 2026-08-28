import { mount } from '@vue/test-utils'
import { ref } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { uploadState } from '@/features/uploads/store'
import type { UploadItem } from '@/features/uploads/types'
import PWAUpdatePrompt from './PWAUpdatePrompt.vue'

const needRefresh = ref(false)
const updateServiceWorker = vi.fn()

vi.mock('virtual:pwa-register/vue', () => ({ useRegisterSW: () => ({ needRefresh, updateServiceWorker }) }))

function upload(status: UploadItem['status']): UploadItem {
  return {
    clientId: 'client-1', filename: 'file.bin', size: 9, lastModified: 1, completedParts: [], activeParts: [],
    transferredByPart: {}, sentBytes: 0, progress: 0, createdAt: 't', selected: true, status,
    ...(status === 'FAILED' ? { error: 'x', errorCode: 'NETWORK_ERROR', retryable: true } : {}),
  } as UploadItem
}

describe('PWAUpdatePrompt', () => {
  beforeEach(() => {
    uploadState.items = []
    needRefresh.value = false
    updateServiceWorker.mockClear()
  })

  it('stays silent without a waiting worker and never reloads on its own', async () => {
    const wrapper = mount(PWAUpdatePrompt)
    expect(wrapper.find('.update-toast').exists()).toBe(false)
    needRefresh.value = true
    await vi.waitFor(() => expect(wrapper.find('.update-toast').exists()).toBe(true))
    expect(updateServiceWorker).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('prompts and activates the update only on explicit consent', async () => {
    needRefresh.value = true
    const wrapper = mount(PWAUpdatePrompt)
    await wrapper.get('.update-toast .button').trigger('click')
    expect(updateServiceWorker).toHaveBeenCalledWith(true)
    wrapper.unmount()
  })

  it('disables activation while a transfer is active', async () => {
    needRefresh.value = true
    uploadState.items.push(upload('UPLOADING'))
    const wrapper = mount(PWAUpdatePrompt)
    const button = wrapper.get<HTMLButtonElement>('.update-toast .button')
    expect(button.attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('当前有文件正在上传')
    await button.trigger('click')
    expect(updateServiceWorker).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('allows an update while paused but warns that files must be reselected', async () => {
    needRefresh.value = true
    uploadState.items.push(upload('PAUSED'))
    const wrapper = mount(PWAUpdatePrompt)
    expect(wrapper.text()).toContain('更新后需要重新选择暂停的文件')
    const button = wrapper.get<HTMLButtonElement>('.update-toast .button')
    expect(button.attributes('disabled')).toBeUndefined()
    await button.trigger('click')
    expect(updateServiceWorker).toHaveBeenCalledWith(true)
    wrapper.unmount()
  })
})
