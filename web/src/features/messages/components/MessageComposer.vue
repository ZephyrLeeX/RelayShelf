<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { BodyFormat, DefaultService, Lifecycle, type CreateMessageRequest } from '@/api/generated'
import { displayError } from '@/shared/api/errors'
import { queryKeys } from '@/shared/api/queryKeys'
import { useCreateTag, useTagsQuery } from '@/features/tags/queries'
import { uploadManager } from '@/features/uploads/manager'
import { visibleUploads } from '@/features/uploads/store'
import { formatBytes } from '@/shared/utils/bytes'
import type { UploadItem } from '@/features/uploads/types'

type ComposerMode = 'text' | 'markdown' | 'code'
const props = defineProps<{ defaultLifecycle: Lifecycle }>()
const emit = defineEmits<{ sent: [] }>()
const body = ref('')
const mode = ref<ComposerMode>('text')
const lifecycle = ref(props.defaultLifecycle)
const sensitive = ref(false)
const selectedTags = ref<string[]>([])
const selectedUploadClients = ref<string[]>([])
const fileInput = ref<HTMLInputElement>()
const dragging = ref(false)
const activeKey = ref('')
const failed = ref(false)
const error = ref('')
const newTagName = ref('')
const newTagColor = ref('#3B8C6E')
const tags = useTagsQuery()
const createTag = useCreateTag()
const client = useQueryClient()
const byteLength = computed(() => new TextEncoder().encode(body.value).byteLength)
const tooLarge = computed(() => byteLength.value > 1024 * 1024)
const selectedUploads = computed(() => selectedUploadClients.value.map((id) => visibleUploads.value.find((item) => item.clientId === id)).filter(Boolean) as unknown as UploadItem[])
const selectedUploadIds = computed(() => selectedUploads.value.flatMap((item) => item?.status === 'COMPLETED' && item.serverUploadId ? [item.serverUploadId] : []))
const attachmentsBlocking = computed(() => selectedUploads.value.some((item) => item?.status !== 'COMPLETED'))
const hasContent = computed(() => Boolean(body.value.trim()) || selectedUploadIds.value.length > 0)
const bodyFormat = computed(() => mode.value === 'markdown' ? BodyFormat.MARKDOWN : BodyFormat.TEXT)
// Completed uploads that exist globally (e.g. restored after reload) but are
// not part of this draft yet; the Composer, not UploadManager, owns selection.
const restorableUploads = computed(() => visibleUploads.value.filter((item) => item.status === 'COMPLETED' && !selectedUploadClients.value.includes(item.clientId)))
// Request identity describes the actual CreateMessage payload (completed
// uploadIds included). Draft identity additionally covers the selection of
// still-pending uploads via stable client IDs, so a newer draft is never
// mistaken for the submitted one — and it never changes with mere progress.
const draftFields = () => [body.value, mode.value, lifecycle.value, sensitive.value, [...selectedTags.value].sort().join(',')]
const requestFingerprint = computed(() => JSON.stringify({ draft: draftFields(), uploadIds: selectedUploadIds.value }))
const draftIdentity = computed(() => JSON.stringify({ draft: draftFields(), selection: [...selectedUploadClients.value] }))
let attemptedIdentity = ''
let attemptedFingerprint = ''

interface SendSnapshot {
  key: string
  fingerprint: string
  identity: string
  payload: CreateMessageRequest
}

const send = useMutation({
  retry: false,
  mutationFn: ({ key, payload }: SendSnapshot) => DefaultService.createMessage(key, payload),
  onSuccess: (_result, snapshot) => {
    // Decide whether the live draft is still the submitted draft BEFORE
    // retiring consumed uploads; retirement changes requestFingerprint and
    // must not make an unchanged draft look edited.
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
    emit('sent')
  },
  onError: (cause, snapshot) => {
    failed.value = true
    error.value = displayError(cause)
    if (requestFingerprint.value !== snapshot.fingerprint || draftIdentity.value !== snapshot.identity) activeKey.value = ''
  },
})

watch([requestFingerprint, draftIdentity], ([fingerprint, identity]) => {
  if (failed.value && (fingerprint !== attemptedFingerprint || identity !== attemptedIdentity)) {
    activeKey.value = ''
    failed.value = false
    error.value = ''
  }
})

