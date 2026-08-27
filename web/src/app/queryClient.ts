import { MutationCache, QueryCache, QueryClient } from '@tanstack/vue-query'
import { ApiError } from '@/api/generated'
import { isAuthExpired } from '@/shared/api/errors'
import { notifyAuthExpired } from '@/shared/api/authExpiry'

function shouldRetry(failureCount: number, error: unknown) {
  if (failureCount >= 1) return false
  if (error instanceof ApiError) return error.status >= 500
  return true
}

export const queryClient = new QueryClient({
  queryCache: new QueryCache({ onError: (error) => { if (isAuthExpired(error)) notifyAuthExpired() } }),
  mutationCache: new MutationCache({ onError: (error) => { if (isAuthExpired(error)) notifyAuthExpired() } }),
  defaultOptions: {
    queries: {
      staleTime: 30_000,
      retry: shouldRetry,
      refetchOnWindowFocus: false,
    },
    mutations: { retry: false },
  },
})
