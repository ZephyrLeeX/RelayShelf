import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, DefaultService } from '@/api/generated'
import { queryClient } from '@/app/queryClient'
import { getCsrfToken } from '@/shared/api/configure'
import { authFixture } from '@/test/fixtures'
import { clearShare, pendingShare } from '@/features/share/receive'
import { useAuthStore } from './store'

function apiError(status: number, code: string) {
  return new ApiError({ method: 'GET', url: '/auth/session' }, { url: '/api/v1/auth/session', ok: false, status, statusText: 'error', body: { code, message: code, traceId: 'trace' } }, code)
}

describe('auth store', () => {
  beforeEach(() => { clearShare(); setActivePinia(createPinia()); queryClient.clear(); vi.restoreAllMocks() })
  it('bootstraps an authenticated session and configures CSRF', async () => {
    vi.spyOn(DefaultService, 'getAuthSession').mockResolvedValue(authFixture)
    const store = useAuthStore()
    expect(await store.bootstrap()).toBe('authenticated')
    expect(store.user?.username).toBe('alice')
    expect(getCsrfToken()).toBe('csrf-a')
  })
  it('treats a 401 as guest but not a network error', async () => {
    vi.spyOn(DefaultService, 'getAuthSession').mockRejectedValueOnce(apiError(401, 'AUTH_REQUIRED')).mockRejectedValueOnce(new TypeError('offline'))
    const guest = useAuthStore()
    await guest.bootstrap()
    expect(guest.status).toBe('guest')
    guest.status = 'unknown'
    await guest.bootstrap()
    expect(guest.status).toBe('error')
  })
  it('clears private cache and old CSRF on logout', async () => {
    vi.spyOn(DefaultService, 'logout').mockResolvedValue(undefined)
    const store = useAuthStore()
    store.accept(authFixture)
    queryClient.setQueryData(['messages', 'list'], ['alice-private'])
    await store.logout()
    expect(queryClient.getQueryData(['messages', 'list'])).toBeUndefined()
    expect(getCsrfToken()).toBeUndefined()
    store.accept({ ...authFixture, user: { ...authFixture.user, id: 'user-2', username: 'bob' }, csrfToken: 'csrf-b' })
    expect(queryClient.getQueryData(['messages', 'list'])).toBeUndefined()
  })
  it('retains incoming share through guest bootstrap and login, clears it on logout', async () => {
    vi.spyOn(DefaultService, 'getAuthSession').mockRejectedValue(apiError(401, 'AUTH_REQUIRED'))
    vi.spyOn(DefaultService, 'login').mockResolvedValue(authFixture)
    vi.spyOn(DefaultService, 'logout').mockResolvedValue(undefined)
    pendingShare.value = { title: '标题', body: '分享正文' }
    const store = useAuthStore()
    await store.bootstrap()
    expect(pendingShare.value?.body).toBe('分享正文')
    await store.login('alice', 'correct-password')
    expect(pendingShare.value?.body).toBe('分享正文')
    await store.logout()
    expect(pendingShare.value).toBeNull()
  })
  it('keeps incoming share across the TOTP challenge and completion', async () => {
    const challenge = { challengeToken: 'challenge-token', expiresAt: '2026-09-30T12:00:00Z' }
    vi.spyOn(DefaultService, 'login').mockResolvedValue(challenge)
    vi.spyOn(DefaultService, 'completeLoginTotp').mockResolvedValue(authFixture)
    const store = useAuthStore()
    pendingShare.value = { title: '', body: '待确认内容' }
    expect(await store.login('alice', 'password')).toEqual(challenge)
    expect(pendingShare.value?.body).toBe('待确认内容')
    await store.completeTotpLogin('challenge-token', '123456')
    expect(pendingShare.value?.body).toBe('待确认内容')
  })
  it('accepts login bootstrap', async () => {
    vi.spyOn(DefaultService, 'login').mockResolvedValue(authFixture)
    const store = useAuthStore()
    await store.login('alice', 'correct-password')
    expect(store.status).toBe('authenticated')
  })
})
