<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useMutation, useQueryClient } from '@tanstack/vue-query'
import { BodyFormat, DefaultService, Lifecycle, type CreateMessageRequest } from '@/api/generated'
import { displayError } from '@/shared/api/errors'
import { queryKeys } from '@/shared/api/queryKeys'
import { useCreateTag, useTagsQuery } from '@/features/tags/queries'

type ComposerMode = 'text' | 'markdown' | 'code'
const props = defineProps<{ defaultLifecycle: Lifecycle }>()
const emit = defineEmits<{ sent: [] }>()
const body = ref('')
const mode = ref<ComposerMode>('text')
const lifecycle = ref(props.defaultLifecycle)
const sensitive = ref(false)
const selectedTags = ref<string[]>([])
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
const empty = computed(() => !body.value.trim())
const bodyFormat = computed(() => mode.value === 'markdown' ? BodyFormat.MARKDOWN : BodyFormat.TEXT)
const fingerprint = computed(() => JSON.stringify({ body: body.value, mode: mode.value, lifecycle: lifecycle.value, sensitive: sensitive.value, tags: [...selectedTags.value].sort() }))
let attemptedFingerprint = ''

interface SendSnapshot {
  key: string
  fingerprint: string
  payload: CreateMessageRequest
}

const send = useMutation({
  retry: false,
  mutationFn: ({ key, payload }: SendSnapshot) => DefaultService.createMessage(key, payload),
  onSuccess: () => {
    body.value = ''
    selectedTags.value = []
    sensitive.value = false
    activeKey.value = ''
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
    if (fingerprint.value !== snapshot.fingerprint) activeKey.value = ''
  },
})

watch(fingerprint, (value) => {
  if (failed.value && value !== attemptedFingerprint) {
    activeKey.value = ''
    failed.value = false
    error.value = ''
  }
})

function submit() {
  error.value = ''
  if (empty.value) { error.value = '正文不能为空。'; return }
  if (tooLarge.value) { error.value = '正文 UTF-8 大小不能超过 1 MiB。'; return }
  if (!activeKey.value) activeKey.value = crypto.randomUUID()
  attemptedFingerprint = fingerprint.value
  send.mutate({
    key: activeKey.value,
    fingerprint: attemptedFingerprint,
    payload: {
      body: body.value,
      bodyFormat: bodyFormat.value,
      lifecycle: lifecycle.value,
      sensitive: sensitive.value,
      tagIds: [...selectedTags.value],
      uploadIds: [],
    },
  })
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
    />
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
        :disabled="send.isPending.value || empty || tooLarge"
        @click="submit"
      >
        {{ send.isPending.value ? '发送中…' : failed ? '重试发送' : '发送' }}
      </button>
    </footer>
  </section>
</template>

<style scoped>
.composer { padding:1rem; display:grid; gap:.8rem; } header,footer,.options { display:flex; align-items:center; justify-content:space-between; gap:.75rem; } h2 { margin:0; font-size:1rem; } textarea { resize:vertical; width:100%; min-height:120px; border:1px solid var(--border); border-radius:var(--radius-sm); padding:.8rem; background:var(--surface-raised); line-height:1.5; }.code{font-family:var(--font-mono)}.modes{display:flex;background:var(--surface-soft);border-radius:999px;padding:.2rem}.modes button{border:0;background:transparent;border-radius:999px;padding:.35rem .65rem}.modes .active{background:var(--surface-raised);box-shadow:0 1px 4px rgb(0 0 0 / .12)}.compact{display:flex;align-items:center;grid-template-columns:auto auto}.compact select{width:auto}.toggle{display:flex;gap:.4rem;align-items:center}.bytes{margin-left:auto;color:var(--muted);font-size:.75rem}fieldset{border:1px solid var(--border);border-radius:var(--radius-sm);padding:.65rem}.tag-options{display:flex;flex-wrap:wrap;gap:.5rem}.tag-options label{display:flex;align-items:center;gap:.25rem}.tag-options i{width:.55rem;height:.55rem;border-radius:50%}.new-tag{display:grid;grid-template-columns:1fr auto auto;gap:.4rem;margin-top:.65rem}.new-tag input[type=color]{width:44px;height:42px;border:1px solid var(--border);border-radius:var(--radius-sm);background:transparent}.warning{margin:0;color:#9a6414}.error{margin:0}.muted{font-size:.8rem}
@media(max-width:600px){.options{align-items:flex-start;flex-wrap:wrap}.bytes{width:100%;margin:0}.new-tag{grid-template-columns:1fr auto}.new-tag .button{grid-column:1/-1}footer .muted{display:none}}
</style>
