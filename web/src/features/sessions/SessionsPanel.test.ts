import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { nextTick } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, DefaultService, TOTPEnrollmentPending } from '@/api/generated'
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

  it('clears stale enrollment material and requires a fresh password after a rotation conflict', async () => {
    vi.spyOn(DefaultService, 'startTotpEnrollment').mockResolvedValue({
      secret: 'ABCDEFGHIJKLMNOPQRSTUVWX23456789',
      otpauthUrl: 'otpauth://totp/RelayShelf%3Aalice',
      digits: TOTPEnrollmentPending.digits._6,
      periodSeconds: TOTPEnrollmentPending.periodSeconds._30,
      algorithm: TOTPEnrollmentPending.algorithm.SHA1,
    })
    vi.spyOn(DefaultService, 'confirmTotpEnrollment').mockRejectedValue(new ApiError(
      {} as never,
      { url: '/auth/totp/confirm', ok: false, status: 409, statusText: 'Conflict', body: { code: 'TOTP_ENROLLMENT_CHANGED', message: 'changed' } },
      'Conflict',
    ))
    const wrapper = await render()
    const passwordInput = wrapper.findAll('input[type="password"]').at(-1)
    if (!passwordInput) throw new Error('TOTP enrollment password input missing')
    await passwordInput.setValue('TOTP_ENROLL_PASSWORD_SENTINEL')
    await wrapper.findAll('button').find((item) => item.text().includes('开始启用'))!.trigger('click')
    await flushPromises()
    const codeInput = wrapper.find('input[inputmode="numeric"]')
    await codeInput.setValue('123456')
    await wrapper.findAll('button').find((item) => item.text().includes('确认并启用'))!.trigger('click')
    await flushPromises()

    const state = wrapper.vm as unknown as { pendingEnrollment: unknown; totpCode: string; enrollmentPassword: string }
    expect(state.pendingEnrollment).toBeNull()
    expect(state.totpCode).toBe('')
    expect(state.enrollmentPassword).toBe('')
    expect(wrapper.text()).toContain('两步验证设置已更新，请重新开始启用。')
    expect(wrapper.findAll('input[type="password"]')).toHaveLength(3)
    wrapper.unmount()
  })
})
