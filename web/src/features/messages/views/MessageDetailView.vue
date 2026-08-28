<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { BodyFormat, DefaultService } from '@/api/generated'
import { displayError } from '@/shared/api/errors'
import TagChip from '@/shared/ui/TagChip.vue'
import { useTagsQuery } from '@/features/tags/queries'
import { mutationErrorMessage, useMessageMutation } from '../mutations'
import { useMessageDetail } from '../queries'

const props = defineProps<{ id: string }>()
const route = useRoute()
const router = useRouter()
const detail = useMessageDetail(() => props.id)
const mutation = useMessageMutation()
const tags = useTagsQuery()
interface RevealedBody {
  messageId: string
  version: number
  body: string
}
const revealedBody = ref<RevealedBody | null>(null)
const revealPending = ref(false)
let revealRequest = 0
const editing = ref(false)
const editBody = ref('')
const editFormat = ref(BodyFormat.TEXT)
const selectedTags = ref<string[]>([])
const error = ref('')
const message = computed(() => detail.data.value)
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
  editBody.value = value.body ?? ''
  editFormat.value = value.bodyFormat
  selectedTags.value = value.tags.map((tag) => tag.id)
}, { immediate: true })
watch(
  () => [message.value?.id, message.value?.version, message.value?.sensitive] as const,
  () => { clearRevealedBody(); editBody.value = ''; editing.value = false },
)
onUnmounted(clearRevealedBody)
function close() {
  clearRevealedBody()
  const from = typeof route.query.from === 'string' && route.query.from.startsWith('/') ? route.query.from : ''
  if (from) void router.push(from); else void router.replace('/temporary')
}
function onKey(event: KeyboardEvent) { if (event.key === 'Escape') close() }
onMounted(() => document.addEventListener('keydown', onKey))
onUnmounted(() => document.removeEventListener('keydown', onKey))
async function reveal() {
  const current = message.value
  if (!current?.sensitive) return
  const requestedMessageId = current.id
  const requestedVersion = current.version
  const request = ++revealRequest
  revealPending.value = true; error.value = ''
  try {
    const response = await DefaultService.revealSensitiveBody(requestedMessageId)
    const latest = message.value
    if (
      request === revealRequest
      && props.id === requestedMessageId
      && latest?.id === requestedMessageId
      && latest.version === requestedVersion
      && response.version === latest.version
      && latest.sensitive
    ) {
      revealedBody.value = { messageId:requestedMessageId, version:response.version, body:response.body }
    }
  }
  catch (cause) { if (request === revealRequest) error.value = displayError(cause) }
  finally { if (request === revealRequest) revealPending.value = false }
}
async function copy() {
  if (!message.value) return
  if (message.value.sensitive && currentSensitiveBody.value === null) await reveal()
  const value = message.value.sensitive ? currentSensitiveBody.value : message.value.body
  if (value !== null) await navigator.clipboard.writeText(value ?? '')
}
function run(command: Parameters<typeof mutation.mutate>[0], success?: () => void) {
  error.value = ''
  mutation.mutate(command, {
    onSuccess: () => { clearRevealedBody(); success?.() },
    onError: (cause) => { error.value = mutationErrorMessage(cause) },
  })
}
async function startEdit() {
  if (!message.value) return
  if (message.value.sensitive) {
    if (currentSensitiveBody.value === null) await reveal()
    if (currentSensitiveBody.value === null) return
    editBody.value = currentSensitiveBody.value
  } else editBody.value = message.value.body ?? ''
  editing.value = true
}
function saveBody() {
  if (!message.value || !editBody.value.trim()) return
  if (message.value.sensitive && currentSensitiveBody.value === null) {
    editing.value = false
    error.value = '内容版本已更新，请重新显示正文后再编辑。'
    return
  }
  const command = message.value.sensitive
    ? { type: 'editSensitive' as const, message: message.value, body: editBody.value }
    : { type: 'edit' as const, message: message.value, body: editBody.value, bodyFormat: editFormat.value }
  run(command, () => { editing.value = false })
}
function removeForever() { if (message.value && window.confirm('永久删除后无法恢复。确定继续吗？')) run({ type:'delete', message:message.value }, close) }
function downloadUrl(id: string) { return `/api/v1/attachments/${encodeURIComponent(id)}/download` }
</script>

