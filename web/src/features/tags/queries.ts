import { useMutation, useQuery, useQueryClient } from '@tanstack/vue-query'
import { DefaultService, type Tag, type TagRequest } from '@/api/generated'
import { queryKeys } from '@/shared/api/queryKeys'

export function useTagsQuery() {
  return useQuery({ queryKey: queryKeys.tags.all(), queryFn: () => DefaultService.listTags() })
}

export function useCreateTag() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (tag: TagRequest) => DefaultService.createTag(tag),
    onSuccess: (created) => {
      client.setQueryData<Tag[]>(queryKeys.tags.all(), (current = []) => current.some((tag) => tag.id === created.id) ? current : [...current, created])
      void client.invalidateQueries({ queryKey: queryKeys.tags.all() })
    },
  })
}
