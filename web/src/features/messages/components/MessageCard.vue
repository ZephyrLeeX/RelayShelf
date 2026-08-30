<script setup lang="ts">
import { ref } from 'vue'
import type { MessageSummary } from '@/api/generated'
import { useDetailSelection } from '@/app/composables/useDetailSelection'
import TagChip from '@/shared/ui/TagChip.vue'
import { mutationErrorMessage, useMessageMutation } from '../mutations'
import AttachmentList from './AttachmentList.vue'

const props = defineProps<{ message: MessageSummary; trash?: boolean }>()
const { openDetail: openSelectedDetail } = useDetailSelection()
const mutation = useMessageMutation()
const error = ref('')

function openDetail() {
  openSelectedDetail(props.message.id)
}
async function copyBody() {
  if (props.message.sensitive || props.message.bodyTruncated) return openDetail()
  await navigator.clipboard.writeText(props.message.bodyPreview ?? '')
}
function run(command: Parameters<typeof mutation.mutate>[0]) {
  error.value = ''
  mutation.mutate(command, { onError: (cause) => { error.value = mutationErrorMessage(cause) } })
}
function removeForever() {
  if (window.confirm('永久删除后无法恢复。确定继续吗？')) run({ type: 'delete', message: props.message })
}
function relativeExpiry(value?: string | null) {
  if (!value) return ''
  const hours = Math.ceil((new Date(value).getTime() - Date.now()) / 3_600_000)
  if (hours <= 24) return '今天过期'
  if (hours <= 48) return '明天过期'
  return `剩余 ${Math.ceil(hours / 24)} 天`
}
</script>

<template>
  <article class="message-card panel">
    <button
      class="body-button"
      type="button"
      :aria-label="`打开内容 ${message.id}`"
      @click="openDetail"
    >
      <span
        v-if="message.sensitive"
        class="locked"
      >🔒 Sensitive</span>
      <pre
        v-else-if="message.bodyPreview"
        :class="{ code: message.detectedType === 'CODE' || Boolean(message.detectedLanguage) }"
      >{{ message.bodyPreview }}</pre>
      <span
        v-else
        class="muted"
      >仅附件内容</span>
      <small
        v-if="message.bodyTruncated"
        class="muted"
      >预览已截断，打开详情查看完整内容</small>
    </button>
    <div
      v-if="message.tags.length"
      class="tags"
    >
      <TagChip
        v-for="tag in message.tags"
        :key="tag.id"
        :name="tag.name"
        :color="tag.color"
      />
    </div>
    <AttachmentList
      v-if="message.attachments.length"
      :files="message.attachments"
      :limit="3"
      :total="message.attachmentCount"
    />
    <footer>
      <div class="meta">
        <time :datetime="message.createdAt">{{ new Date(message.createdAt).toLocaleString() }}</time><span v-if="message.lifecycle === 'TEMPORARY'">{{ relativeExpiry(message.expiresAt) }}</span><span v-if="message.attachmentCount">{{ message.attachmentCount }} 个附件</span>
      </div>
      <div class="actions">
        <template v-if="trash">
          <button
            class="button"
            @click="run({ type: 'restore', message })"
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
            @click="copyBody"
          >
            {{ message.bodyTruncated ? '打开并复制' : '复制' }}
          </button>
          <button
            v-if="message.lifecycle === 'TEMPORARY'"
            class="button"
            @click="run({ type: 'permanent', message })"
          >
            转为长期
          </button>
          <button
            v-else
            class="button"
            @click="run({ type: 'favorite', message, favorite: !message.favorite })"
          >
            {{ message.favorite ? '取消收藏' : '收藏' }}
          </button>
          <button
            class="button danger"
            @click="run({ type: 'trash', message })"
          >
            删除
          </button>
        </template>
      </div>
    </footer>
    <p
      v-if="error"
      class="error"
      role="alert"
    >
      {{ error }}
    </p>
  </article>
</template>

<style scoped>
.message-card { padding:1rem 1.1rem; display:grid; gap:.8rem; }.body-button { display:grid; gap:.45rem; width:100%; border:0; padding:0; background:transparent; text-align:left; }.locked { font-weight:700; color:var(--muted); } pre { margin:0; white-space:pre-wrap; overflow-wrap:anywhere; font:inherit; line-height:1.55; max-height:17rem; overflow:hidden; }.code { font-family:var(--font-mono); font-size:.9rem; background:var(--surface-soft); border-radius:var(--radius-sm); padding:.75rem; }.tags,.actions,.meta { display:flex; flex-wrap:wrap; gap:.4rem; align-items:center; } footer { display:flex; justify-content:space-between; align-items:flex-end; gap:.75rem; border-top:1px solid var(--border); padding-top:.75rem; }.meta { color:var(--muted); font-size:.76rem; }.button { min-height:34px; padding:.35rem .6rem; font-size:.82rem; }
@media(max-width:600px){footer{display:grid}.actions{justify-content:flex-end}.button{min-height:42px}}
</style>
