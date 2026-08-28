import { defineStore } from 'pinia'
import { DefaultService, type AuthBootstrap, type Device, type Session, type User } from '@/api/generated'
import { queryClient } from '@/app/queryClient'
import { setCsrfToken } from '@/shared/api/configure'
import { isAuthExpired, toApiError } from '@/shared/api/errors'
import { stopRealtime } from '@/app/realtime'
import { uploadManager } from '@/features/uploads/manager'

export type AuthStatus = 'unknown' | 'authenticated' | 'guest' | 'error'

let bootstrapRequest: Promise<AuthStatus> | undefined

export const useAuthStore = defineStore('auth', {
  state: () => ({
    status: 'unknown' as AuthStatus,
    user: null as User | null,
    device: null as Device | null,
    session: null as Session | null,
    csrfToken: null as string | null,
    bootstrapError: null as string | null,
  }),
  actions: {
    accept(data: AuthBootstrap) {
      this.user = data.user
      this.device = data.device
      this.session = data.session
      this.csrfToken = data.csrfToken
      this.status = 'authenticated'
      this.bootstrapError = null
      setCsrfToken(data.csrfToken)
    },
    clear(status: AuthStatus = 'guest') {
      uploadManager.clearForLogout()
      this.user = null
      this.device = null
      this.session = null
      this.csrfToken = null
      this.status = status
      setCsrfToken(undefined)
      stopRealtime()
      queryClient.clear()
    },
    async bootstrap(force = false): Promise<AuthStatus> {
      if (!force && this.status !== 'unknown' && this.status !== 'error') return this.status
      if (!force && bootstrapRequest) return bootstrapRequest
      bootstrapRequest = (async () => {
        try {
          this.accept(await DefaultService.getAuthSession())
        } catch (error) {
          if (isAuthExpired(error) || toApiError(error).status === 401) {
            this.clear('guest')
          } else {
            this.status = 'error'
            this.bootstrapError = toApiError(error).message
          }
        } finally {
          bootstrapRequest = undefined
        }
        return this.status
      })()
      return bootstrapRequest
    },
    async login(username: string, password: string) {
      const data = await DefaultService.login({ username, password, deviceName: defaultDeviceName() })
      queryClient.clear()
      this.accept(data)
    },
    async logout() {
      try {
        await DefaultService.logout()
      } catch (error) {
        if (!isAuthExpired(error)) throw error
      } finally {
        this.clear('guest')
      }
    },
  },
})

function defaultDeviceName() {
  const platform = navigator.platform || 'Browser'
  return `RelayShelf · ${platform}`.slice(0, 100)
}
