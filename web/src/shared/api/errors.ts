import { ApiError } from '@/api/generated'

export const apiCodes = {
  authRequired: 'AUTH_REQUIRED',
  sessionExpired: 'AUTH_SESSION_EXPIRED',
  invalidCredentials: 'AUTH_INVALID_CREDENTIALS',
  csrfInvalid: 'CSRF_INVALID',
  versionConflict: 'MESSAGE_VERSION_CONFLICT',
  messageTrashed: 'MESSAGE_TRASHED',
  favoriteRequiresPermanent: 'MESSAGE_FAVORITE_REQUIRES_PERMANENT',
  validation: 'VALIDATION_ERROR',
  totpInvalid: 'TOTP_INVALID',
  totpChallengeExpired: 'TOTP_CHALLENGE_EXPIRED',
  totpAlreadyEnabled: 'TOTP_ALREADY_ENABLED',
  totpNotEnrolled: 'TOTP_NOT_ENROLLED',
} as const

export interface AppApiError {
  status: number
  code: string
  message: string
  traceId?: string
}

export function toApiError(error: unknown): AppApiError {
  if (error instanceof ApiError) {
    const body = typeof error.body === 'object' && error.body ? error.body as Record<string, unknown> : {}
    return {
      status: error.status,
      code: typeof body.code === 'string' ? body.code : 'HTTP_ERROR',
      message: typeof body.message === 'string' ? body.message : error.statusText,
      traceId: typeof body.traceId === 'string' ? body.traceId : undefined,
    }
  }
  return { status: 0, code: 'NETWORK_ERROR', message: '无法连接服务器，请检查网络后重试。' }
}

export function isAuthExpired(error: unknown) {
  const adapted = toApiError(error)
  return adapted.status === 401 && [apiCodes.authRequired, apiCodes.sessionExpired].includes(adapted.code as never)
}

export function displayError(error: unknown) {
  const adapted = toApiError(error)
  const suffix = adapted.traceId ? `（错误编号：${adapted.traceId}）` : ''
  return `${adapted.message || '请求失败'}${suffix}`
}
