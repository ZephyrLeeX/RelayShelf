import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'

export function useDetailSelection() {
  const route = useRoute()
  const router = useRouter()
  const selectedMessageId = computed(() => typeof route.query.detail === 'string' ? route.query.detail : '')

  function openDetail(id: string) {
    if (!id || selectedMessageId.value === id) return
    void router.push({ query: { ...route.query, detail: id } })
  }

  function closeDetail() {
    if (!selectedMessageId.value) return
    const query = { ...route.query }
    delete query.detail
    // Attachment selection only has meaning while a detail is open.
    delete query.attachment
    void router.replace({ query })
  }

  return { selectedMessageId, openDetail, closeDetail }
}
