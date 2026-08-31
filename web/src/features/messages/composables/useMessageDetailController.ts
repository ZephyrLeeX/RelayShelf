import { computed, onUnmounted, ref, toValue, watch, type MaybeRefOrGetter } from 'vue'
import { useQueryClient } from '@tanstack/vue-query'
import { useRoute, useRouter } from 'vue-router'
import { DefaultService, type RecipientUser } from '@/api/generated'
import { displayError } from '@/shared/api/errors'
import { useTagsQuery } from '@/features/tags/queries'
import { uploadManager } from '@/features/uploads/manager'
import { visibleUploads } from '@/features/uploads/store'
import { parseStoredContent, serializeContent, unwrapSingleFencedCode, type ContentTypeId } from '../content/contentFormat'
import { invalidateMessageTruth, mutationErrorMessage, useMessageMutation } from '../mutations'
import { useMessageDetail } from '../queries'

interface RevealedBody {
  messageId: string
  version: number
  body: string
}

export function useMessageDetailController(messageId: MaybeRefOrGetter<string>) {
  const route = useRoute()
  const router = useRouter()
  const detail = useMessageDetail(messageId)
  const mutation = useMessageMutation()
  const tags = useTagsQuery()
  const queryClient = useQueryClient()
  const revealedBody = ref<RevealedBody | null>(null)
  const revealPending = ref(false)
  let revealRequest = 0
  const editing = ref(false)
  const editBody = ref('')
  const editContentType = ref<ContentTypeId>('text')
  const selectedTags = ref<string[]>([])
  const error = ref('')
  const forwardRecipient = ref<RecipientUser | null>(null)
  const forwardOpen = ref(false)
  const notice = ref('')
  const attachmentInput = ref<HTMLInputElement>()
  const detailUploadClients = ref<string[]>([])
  const attachmentMutationPending = ref(false)
  const message = computed(() => detail.data.value)
  const detailUploads = computed(() => detailUploadClients.value.flatMap((id) => {
    const item = visibleUploads.value.find((candidate) => candidate.clientId === id)
    return item ? [item] : []
  }))
  const completedDetailUploads = computed(() => detailUploads.value.flatMap((item) => item.status === 'COMPLETED' ? [item.serverUploadId] : []))
  const detailUploadsReady = computed(() => detailUploads.value.length > 0 && detailUploads.value.every((item) => item.status === 'COMPLETED'))
  // The controller, not UploadManager, owns which completed uploads are
  // selected for this message.
  const restorableUploads = computed(() => visibleUploads.value.filter((item) => item.status === 'COMPLETED' && !detailUploadClients.value.includes(item.clientId)))
  const viewerId = computed(() => typeof route.query.attachment === 'string' && message.value?.attachments.some((file) => file.id === route.query.attachment) ? route.query.attachment : '')
  const currentSensitiveBody = computed(() => {
    const current = message.value
    const revealed = revealedBody.value
    return current?.sensitive && revealed?.messageId === current.id && revealed.version === current.version
      ? revealed.body
      : null
  })

  function clearRevealedBody() {
    revealRequest++
    revealedBody.value = null
    revealPending.value = false
  }

  watch(message, (value) => {
    if (!value) return
    // Editing state derives from stored content: a Markdown body that is one
    // recognized fenced block edits as that code language with the fence
    // stripped; plain TEXT always edits as 纯文本 (detectedLanguage is a
    // display hint only and never rewrites history).
    const parsed = parseStoredContent(value.body, value.bodyFormat)
    editBody.value = parsed.text
    editContentType.value = parsed.typeId
    selectedTags.value = value.tags.map((tag) => tag.id)
  }, { immediate: true })
  watch(
    () => [message.value?.id, message.value?.version, message.value?.sensitive] as const,
    () => { clearRevealedBody(); editBody.value = ''; editing.value = false },
  )
  onUnmounted(clearRevealedBody)

  async function reveal() {
    const current = message.value
    if (!current?.sensitive) return
    const requestedMessageId = current.id
    const requestedVersion = current.version
    const request = ++revealRequest
    revealPending.value = true
    error.value = ''
    try {
      const response = await DefaultService.revealSensitiveBody(requestedMessageId)
      const latest = message.value
      if (
        request === revealRequest
        && toValue(messageId) === requestedMessageId
        && latest?.id === requestedMessageId
        && latest.version === requestedVersion
        && response.version === latest.version
        && latest.sensitive
      ) {
        revealedBody.value = { messageId: requestedMessageId, version: response.version, body: response.body }
      }
    } catch (cause) {
      if (request === revealRequest) error.value = displayError(cause)
    } finally {
      if (request === revealRequest) revealPending.value = false
    }
  }

  async function copy() {
    if (!message.value) return
    if (message.value.sensitive && currentSensitiveBody.value === null) await reveal()
    if (message.value.sensitive) {
      if (currentSensitiveBody.value !== null) await navigator.clipboard.writeText(currentSensitiveBody.value)
      return
    }
    // A body that is exactly one fenced code block copies as bare code.
    const body = unwrapSingleFencedCode(message.value.body) ?? message.value.body
    if (body !== null) await navigator.clipboard.writeText(body)
  }

  function run(command: Parameters<typeof mutation.mutate>[0], success?: () => void) {
    error.value = ''
    mutation.mutate(command, {
      onSuccess: () => { clearRevealedBody(); success?.() },
      onError: (cause) => { error.value = mutationErrorMessage(cause) },
    })
  }

  function forward() {
    if (!message.value || !forwardRecipient.value) return
    error.value = ''
    notice.value = ''
    mutation.mutate({ type: 'forward', message: message.value, recipientUserId: forwardRecipient.value.id }, {
      onSuccess: () => { notice.value = '已转发'; forwardRecipient.value = null; forwardOpen.value = false },
      onError: (cause) => { error.value = mutationErrorMessage(cause) },
    })
  }

  async function startEdit() {
    if (!message.value) return
    if (message.value.sensitive) {
      if (currentSensitiveBody.value === null) await reveal()
      if (currentSensitiveBody.value === null) return
      editBody.value = currentSensitiveBody.value
    } else {
      const parsed = parseStoredContent(message.value.body, message.value.bodyFormat)
      editBody.value = parsed.text
      editContentType.value = parsed.typeId
    }
    editing.value = true
  }

  function saveBody() {
    if (!message.value || !editBody.value.trim()) return
    if (message.value.sensitive && currentSensitiveBody.value === null) {
      editing.value = false
      error.value = '内容版本已更新，请重新显示正文后再编辑。'
      return
    }
    // Sensitive edits keep their existing format contract (body only);
    // ordinary edits serialize through the shared content-format bridge so
    // picking Shell/Python/Java stores a Markdown fenced block.
    const command = message.value.sensitive
      ? { type: 'editSensitive' as const, message: message.value, body: editBody.value }
      : (() => {
          const serialized = serializeContent(editBody.value, editContentType.value)
          return { type: 'edit' as const, message: message.value!, body: serialized.body, bodyFormat: serialized.bodyFormat }
        })()
    run(command, () => { editing.value = false })
  }

  function removeForever(onDeleted?: () => void) {
    if (message.value && window.confirm('永久删除后无法恢复。确定继续吗？')) run({ type: 'delete', message: message.value }, onDeleted)
  }

  function openViewer(id: string) { void router.push({ query: { ...route.query, attachment: id } }) }
  function closeViewer() { router.back() }
  function selectViewer(id: string) { void router.replace({ query: { ...route.query, attachment: id } }) }

  async function chooseDetailFiles(event: Event) {
    const input = event.target as HTMLInputElement
    if (input.files) detailUploadClients.value.push(...await uploadManager.addFiles(Array.from(input.files)))
    input.value = ''
  }

  async function addAttachments() {
    if (!message.value || !detailUploadsReady.value) return
    attachmentMutationPending.value = true
    error.value = ''
    // Snapshot this request so a newer selection survives its completion.
    const currentMessageId = message.value.id
    const expectedVersion = message.value.version
    const uploadIds = [...completedDetailUploads.value]
    const consumed = new Set(uploadIds)
    const consumedClients = detailUploadClients.value.filter((clientId) => {
      const item = uploadManager.getItem(clientId)
      return Boolean(item?.serverUploadId && consumed.has(item.serverUploadId))
    })
    try {
      await DefaultService.addMessageAttachments(currentMessageId, { expectedVersion, uploadIds })
      detailUploadClients.value = detailUploadClients.value.filter((clientId) => !consumedClients.includes(clientId))
      uploadManager.retireUploadIds(uploadIds)
      invalidateMessageTruth(currentMessageId, queryClient)
    } catch (cause) {
      error.value = mutationErrorMessage(cause)
      invalidateMessageTruth(currentMessageId, queryClient)
    } finally {
      attachmentMutationPending.value = false
    }
  }

  function addRestored(clientId: string) {
    if (!detailUploadClients.value.includes(clientId)) detailUploadClients.value.push(clientId)
  }

  async function removeAttachment(id: string) {
    if (!message.value) return
    error.value = ''
    try {
      await DefaultService.removeMessageAttachment(message.value.id, id, { expectedVersion: message.value.version })
      invalidateMessageTruth(message.value.id, queryClient)
    } catch (cause) {
      error.value = mutationErrorMessage(cause)
      invalidateMessageTruth(message.value.id, queryClient)
    }
  }

  return {
    detail, mutation, tags, message, revealPending, editing, editBody, editContentType, selectedTags,
    error, forwardRecipient, forwardOpen, notice, attachmentInput, detailUploads, detailUploadsReady,
    restorableUploads, attachmentMutationPending, viewerId, currentSensitiveBody,
    clearRevealedBody, reveal, copy, run, forward, startEdit, saveBody, removeForever,
    openViewer, closeViewer, selectViewer, chooseDetailFiles, addAttachments, addRestored,
    removeAttachment,
  }
}
