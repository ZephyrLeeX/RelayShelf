import { useQuery } from '@tanstack/vue-query'
import { DefaultService } from '@/api/generated'
export const savedSearchKey = ['saved-searches'] as const
export function useSavedSearches() {
  return useQuery({ queryKey: savedSearchKey, queryFn: () => DefaultService.listSavedSearches(), staleTime: 0, refetchOnWindowFocus: true })
}
