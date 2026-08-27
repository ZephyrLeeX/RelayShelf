import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError, DefaultService } from '@/api/generated'
import { queryClient } from '@/app/queryClient'
import { getCsrfToken } from '@/shared/api/configure'
import { authFixture } from '@/test/fixtures'
import { useAuthStore } from './store'

function apiError(status: number, code: string) {
  return new ApiError({ method: 'GET', url: '/auth/session' }, { url: '/api/v1/auth/session', ok: false, status, statusText: 'error', body: { code, message: code, traceId: 'trace' } }, code)
}

describe('auth store', () => {
  beforeEach(() => { setActivePinia(createPinia()); queryClient.clear(); vi.restoreAllMocks() })
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
  it('accepts login bootstrap', async () => {
    vi.spyOn(DefaultService, 'login').mockResolvedValue(authFixture)
    const store = useAuthStore()
    await store.login('alice', 'correct-password')
    expect(store.status).toBe('authenticated')
  })
})
