import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { DefaultService, type TagRequest } from '@/api/generated'
import { queryKeys } from '@/shared/api/queryKeys'

export function useTagsQuery() {
  return useQuery({ queryKey: queryKeys.tags.all(), queryFn: () => DefaultService.listTags() })
}

export function useCreateTag() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (tag: TagRequest) => DefaultService.createTag(tag),
    onSuccess: () => client.invalidateQueries({ queryKey: queryKeys.tags.all() }),
  })
}