function submit() {
  if (send.isPending.value) return
  error.value = ''
  if (!hasContent.value) { error.value = '请输入正文或添加至少一个附件。'; return }
  if (attachmentsBlocking.value) { error.value = '等待附件上传完成，或移除失败的附件。'; return }
  if (tooLarge.value) { error.value = '正文 UTF-8 大小不能超过 1 MiB。'; return }
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
async function selectFiles(files: FileList | File[]) {
  const ids = await uploadManager.addFiles(Array.from(files))
  selectedUploadClients.value.push(...ids)
}
function filesChanged(event: Event) {
  const input = event.target as HTMLInputElement
  if (input.files) void selectFiles(input.files)
  input.value = ''
}
function dropFiles(event: DragEvent) {
  dragging.value = false
  if (event.dataTransfer?.files.length) void selectFiles(event.dataTransfer.files)
}
function pasteFiles(event: ClipboardEvent) {
  const images = Array.from(event.clipboardData?.items ?? []).filter((item) => item.kind === 'file' && item.type.startsWith('image/'))
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
function removeSelected(clientId: string) {
  const item = uploadManager.getItem(clientId)
  selectedUploadClients.value = selectedUploadClients.value.filter((id) => id !== clientId)
  if (item && item.status !== 'COMPLETED') uploadManager.remove(clientId)
}
function addRestored(clientId: string) {
  if (!selectedUploadClients.value.includes(clientId)) selectedUploadClients.value.push(clientId)
}
function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Enter' && (event.ctrlKey || event.metaKey)) { event.preventDefault(); submit() }
}
async function addTag() {
  const name = newTagName.value.trim()
  if (!name) return
  try {
    const tag = await createTag.mutateAsync({ name, color: newTagColor.value })
    selectedTags.value.push(tag.id)
    newTagName.value = ''
  } catch (cause) { error.value = displayError(cause) }
}
</script>

<template>
  <section
    class="composer panel"
    aria-labelledby="composer-title"
  >
    <header>
      <h2 id="composer-title">
        新内容
      </h2><div
        class="modes"
        role="group"
        aria-label="正文格式"
      >
        <button
          v-for="item in (['text','markdown','code'] as const)"
          :key="item"
          type="button"
          :class="{ active: mode === item }"
          @click="mode = item"
        >
          {{ item === 'text' ? 'Text' : item === 'markdown' ? 'Markdown' : 'Code' }}
        </button>
      </div>
    </header>
    <label
      class="sr-only"
      for="composer-body"
    >正文</label>
    <textarea
      id="composer-body"
      v-model="body"
      rows="5"
      :class="{ code: mode === 'code' }"
      placeholder="写下要在其他设备取回的内容…"
      @keydown="onKeydown"
      @paste="pasteFiles"
    />
    <section
      class="drop-zone"
      :class="{ dragging }"
      @dragenter.prevent="dragging = true"
      @dragover.prevent="dragging = true"
      @dragleave.prevent="dragging = false"
      @drop.prevent="dropFiles"
    >
      <input
        ref="fileInput"
        class="sr-only"
        type="file"
        multiple
        @change="filesChanged"
      >
      <button
        class="button attach"
        type="button"
        @click="fileInput?.click()"
      >
        添加文件或照片
      </button>
      <span>也可拖放文件，或直接粘贴图片</span>
    </section>
    <ul
      v-if="selectedUploads.length"
      class="selected-files"
      aria-label="本条内容的附件"
    >
      <li
        v-for="item in selectedUploads"
        :key="item.clientId"
      >
        <div><strong>{{ item.filename }}</strong><small>{{ formatBytes(item.size) }} · {{ item.status }}<template v-if="item.status === 'UPLOADING'"> · {{ Math.round(item.progress * 100) }}%</template></small></div>
        <button
          class="button"
          type="button"
          @click="removeSelected(item.clientId)"
        >
          移除
        </button>
      </li>
    </ul>
    <p
      v-if="attachmentsBlocking"
      class="warning"
    >
      等待附件上传完成。失败的附件需要重试或移除后才能发送。
    </p>
    <section
      v-if="restorableUploads.length"
      class="restored"
      aria-label="已完成的上传"
    >
      <h3>已完成的上传</h3>
      <ul>
        <li
          v-for="item in restorableUploads"
          :key="item.clientId"
        >
          <span>{{ item.filename }} · {{ formatBytes(item.size) }}</span>
          <button
            class="button"
            type="button"
            @click="addRestored(item.clientId)"
          >
            添加到本条
          </button>
        </li>
      </ul>
    </section>
    <div class="options">
      <label class="field compact">保存位置<select v-model="lifecycle"><option :value="Lifecycle.TEMPORARY">Temporary</option><option :value="Lifecycle.PERMANENT">Permanent</option></select></label>
      <label class="toggle"><input
        v-model="sensitive"
        type="checkbox"
      > Sensitive</label>
      <span
        class="bytes"
        :class="{ error: tooLarge }"
      >{{ byteLength.toLocaleString() }} / 1,048,576 bytes</span>
    </div>
    <fieldset>
      <legend>标签</legend><div class="tag-options">
        <label
          v-for="tag in tags.data.value"
          :key="tag.id"
        ><input
          v-model="selectedTags"
          type="checkbox"
          :value="tag.id"
        ><i :style="{ backgroundColor: tag.color }" />{{ tag.name }}</label>
      </div><div class="new-tag">
        <input
          v-model="newTagName"
          class="input"
          maxlength="64"
          placeholder="创建新标签"
        ><input
          v-model="newTagColor"
          type="color"
          aria-label="标签颜色"
        ><button
          class="button"
          type="button"
          @click="addTag"
        >
          添加
        </button>
      </div>
    </fieldset>
    <p
      v-if="lifecycle === Lifecycle.PERMANENT && !selectedTags.length"
      class="warning"
    >
      长期内容尚未添加标签（仍可发送）。
    </p>
    <p
      v-if="tooLarge"
      class="error"
      role="alert"
    >
      正文 UTF-8 大小不能超过 1 MiB。
    </p>
    <p
      v-if="error"
      class="error"
      role="alert"
    >
      {{ error }}
    </p>
    <footer>
      <span class="muted">Ctrl/⌘ + Enter 发送</span><button
        class="button primary"
        type="button"
        :disabled="send.isPending.value || !hasContent || attachmentsBlocking || tooLarge"
        @click="submit"
      >
        {{ send.isPending.value ? '发送中…' : failed ? '重试发送' : '发送' }}
      </button>
    </footer>
  </section>
</template>

<style scoped>
.composer { padding:1rem; display:grid; gap:.8rem; } header,footer,.options { display:flex; align-items:center; justify-content:space-between; gap:.75rem; } h2 { margin:0; font-size:1rem; } textarea { resize:vertical; width:100%; min-height:120px; border:1px solid var(--border); border-radius:var(--radius-sm); padding:.8rem; background:var(--surface-raised); line-height:1.5; }.code{font-family:var(--font-mono)}.modes{display:flex;background:var(--surface-soft);border-radius:999px;padding:.2rem}.modes button{border:0;background:transparent;border-radius:999px;padding:.35rem .65rem}.modes .active{background:var(--surface-raised);box-shadow:0 1px 4px rgb(0 0 0 / .12)}.compact{display:flex;align-items:center;grid-template-columns:auto auto}.compact select{width:auto}.toggle{display:flex;gap:.4rem;align-items:center}.bytes{margin-left:auto;color:var(--muted);font-size:.75rem}fieldset{border:1px solid var(--border);border-radius:var(--radius-sm);padding:.65rem}.tag-options{display:flex;flex-wrap:wrap;gap:.5rem}.tag-options label{display:flex;align-items:center;gap:.25rem}.tag-options i{width:.55rem;height:.55rem;border-radius:50%}.new-tag{display:grid;grid-template-columns:1fr auto auto;gap:.4rem;margin-top:.65rem}.new-tag input[type=color]{width:44px;height:42px;border:1px solid var(--border);border-radius:var(--radius-sm);background:transparent}.warning{margin:0;color:#9a6414}.error{margin:0}.muted{font-size:.8rem}
.drop-zone{display:flex;align-items:center;gap:.65rem;padding:.65rem .75rem;border:1px dashed var(--border);border-radius:var(--radius-sm);color:var(--muted);font-size:.8rem;transition:border-color .12s,background .12s}.drop-zone.dragging{border-color:var(--accent);background:var(--accent-soft)}.attach{color:var(--accent-strong)}.selected-files{list-style:none;margin:0;padding:0;display:grid;gap:.4rem}.selected-files li{display:flex;align-items:center;justify-content:space-between;gap:.7rem;padding:.55rem .7rem;background:var(--surface-soft);border-radius:var(--radius-sm)}.selected-files div{min-width:0;display:grid}.selected-files strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.selected-files small{color:var(--muted)}
.restored{border:1px solid var(--border);border-radius:var(--radius-sm);padding:.55rem .65rem}.restored h3{margin:.1rem 0 .4rem;font-size:.78rem;color:var(--muted)}.restored ul{list-style:none;margin:0;padding:0;display:grid;gap:.35rem}.restored li{display:flex;align-items:center;justify-content:space-between;gap:.7rem;font-size:.82rem}.restored span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.restored .button{min-height:32px;padding:.25rem .55rem;font-size:.76rem;white-space:nowrap}
@media(max-width:600px){.options{align-items:flex-start;flex-wrap:wrap}.bytes{width:100%;margin:0}.new-tag{grid-template-columns:1fr auto}.new-tag .button{grid-column:1/-1}footer .muted{display:none}}
</style>
