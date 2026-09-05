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
    await wrapper.findAll('button').find((item) => item.text() === '设置两步验证')!.trigger('click')
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
    await wrapper.findAll('button').find((item) => item.text() === '设置两步验证')!.trigger('click')
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
    await wrapper.findAll('button').find((item) => item.text() === '设置两步验证')!.trigger('click')
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
    expect(toast.items.value.at(-1)?.message).toBe('两步验证设置已更新，请重新开始启用。')
    expect(wrapper.findAll('input[type="password"]')).toHaveLength(1)
    wrapper.unmount()
  })
})

describe('device and security cards', () => {
  beforeEach(() => {
    toast.clear()
    vi.spyOn(DefaultService, 'getTotpStatus').mockResolvedValue({ enabled: false })
    vi.spyOn(DefaultService, 'listSessions').mockResolvedValue([])
    vi.spyOn(DefaultService, 'listDevices').mockResolvedValue([])
  })

  it('expands rename and password forms, keeps payloads and reports success through toast', async () => {
    const rename = vi.spyOn(DefaultService, 'renameDevice').mockResolvedValue({ id: 'device-1', name: 'Laptop' } as never)
    const change = vi.spyOn(DefaultService, 'changePassword').mockResolvedValue(undefined)
    const wrapper = await render()
    expect(wrapper.find('#rename-form').exists()).toBe(false)
    expect(wrapper.find('#password-form').exists()).toBe(false)
    expect(wrapper.get('#totp-form').attributes('style')).toContain('display: none')
    await wrapper.findAll('button').find((item) => item.text() === '重命名')!.trigger('click')
    await wrapper.get('#rename-form input').setValue('  Laptop  ')
    await wrapper.get('#rename-form').trigger('submit')
    await flushPromises()
    expect(rename).toHaveBeenCalledWith('device-1', { name: 'Laptop' })
    expect(wrapper.text()).toContain('Laptop')
    expect(toast.items.value.at(-1)?.message).toBe('设备名已保存')
    await wrapper.findAll('button').find((item) => item.text() === '修改密码')!.trigger('click')
    await wrapper.get('[autocomplete="current-password"]').setValue('old-password')
    await wrapper.get('[autocomplete="new-password"]').setValue('new-password')
    await wrapper.get('#password-form').trigger('submit')
    await flushPromises()
    expect(change).toHaveBeenCalledWith({ currentPassword: 'old-password', newPassword: 'new-password' })
    expect(wrapper.find('#password-form').exists()).toBe(false)
    expect((wrapper.vm as unknown as { currentPassword: string; newPassword: string }).currentPassword).toBe('')
    expect((wrapper.vm as unknown as { newPassword: string }).newPassword).toBe('')
    expect(toast.items.value.at(-1)?.message).toBe('密码已修改，其他会话已撤销')
    wrapper.unmount()
  })

  it('keeps current-session identification and revokes only the selected other session', async () => {
    vi.mocked(DefaultService.listSessions).mockResolvedValue([
      { id: 'session-1', current: true, deviceId: 'device-1', lastSeenAt: '2026-09-01' },
      { id: 'session-2', current: false, deviceId: 'device-2', lastSeenAt: '2026-09-01' },
    ] as never)
    const revoke = vi.spyOn(DefaultService, 'revokeSession').mockResolvedValue(undefined)
    const wrapper = await render()
    expect(wrapper.get('.session-card.current').text()).toContain('当前会话')
    expect(wrapper.get('.session-card.current').find('button').exists()).toBe(false)
    await wrapper.get('.session-card:not(.current) button').trigger('click')
    await flushPromises()
    expect(revoke).toHaveBeenCalledWith('session-2')
    expect(toast.items.value.at(-1)?.message).toBe('会话已撤销')
    wrapper.unmount()
  })

  it('retains password failure input and uses a global error toast', async () => {
    vi.spyOn(DefaultService, 'changePassword').mockRejectedValue(new Error('failed'))
    const wrapper = await render()
    await wrapper.findAll('button').find((item) => item.text() === '修改密码')!.trigger('click')
    await wrapper.get('[autocomplete="new-password"]').setValue('retry-password')
    await wrapper.get('#password-form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('#password-form').exists()).toBe(true)
    expect((wrapper.get('[autocomplete="new-password"]').element as HTMLInputElement).value).toBe('retry-password')
    expect(toast.items.value.at(-1)?.type).toBe('error')
    wrapper.unmount()
  })
})
