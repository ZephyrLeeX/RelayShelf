import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { nextTick } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { DefaultService, TOTPEnrollmentPending } from '@/api/generated'
import { useAuthStore } from '@/features/auth/store'
import SessionsPanel from './SessionsPanel.vue'

async function render() {
  const pinia = createPinia()
  const auth = useAuthStore(pinia)
  auth.user = { id: 'user-1', username: 'alice', displayName: 'Alice', isAdmin: false }
  auth.device = { id: 'device-1', name: 'Browser', userAgent: '', firstSeenAt: '2026-08-29T00:00:00Z', lastSeenAt: '2026-08-29T00:00:00Z' }
  auth.session = { id: 'session-1', deviceId: 'device-1', createdAt: '2026-08-29T00:00:00Z', lastSeenAt: '2026-08-29T00:00:00Z', expiresAt: '2026-08-30T00:00:00Z', absoluteExpiresAt: '2026-09-29T00:00:00Z', lastIp: '', current: true }
  const client = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  const wrapper = mount(SessionsPanel, { global: { plugins: [pinia, [VueQueryPlugin, { queryClient: client }]], stubs: { teleport: true } } })
  await flushPromises()
  return wrapper
}

describe('TOTP enrollment re-authentication', () => {
  beforeEach(() => {
    vi.spyOn(DefaultService, 'getTotpStatus').mockResolvedValue({ enabled: false })
    vi.spyOn(DefaultService, 'listSessions').mockResolvedValue([])
    vi.spyOn(DefaultService, 'listDevices').mockResolvedValue([])
  })

  it('sends the current password and clears it after enrollment starts', async () => {
    const start = vi.spyOn(DefaultService, 'startTotpEnrollment').mockResolvedValue({
      secret: 'ABCDEFGHIJKLMNOPQRSTUVWX23456789',
      otpauthUrl: 'otpauth://totp/RelayShelf%3Aalice',
      digits: TOTPEnrollmentPending.digits._6,
      periodSeconds: TOTPEnrollmentPending.periodSeconds._30,
      algorithm: TOTPEnrollmentPending.algorithm.SHA1,
    })
    const wrapper = await render()
    const passwordInputs = wrapper.findAll('input[type="password"]')
    const input = passwordInputs.at(-1)
    if (!input) throw new Error('TOTP enrollment password input missing')
    await input.setValue('TOTP_ENROLL_PASSWORD_SENTINEL')
    await nextTick()
    const button = wrapper.findAll('button').find((item) => item.text().includes('开始启用'))
    if (!button) throw new Error('enrollment button missing')
    expect((button.element as HTMLButtonElement).disabled).toBe(false)
    await button.trigger('click')
    await flushPromises()
    expect(start).toHaveBeenCalledWith({ currentPassword: 'TOTP_ENROLL_PASSWORD_SENTINEL' })
    expect((wrapper.vm as unknown as { enrollmentPassword: string }).enrollmentPassword).toBe('')
    expect(wrapper.findAll('input[type="password"]')).toHaveLength(2)
    wrapper.unmount()
  })
})
