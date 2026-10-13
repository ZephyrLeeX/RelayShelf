import { computed, type MaybeRefOrGetter, toValue } from 'vue'
import { useInfiniteQuery, useQuery } from '@tanstack/vue-query'
import { DefaultService } from '@/api/generated'
import { queryKeys, type MessageFilters } from '@/shared/api/queryKeys'

export const PAGE_SIZE = 30

export function useMessageFeed(filters: MaybeRefOrGetter<MessageFilters>, enabled: MaybeRefOrGetter<boolean> = true) {
  const resolved = computed(() => toValue(filters))
  const key = computed(() => {
    const value = resolved.value
    if (value.search) return queryKeys.search.results(value.search)
    if (value.trash) return queryKeys.trash.list()
    return queryKeys.messages.list(value)
  })
  return useInfiniteQuery({
    queryKey: key,
    enabled: computed(() => toValue(enabled)),
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => {
      const value = resolved.value
      if (value.trash) return DefaultService.listTrash(pageParam, PAGE_SIZE)
      if (value.search) {
        const search = value.search
        return DefaultService.searchMessages(search.q, search.lifecycle, search.favorite, search.tagIds, search.type, search.createdAfter, search.createdBefore, pageParam, PAGE_SIZE)
      }
      return DefaultService.listMessages(value.lifecycle, value.favorite, value.tagIds, pageParam, PAGE_SIZE)
    },
    getNextPageParam: (lastPage) => lastPage.nextCursor ?? undefined,
  })
}

export function useMessageDetail(id: MaybeRefOrGetter<string>) {
  return useQuery({
    queryKey: computed(() => queryKeys.messages.detail(toValue(id))),
    queryFn: () => DefaultService.getMessage(toValue(id)),
  })
}
