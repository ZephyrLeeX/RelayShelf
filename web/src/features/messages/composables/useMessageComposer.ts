import { computed, ref, watch, type MaybeRefOrGetter } from 'vue'
import { toValue } from 'vue'
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import {
  BodyFormat,
  DefaultService,
  type CreateMessageRequest,
  type DirectSendRequest,
  type Lifecycle,
} from '@/api/generated'
import { displayError } from '@/shared/api/errors'
import { queryKeys } from '@/shared/api/queryKeys'
import { useCreateTag, useTagsQuery } from '@/features/tags/queries'
import { uploadManager } from '@/features/uploads/manager'
import { visibleUploads } from '@/features/uploads/store'
import type { UploadItem } from '@/features/uploads/types'

export type ComposerMode = 'text' | 'markdown' | 'code'

interface SendSnapshot {
  key: string
  fingerprint: string
  identity: string
  payload: CreateMessageRequest
}

function isUUID(value: string) {
  return /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(value)
}

export function useMessageComposer(defaultLifecycle: MaybeRefOrGetter<Lifecycle>, onSent?: () => void) {
  const body = ref('')
  const mode = ref<ComposerMode>('text')
  const lifecycle = ref(toValue(defaultLifecycle))
  const sensitive = ref(false)
  const selectedTags = ref<string[]>([])
  const selectedUploadClients = ref<string[]>([])
  const dragging = ref(false)
  const activeKey = ref('')
  const failed = ref(false)
  const error = ref('')
  const newTagName = ref('')
  const newTagColor = ref('#3B8C6E')
  const directRecipient = ref('')

  const tags = useTagsQuery()
  const createTag = useCreateTag()
  const client = useQueryClient()
  const directMode = computed(() => isUUID(directRecipient.value.trim()))
  const byteLength = computed(() => new TextEncoder().encode(body.value).byteLength)
  const tooLarge = computed(() => byteLength.value > 1024 * 1024)
  const selectedUploads = computed<UploadItem[]>(() => selectedUploadClients.value.flatMap((id) => {
    const item = visibleUploads.value.find((upload) => upload.clientId === id)
    return item ? [item] : []
  }))
  const selectedUploadIds = computed(() => selectedUploads.value.flatMap((item) =>
    item.status === 'COMPLETED' && item.serverUploadId ? [item.serverUploadId] : []))
  const attachmentsBlocking = computed(() => selectedUploads.value.some((item) => item.status !== 'COMPLETED'))
  const hasContent = computed(() => Boolean(body.value.trim()) || selectedUploadIds.value.length > 0)
  const bodyFormat = computed(() => mode.value === 'markdown' ? BodyFormat.MARKDOWN : BodyFormat.TEXT)
  // Restored completed uploads remain globally owned until this draft selects
  // and successfully consumes them.
  const restorableUploads = computed(() => visibleUploads.value.filter((item) =>
    item.status === 'COMPLETED' && !selectedUploadClients.value.includes(item.clientId)))

  // Request identity covers the API payload. Draft identity also covers
  // pending upload client IDs so progress changes do not rotate retry keys,
  // while a changed selection always does.
  const draftFields = () => [
    body.value,
    mode.value,
    directMode.value ? 'DIRECT' : lifecycle.value,
    sensitive.value,
    directMode.value ? 'direct' : [...selectedTags.value].sort().join(','),
  ]
  const requestFingerprint = computed(() => JSON.stringify({ draft: draftFields(), uploadIds: selectedUploadIds.value }))
  const draftIdentity = computed(() => JSON.stringify({ draft: draftFields(), selection: [...selectedUploadClients.value] }))
  let attemptedIdentity = ''
  let attemptedFingerprint = ''

  const send = useMutation({
    retry: false,
    mutationFn: ({ key, payload }: SendSnapshot) => DefaultService.createMessage(key, payload),
    onSuccess: (_result, snapshot) => {
      // Compare before retiring uploads: retirement changes the live request
      // fingerprint even when the user has not edited the draft.
      const unchanged = draftIdentity.value === snapshot.identity
      const consumed = new Set(snapshot.payload.uploadIds ?? [])
      selectedUploadClients.value = selectedUploadClients.value.filter((clientId) => {
        const item = uploadManager.getItem(clientId)
        return !item?.serverUploadId || !consumed.has(item.serverUploadId)
      })
      uploadManager.retireUploadIds([...consumed])
      if (unchanged) {
        body.value = ''
        selectedTags.value = []
        selectedUploadClients.value = []
        sensitive.value = false
      }
      activeKey.value = ''
      attemptedIdentity = ''
      attemptedFingerprint = ''
      failed.value = false
      error.value = ''
      void client.invalidateQueries({ queryKey: queryKeys.messages.root() })
      void client.invalidateQueries({ queryKey: queryKeys.search.root() })
      onSent?.()
    },
    onError: (cause, snapshot) => {
      failed.value = true
      error.value = displayError(cause)
      if (requestFingerprint.value !== snapshot.fingerprint || draftIdentity.value !== snapshot.identity) activeKey.value = ''
    },
  })

  const direct = useMutation({
    retry: false,
    mutationFn: (snapshot: { key: string; identity: string; payload: DirectSendRequest }) =>
      DefaultService.directSendMessage(snapshot.key, snapshot.payload),
    onSuccess: (_result, snapshot) => {
      const unchanged = draftIdentity.value === snapshot.identity
      const consumed = new Set(snapshot.payload.uploadIds ?? [])
      selectedUploadClients.value = selectedUploadClients.value.filter((clientId) => {
        const item = uploadManager.getItem(clientId)
        return !item?.serverUploadId || !consumed.has(item.serverUploadId)
      })
      uploadManager.retireUploadIds([...consumed])
      if (unchanged) {
        body.value = ''
        selectedTags.value = []
        selectedUploadClients.value = []
        sensitive.value = false
        directRecipient.value = ''
      }
      activeKey.value = ''
      failed.value = false
      error.value = ''
      void client.invalidateQueries({ queryKey: queryKeys.messages.root() })
      onSent?.()
    },
    onError: (cause) => {
      failed.value = true
      error.value = displayError(cause)
    },
  })

  const sending = computed(() => directMode.value ? direct.isPending.value : send.isPending.value)

  watch([requestFingerprint, draftIdentity], ([fingerprint, identity]) => {
    if (failed.value && (fingerprint !== attemptedFingerprint || identity !== attemptedIdentity)) {
      activeKey.value = ''
      failed.value = false
      error.value = ''
    }
  })

  function validate() {
    error.value = ''
    if (!hasContent.value) error.value = '请输入正文或添加至少一个附件。'
    else if (attachmentsBlocking.value) error.value = '等待附件上传完成，或移除失败的附件。'
    else if (tooLarge.value) error.value = '正文 UTF-8 大小不能超过 1 MiB。'
    return !error.value
  }

  function submit() {
    if (send.isPending.value || !validate()) return
    if (!activeKey.value) activeKey.value = crypto.randomUUID()
    attemptedIdentity = draftIdentity.value
    attemptedFingerprint = requestFingerprint.value
    send.mutate({
      key: activeKey.value,
      fingerprint: requestFingerprint.value,
      identity: attemptedIdentity,
      payload: {
        body: body.value.trim() ? body.value : null,
        bodyFormat: bodyFormat.value,
        lifecycle: lifecycle.value,
        sensitive: sensitive.value,
        tagIds: [...selectedTags.value],
        uploadIds: [...selectedUploadIds.value],
      },
    })
  }

  function submitDirect() {
    if (direct.isPending.value || !validate()) return
    direct.mutate({
      key: activeKey.value || crypto.randomUUID(),
      identity: draftIdentity.value,
      payload: {
        recipientUserId: directRecipient.value.trim(),
        body: body.value.trim() ? body.value : null,
        bodyFormat: bodyFormat.value,
        sensitive: sensitive.value,
        uploadIds: [...selectedUploadIds.value],
      },
    })
  }

  async function selectFiles(files: FileList | File[]) {
    const ids = await uploadManager.addFiles(Array.from(files))
    selectedUploadClients.value.push(...ids)
  }

  function removeSelected(clientId: string) {
    const item = uploadManager.getItem(clientId)
    selectedUploadClients.value = selectedUploadClients.value.filter((id) => id !== clientId)
    if (item && item.status !== 'COMPLETED') uploadManager.remove(clientId)
  }

  function pasteFiles(event: ClipboardEvent) {
    const images = Array.from(event.clipboardData?.items ?? []).filter((item) =>
      item.kind === 'file' && item.type.startsWith('image/'))
    if (!images.length) return
    event.preventDefault()
    const files = images.flatMap((item) => {
      const blob = item.getAsFile()
      if (!blob) return []
      if (blob.name) return [blob]
      const extension = item.type.split('/')[1]?.replace('jpeg', 'jpg') || 'png'
      const stamp = new Date().toISOString().replace(/[-:]/g, '').replace('T', '-').slice(0, 15)
      return [new File([blob], `pasted-${stamp}.${extension}`, { type: item.type, lastModified: Date.now() })]
    })
    void selectFiles(files)
  }

  function dropFiles(event: DragEvent) {
    dragging.value = false
    if (event.dataTransfer?.files.length) void selectFiles(event.dataTransfer.files)
  }

  function addRestored(clientId: string) {
    if (!selectedUploadClients.value.includes(clientId)) selectedUploadClients.value.push(clientId)
  }

  async function addTag() {
    const name = newTagName.value.trim()
    if (!name) return
    try {
      const tag = await createTag.mutateAsync({ name, color: newTagColor.value })
      selectedTags.value.push(tag.id)
      newTagName.value = ''
    } catch (cause) {
      error.value = displayError(cause)
    }
  }

  function onKeydown(event: KeyboardEvent) {
    if (event.key !== 'Enter' || (!event.ctrlKey && !event.metaKey)) return
    event.preventDefault()
    if (directMode.value) submitDirect()
    else submit()
  }

  return {
    body, mode, lifecycle, sensitive, selectedTags, selectedUploadClients, selectedUploads,
    selectedUploadIds, restorableUploads, directRecipient, directMode, byteLength, tooLarge,
    attachmentsBlocking, hasContent, dragging, error, failed, sending, tags, newTagName,
    newTagColor, selectFiles, removeSelected, pasteFiles, dropFiles, addTag, addRestored,
    submit, submitDirect, onKeydown,
  }
}
