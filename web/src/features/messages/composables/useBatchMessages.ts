import { computed, onBeforeUnmount, ref, watch, type Ref } from 'vue'
import { useQueryClient } from '@tanstack/vue-query'
import { DefaultService, type Message, type MessageSummary } from '@/api/generated'
import { queryKeys, type MessageFilters } from '@/shared/api/queryKeys'
import { executeMessageCommand, mutationErrorMessage } from '../mutations'

export type BatchAction = 'permanent' | 'tags' | 'trash' | 'restore'
type Failure = { message: Message; error: string }

export function useBatchMessages(items: Ref<MessageSummary[]>, filters: () => MessageFilters) {
  const client = useQueryClient()
  const active = ref(false)
  const selected = ref(new Map<string, Message>())
  const busy = ref(false)
  const completed = ref(0)
  const total = ref(0)
  const failures = ref<Failure[]>([])
  const succeeded = ref(0)
  let generation = 0
  let lastAction: BatchAction = 'trash'
  let lastTags: string[] = []
  const count = computed(() => selected.value.size)

  function reset() {
    generation++
    active.value = false
    selected.value = new Map()
    failures.value = []
    busy.value = false
    total.value = 0
  }
  watch(() => JSON.stringify(filters()), reset)
  onBeforeUnmount(reset)

  function toggle(message: Message) {
    if (busy.value) return
    if (selected.value.has(message.id)) selected.value.delete(message.id)
    else selected.value.set(message.id, message)
    failures.value = failures.value.filter(item => item.message.id !== message.id)
  }
  function selectLoaded() {
    if (busy.value) return
    for (const message of items.value) selected.value.set(message.id, message)
  }
  function clear() {
    if (busy.value) return
    selected.value.clear()
    failures.value = []
    total.value = 0
  }

  async function run(action: BatchAction, tagIds: string[] = [], retry = false) {
    if (busy.value) return
    const targets = retry ? failures.value.map(item => item.message) : [...selected.value.values()]
    if (!targets.length || (action === 'tags' && !tagIds.length)) return
    const current = ++generation
    lastAction = action
    lastTags = [...tagIds]
    busy.value = true
    completed.value = 0
    succeeded.value = 0
    total.value = targets.length
    failures.value = []
    let next = 0
    const changed: string[] = []
    async function worker() {
      while (current === generation && next < targets.length) {
        const original = targets[next++]!
        try {
          // An explicit retry uses current metadata, never the sensitive-body API.
          const message = retry ? await DefaultService.getMessage(original.id) : original
          const command = action === 'tags'
            ? { type: 'tags' as const, message, tagIds: [...new Set([...message.tags.map(tag => tag.id), ...tagIds])] }
            : { type: action, message }
          await executeMessageCommand(command)
          changed.push(message.id)
          if (current === generation) {
            selected.value.delete(message.id)
            succeeded.value++
          }
        } catch (error) {
          changed.push(original.id)
          if (current === generation) failures.value.push({ message: original, error: mutationErrorMessage(error) })
        } finally {
          if (current === generation) completed.value++
        }
      }
    }
    await Promise.all(Array.from({ length: Math.min(3, targets.length) }, worker))
    // Refresh once per batch; requests continue to use the existing domain services.
    for (const id of changed) void client.invalidateQueries({ queryKey: queryKeys.messages.detail(id) })
    void client.invalidateQueries({ queryKey: queryKeys.messages.lists() })
    void client.invalidateQueries({ queryKey: queryKeys.search.root() })
    void client.invalidateQueries({ queryKey: queryKeys.trash.list() })
    if (current === generation) busy.value = false
  }

  function retryFailed() { return run(lastAction, lastTags, true) }
  return { active, selected, busy, completed, total, failures, succeeded, count, toggle, selectLoaded, clear, reset, run, retryFailed }
}