<template>
  <Teleport to="body">
    <div
      class="detail-backdrop"
      @click.self="close"
    >
      <article
        class="detail panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="detail-title"
      >
        <header>
          <div>
            <h1 id="detail-title">
              内容详情
            </h1><p
              v-if="message"
              class="muted"
            >
              {{ new Date(message.createdAt).toLocaleString() }} · v{{ message.version }}
            </p>
          </div><button
            class="button close"
            aria-label="关闭详情"
            @click="close"
          >
            ×
          </button>
        </header>
        <div
          v-if="detail.isPending.value"
          class="state"
        >
          正在加载…
        </div>
        <div
          v-else-if="detail.isError.value"
          class="state error"
        >
          详情加载失败。<button
            class="button"
            @click="detail.refetch()"
          >
            重试
          </button>
        </div>
        <template v-else-if="message">
          <section
            v-if="message.sensitive"
            class="sensitive panel"
          >
            <template v-if="currentSensitiveBody === null">
              <strong>🔒 Sensitive locked</strong><p class="muted">
                明文仅在本详情页内存中短暂保留。
              </p><button
                class="button primary"
                :disabled="revealPending"
                @click="reveal"
              >
                {{ revealPending ? '读取中…' : '显示正文' }}
              </button>
            </template>
            <template v-else>
              <pre>{{ currentSensitiveBody }}</pre><button
                class="button"
                @click="clearRevealedBody"
              >
                隐藏
              </button>
            </template>
          </section>
          <pre
            v-else-if="!editing"
            :class="{ code: message.detectedType === 'CODE' || Boolean(message.detectedLanguage) }"
          >{{ message.body }}</pre>
          <form
            v-if="editing"
            class="edit"
            @submit.prevent="saveBody"
          >
            <label class="field">格式<select
              v-model="editFormat"
              :disabled="message.sensitive"
            ><option :value="BodyFormat.TEXT">Text</option><option :value="BodyFormat.MARKDOWN">Markdown</option></select></label><label class="field">正文<textarea
              v-model="editBody"
              rows="12"
              required
            /></label><div>
              <button
                class="button"
                type="button"
                @click="editing = false"
              >
                取消
              </button> <button class="button primary">
                保存
              </button>
            </div>
          </form>
          <div class="tags">
            <TagChip
              v-for="tag in message.tags"
              :key="tag.id"
              :name="tag.name"
              :color="tag.color"
            />
          </div>
          <details v-if="!message.trashedAt">
            <summary>修改标签</summary><div class="tag-picker">
              <label
                v-for="tag in tags.data.value"
                :key="tag.id"
              ><input
                v-model="selectedTags"
                type="checkbox"
                :value="tag.id"
              > {{ tag.name }}</label>
            </div><button
              class="button"
              @click="run({ type:'tags', message, tagIds:selectedTags })"
            >
              保存标签
            </button>
          </details>
          <section
            v-if="message.attachments.length"
            class="files"
          >
            <h2>附件元数据</h2><a
              v-for="file in message.attachments"
              :key="file.id"
              :href="downloadUrl(file.id)"
            ><strong>{{ file.originalFilename }}</strong><small>{{ file.detectedMime }} · {{ file.sizeBytes.toLocaleString() }} bytes · 下载</small></a>
          </section>
          <div class="actions">
            <template v-if="message.trashedAt">
              <button
                class="button"
                @click="run({ type:'restore', message })"
              >
                恢复
              </button><button
                class="button danger"
                @click="removeForever"
              >
                永久删除
              </button>
            </template>
            <template v-else>
              <button
                class="button"
                @click="copy"
              >
                复制正文
              </button><button
                class="button"
                @click="startEdit"
              >
                编辑
              </button><button
                class="button"
                @click="run({ type:'sensitive', message, sensitive:!message.sensitive })"
              >
                {{ message.sensitive ? '转为普通' : '设为 Sensitive' }}
              </button><button
                v-if="message.lifecycle === 'TEMPORARY'"
                class="button"
                @click="run({ type:'permanent', message })"
              >
                转为长期
              </button><button
                v-else
                class="button"
                @click="run({ type:'favorite', message, favorite:!message.favorite })"
              >
                {{ message.favorite ? '取消收藏' : '收藏' }}
              </button><button
                class="button danger"
                @click="run({ type:'trash', message })"
              >
                移到回收站
              </button>
            </template>
          </div>
          <p
            v-if="error"
            class="error"
            role="alert"
          >
            {{ error }}
          </p>
        </template>
      </article>
    </div>
  </Teleport>
</template>

<style scoped>
.detail-backdrop{position:fixed;inset:0;z-index:45;background:rgb(0 0 0 / .42);display:flex;justify-content:flex-end}.detail{height:100%;width:min(760px,82vw);overflow:auto;border-radius:var(--radius-lg) 0 0 var(--radius-lg);padding:1.3rem;display:grid;align-content:start;gap:1rem;box-shadow:var(--shadow)}header{display:flex;justify-content:space-between;gap:1rem;align-items:flex-start}h1,h2,p{margin:.2rem 0}h1{font-size:1.35rem}.close{font-size:1.3rem}pre{margin:0;white-space:pre-wrap;overflow-wrap:anywhere;line-height:1.6}.code{font-family:var(--font-mono);background:var(--surface-soft);padding:1rem;border-radius:var(--radius)}.sensitive{padding:1.25rem;box-shadow:none}.edit{display:grid;gap:.75rem}.edit textarea{resize:vertical}.tags,.actions,.tag-picker{display:flex;flex-wrap:wrap;gap:.5rem}.files{display:grid;gap:.5rem}.files a{display:flex;justify-content:space-between;gap:1rem;padding:.75rem;border:1px solid var(--border);border-radius:var(--radius-sm);text-decoration:none}.files small{color:var(--muted)}details{display:grid;gap:.6rem}summary{cursor:pointer}.state{padding:3rem;text-align:center}
@media(prefers-reduced-motion:no-preference){.detail{animation:slide .16s ease-out}@keyframes slide{from{transform:translateX(25px);opacity:.8}}}
@media(max-width:720px){.detail-backdrop{display:block;background:var(--surface)}.detail{width:100%;height:100%;border:0;border-radius:0;padding:1rem}.files a{display:grid}}
</style>
