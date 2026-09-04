import { QueryClient, VueQueryPlugin } from '@tanstack/vue-query'
import { flushPromises, mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { nextTick } from 'vue'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import QRCode from 'qrcode'
import { ApiError, DefaultService, TOTPEnrollmentPending } from '@/api/generated'
import { useAuthStore } from '@/features/auth/store'
import { toast } from '@/shared/ui/toast'
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
    toast.clear()
    vi.spyOn(QRCode, 'toCanvas').mockResolvedValue(undefined)
    vi.spyOn(DefaultService, 'getTotpStatus').mockResolvedValue({ enabled: false })
    vi.spyOn(DefaultService, 'listSessions').mockResolvedValue([])
    vi.spyOn(DefaultService, 'listDevices').mockResolvedValue([])
  })

  it('sends the current password and clears it after enrollment starts', async () => {
    const provisioningUri = 'otpauth://totp/RelayShelf%3Aalice?secret=ABCDEFGHIJKLMNOPQRSTUVWX23456789&issuer=RelayShelf'
    const start = vi.spyOn(DefaultService, 'startTotpEnrollment').mockResolvedValue({
      secret: 'ABCDEFGHIJKLMNOPQRSTUVWX23456789',
      otpauthUrl: provisioningUri,
      digits: TOTPEnrollmentPending.digits._6,
      periodSeconds: TOTPEnrollmentPending.periodSeconds._30,
      algorithm: TOTPEnrollmentPending.algorithm.SHA1,
    })
    const wrapper = await render()
    await wrapper.get('[aria-controls="totp-form"]').trigger('click')
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
    expect(wrapper.findAll('input[type="password"]')).toHaveLength(0)
    expect(wrapper.get('[aria-label="TOTP enrollment 二维码"]').element).toBeInstanceOf(HTMLCanvasElement)
    expect(QRCode.toCanvas).toHaveBeenCalledWith(expect.any(HTMLCanvasElement), provisioningUri, expect.objectContaining({ errorCorrectionLevel: 'M' }))
    wrapper.unmount()
  })

  it('keeps enrollment material out of persistent browser and query state', async () => {
    const secret = 'PERSISTENCE_SENTINEL_234567'
    const provisioningUri = `otpauth://totp/RelayShelf%3Aalice?secret=${secret}&issuer=RelayShelf`
    const localWrite = vi.spyOn(Storage.prototype, 'setItem')
    vi.spyOn(DefaultService, 'startTotpEnrollment').mockResolvedValue({
      secret,
      otpauthUrl: provisioningUri,
      digits: TOTPEnrollmentPending.digits._6,
      periodSeconds: TOTPEnrollmentPending.periodSeconds._30,
      algorithm: TOTPEnrollmentPending.algorithm.SHA1,
    })
    const wrapper = await render()
    await wrapper.get('[aria-controls="totp-form"]').trigger('click')
    const passwordInput = wrapper.findAll('input[type="password"]').at(-1)!
    await passwordInput.setValue('TOTP_ENROLL_PASSWORD_SENTINEL')
    await wrapper.findAll('button').find((item) => item.text().includes('开始启用'))!.trigger('click')
    await flushPromises()

    expect(localWrite).not.toHaveBeenCalledWith(expect.any(String), expect.stringContaining(secret))
    expect(localStorage.length).toBe(0)
    expect(sessionStorage.length).toBe(0)
    expect(window.location.href).not.toContain(secret)
    expect(wrapper.find('[aria-label="TOTP enrollment 二维码"]').exists()).toBe(true)
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
    await wrapper.get('[aria-controls="totp-form"]').trigger('click')
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
    expect(wrapper.findAll('input[type="password"]')).toHaveLength(1)
    expect(toast.items.value.at(-1)).toMatchObject({ type: 'error', message: '两步验证设置已更新，请重新开始启用。' })
    wrapper.unmount()
  })

  it('keeps security forms collapsed until requested and preserves device, session, and password mutations', async () => {
    vi.spyOn(DefaultService, 'listDevices').mockResolvedValue([
      { id: 'device-1', name: 'Browser', userAgent: 'Desktop', firstSeenAt: '2026-08-29T00:00:00Z', lastSeenAt: '2026-08-29T00:00:00Z' },
      { id: 'device-2', name: 'Phone', userAgent: 'Mobile', firstSeenAt: '2026-08-28T00:00:00Z', lastSeenAt: '2026-08-29T00:00:00Z' },
    ])
    vi.spyOn(DefaultService, 'listSessions').mockResolvedValue([
      { id: 'session-1', deviceId: 'device-1', createdAt: '2026-08-29T00:00:00Z', lastSeenAt: '2026-08-29T00:00:00Z', expiresAt: '2026-08-30T00:00:00Z', absoluteExpiresAt: '2026-09-29T00:00:00Z', lastIp: '', current: true },
      { id: 'session-2', deviceId: 'device-2', createdAt: '2026-08-28T00:00:00Z', lastSeenAt: '2026-08-29T00:00:00Z', expiresAt: '2026-08-30T00:00:00Z', absoluteExpiresAt: '2026-09-29T00:00:00Z', lastIp: '', current: false },
    ])
    const rename = vi.spyOn(DefaultService, 'renameDevice').mockResolvedValue({ id: 'device-1', name: 'Work laptop', userAgent: 'Desktop', firstSeenAt: '2026-08-29T00:00:00Z', lastSeenAt: '2026-08-29T00:00:00Z' })
    const revoke = vi.spyOn(DefaultService, 'revokeSession').mockResolvedValue(undefined)
    const changePassword = vi.spyOn(DefaultService, 'changePassword').mockResolvedValue(undefined)
    const wrapper = await render()

    expect(wrapper.find('#password-form').exists()).toBe(false)
    expect(wrapper.find('#totp-form').exists()).toBe(false)
    expect(wrapper.text()).toContain('当前会话')
    await wrapper.findAll('button').find((item) => item.text().includes('重命名'))!.trigger('click')
    await wrapper.get('input[maxlength="100"]').setValue('Work laptop')
    await wrapper.get('.inline-form').trigger('submit')
    await flushPromises()
    expect(rename).toHaveBeenCalledWith('device-1', { name: 'Work laptop' })

    await wrapper.findAll('button').find((item) => item.text() === '撤销')!.trigger('click')
    await flushPromises()
    expect(revoke).toHaveBeenCalledWith('session-2')

    await wrapper.get('[aria-controls="password-form"]').trigger('click')
    const passwordInputs = wrapper.findAll('#password-form input')
    await passwordInputs[0]!.setValue('current-pass')
    await passwordInputs[1]!.setValue('new-password-123')
    await wrapper.get('#password-form').trigger('submit')
    await flushPromises()
    expect(changePassword).toHaveBeenCalledWith({ currentPassword: 'current-pass', newPassword: 'new-password-123' })
    expect(wrapper.find('#password-form').exists()).toBe(false)
    expect(toast.items.value.some((item) => item.type === 'success')).toBe(true)
    wrapper.unmount()
  })
})
