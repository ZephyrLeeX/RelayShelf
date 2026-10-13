import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { AdminUser, DefaultService, HealthState, StorageThresholdState, type AdminStatus } from '@/api/generated'
import { toast } from '@/shared/ui/toast'
import AdminView from './AdminView.vue'

const operationalStatus: AdminStatus = {
  state: HealthState.HEALTHY, databaseState: HealthState.HEALTHY,
  build: { version: '1.0.0', gitCommit: 'abcdef1234567890', buildTime: '2026-08-28T00:00:00Z' },
  migration: { currentVersion: 5, latestVersion: 5, compatible: true }, failedJobs: [],
  storage: { state: HealthState.HEALTHY, logicalUsageBytes: 8, maxStorageBytes: 100, thresholdState: StorageThresholdState.NORMAL, nasAvailableBytes: 80, nasTotalBytes: 100, stagingUsageBytes: 4, stagingAvailableBytes: 90, stagingTotalBytes: 100, degradedReasons: [] },
  security: { activeAdmins: 1, activeAdminsWithoutTOTP: 1, adminTotpSatisfied: false },
}

async function render() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  const wrapper = mount(AdminView, { global: { plugins: [[VueQueryPlugin, { queryClient: client }]], stubs: { teleport: true } } })
  await flushPromises()
  return wrapper
}

describe('admin operations', () => {
  beforeEach(() => {
    vi.spyOn(DefaultService, 'getAdminStatus').mockResolvedValue(operationalStatus)
    vi.spyOn(DefaultService, 'getStorageStatus').mockResolvedValue(operationalStatus.storage)
    vi.spyOn(DefaultService, 'getRuntimeSettings').mockResolvedValue({ temporaryTtlHours:72,trashTtlHours:168,maxFileSizeBytes:100,maxStorageBytes:null,auditRetentionDays:90,uploadRetentionHours:24,updatedAt:'2026-08-28T00:00:00Z' })
    vi.spyOn(DefaultService, 'listAdminUsers').mockResolvedValue({ items: [{ id:'user-a',username:'alice',displayName:'Alice',isAdmin:false,status:AdminUser.status.ACTIVE,createdAt:'2026-08-28T00:00:00Z',updatedAt:'2026-08-28T00:00:00Z' }], nextCursor: null })
  })

  it('shows pending, success and failure feedback for password resets without duplicates', async () => {
    toast.clear()
    let resolve!: () => void
    const reset = vi.spyOn(DefaultService, 'resetAdminUserPassword').mockImplementationOnce(() => new Promise<void>(done => { resolve = done }) as ReturnType<typeof DefaultService.resetAdminUserPassword>)
    const wrapper = await render()
    await wrapper.findAll('.admin-tabs button')[3].trigger('click'); await flushPromises()
    await wrapper.findAll('.actions button').find(button => button.text() === '重置密码')!.trigger('click')
    await wrapper.get('.confirm input').setValue('new-password-123')
    await wrapper.get('.confirm .danger').trigger('click'); await flushPromises()
    expect(wrapper.get('.confirm .danger').text()).toBe('处理中…')
    expect(wrapper.get('.confirm .danger').attributes('disabled')).toBeDefined()
    await wrapper.get('.confirm .danger').trigger('click')
    expect(reset).toHaveBeenCalledTimes(1)
    resolve(); await flushPromises()
    expect(wrapper.find('.confirm').exists()).toBe(false)
    expect(toast.items.value.at(-1)?.message).toContain('密码已重置')
    reset.mockRejectedValueOnce(new Error('reset failed'))
    await wrapper.findAll('.actions button').find(button => button.text() === '重置密码')!.trigger('click')
    await wrapper.get('.confirm input').setValue('new-password-456')
    await wrapper.get('.confirm .danger').trigger('click'); await flushPromises()
    expect(wrapper.find('.confirm').exists()).toBe(true)
    expect(toast.items.value.at(-1)?.type).toBe('error')
    wrapper.unmount(); toast.clear()
  })

  it('presents operational status and explicitly states the privacy boundary', async () => {
    const wrapper = await render()
    expect(wrapper.text()).toContain('私人内容不在此处出现')
    expect(wrapper.text()).toContain('PostgreSQL')
    expect(wrapper.text()).toContain('失败任务')
    wrapper.unmount()
  })

  it('localizes a full NAS degradation reason', async () => {
    vi.spyOn(DefaultService, 'getStorageStatus').mockResolvedValue({
      ...operationalStatus.storage, state: HealthState.DEGRADED, degradedReasons: ['NAS_FULL'],
    })
    const wrapper = await render()
    await wrapper.findAll('.admin-tabs button')[1].trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('NAS 空间已满')
    expect(wrapper.text()).not.toContain('NAS_FULL')
    wrapper.unmount()
  })

  it('requires the exact username before permanent deletion', async () => {
    const remove = vi.spyOn(DefaultService, 'deleteAdminUser').mockResolvedValue(undefined)
    const wrapper = await render()
    await wrapper.findAll('.admin-tabs button')[3].trigger('click'); await flushPromises()
    await wrapper.get('.actions .danger').trigger('click')
    const confirm = wrapper.get('.confirm .danger')
    expect((confirm.element as HTMLButtonElement).disabled).toBe(true)
    await wrapper.get('.confirm input').setValue('wrong')
    expect((confirm.element as HTMLButtonElement).disabled).toBe(true)
    await wrapper.get('.confirm input').setValue('alice')
    const enabledConfirm = wrapper.get('.confirm .danger')
    expect((enabledConfirm.element as HTMLButtonElement).disabled).toBe(false)
    await enabledConfirm.trigger('click'); await flushPromises()
    expect(remove).toHaveBeenCalledWith('user-a')
    wrapper.unmount()
  })

  it('loads further admin users through the cursor instead of a hard cap', async () => {
    const list = vi.spyOn(DefaultService, 'listAdminUsers')
      .mockResolvedValueOnce({ items: [{ id:'user-a',username:'alice',displayName:'Alice',isAdmin:false,status:AdminUser.status.ACTIVE,createdAt:'2026-08-28T00:00:00Z',updatedAt:'2026-08-28T00:00:00Z' }], nextCursor: 'page-2' })
      .mockResolvedValueOnce({ items: [{ id:'user-b',username:'bob',displayName:'Bob',isAdmin:false,status:AdminUser.status.ACTIVE,createdAt:'2026-08-28T00:01:00Z',updatedAt:'2026-08-28T00:01:00Z' }], nextCursor: null })
    const wrapper = await render()
    await wrapper.findAll('.admin-tabs button')[3].trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('alice')
    expect(wrapper.text()).not.toContain('bob')
    await wrapper.get('.load-more button').trigger('click'); await flushPromises()
    expect(list).toHaveBeenLastCalledWith('page-2', 30)
    expect(wrapper.text()).toContain('bob')
    expect(wrapper.find('.load-more').exists()).toBe(false)
    wrapper.unmount()
  })
})
