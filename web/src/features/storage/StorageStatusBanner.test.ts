import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import { StorageRuntimeStatus } from '@/api/generated'
import { setStorageRuntimeStatus } from './runtime'
import StorageStatusBanner from './StorageStatusBanner.vue'

const status = (reason: StorageRuntimeStatus.reason) => ({
  healthy: reason === StorageRuntimeStatus.reason.HEALTHY,
  reason,
  lastCheckedAt: null,
  changedAt: '2026-09-03T00:00:00Z',
})

describe('StorageStatusBanner', () => {
  afterEach(() => setStorageRuntimeStatus(undefined))

  it('is absent while healthy and disappears after recovery', async () => {
    setStorageRuntimeStatus(status(StorageRuntimeStatus.reason.HEALTHY))
    const wrapper = mount(StorageStatusBanner)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
    setStorageRuntimeStatus(status(StorageRuntimeStatus.reason.NAS_TIMEOUT))
    await wrapper.vm.$nextTick()
    expect(wrapper.text()).toContain('存储服务暂时不可用')
    setStorageRuntimeStatus(status(StorageRuntimeStatus.reason.HEALTHY))
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })

  it.each([StorageRuntimeStatus.reason.NAS_UNAVAILABLE, StorageRuntimeStatus.reason.NAS_TIMEOUT])('shows unavailable guidance for %s', (reason) => {
    setStorageRuntimeStatus(status(reason))
    const wrapper = mount(StorageStatusBanner)
    expect(wrapper.text()).toContain('文件上传、下载和预览当前不可用')
  })

  it('uses capacity-specific guidance for full storage', () => {
    setStorageRuntimeStatus(status(StorageRuntimeStatus.reason.NAS_FULL))
    const wrapper = mount(StorageStatusBanner)
    expect(wrapper.text()).toContain('存储空间已满')
    expect(wrapper.text()).toContain('无法上传新的文件')
  })
})
