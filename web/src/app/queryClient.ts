import { MutationCache, QueryCache, QueryClient } from '@tanstack/vue-query'
import { ApiError } from '@/api/generated'
import { apiCodes, getApiErrorCode, isAuthExpired } from '@/shared/api/errors'
import { notifyAuthExpired } from '@/shared/api/authExpiry'
import { queryKeys } from '@/shared/api/queryKeys'

function shouldRetry(failureCount: number, error: unknown) {
  if (failureCount >= 1) return false
  if (error instanceof ApiError) return error.status >= 500
  return true
}

const storageRefreshCodes = new Set<string>([apiCodes.storageUnavailable, 'UPLOAD_FINALIZE_RETRYABLE'])

export function refreshStorageStatusOnError(error: unknown, queryKey?: readonly unknown[]) {
  if (!storageRefreshCodes.has(getApiErrorCode(error))) return
  if (queryKey && JSON.stringify(queryKey) === JSON.stringify(queryKeys.storage.status())) return
  void queryClient.invalidateQueries({ queryKey: queryKeys.storage.status() })
}

export const queryClient = new QueryClient({
  queryCache: new QueryCache({ onError: (error, query) => {
    if (isAuthExpired(error)) notifyAuthExpired()
    refreshStorageStatusOnError(error, query.queryKey)
  } }),
  mutationCache: new MutationCache({ onError: (error) => {
    if (isAuthExpired(error)) notifyAuthExpired()
    refreshStorageStatusOnError(error)
  } }),
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: shouldRetry,
      refetchOnWindowFocus: false,
    },
    mutations: { retry: false },
  },
})
