<script setup lang="ts">
import { LockKeyhole, Star } from '@lucide/vue'
import { computed, ref } from 'vue'
import { BodyFormat, type MessageSummary } from '@/api/generated'
import { useDetailSelection } from '@/app/composables/useDetailSelection'
import TagChip from '@/shared/ui/TagChip.vue'
import { extractFenceLanguage, unwrapSingleFencedCode } from '../content/contentFormat'
import { mutationErrorMessage, useMessageMutation } from '../mutations'
import AttachmentGrid from './attachments/AttachmentGrid.vue'
import LinkifiedText from './LinkifiedText.vue'
import QuickCopyButton from './QuickCopyButton.vue'
import SafeMarkdown from './SafeMarkdown.vue'

const props = defineProps<{ message: MessageSummary; trash?: boolean }>()
const { selectedMessageId, openDetail: openSelectedDetail } = useDetailSelection()
const mutation = useMessageMutation()
const error = ref('')
const selected = computed(() => selectedMessageId.value === props.message.id)
const hasBody = computed(() => props.message.sensitive || Boolean(props.message.bodyPreview))
const requiresDetailToCopy = computed(() => props.message.sensitive || props.message.bodyTruncated)
// Legacy TEXT messages flagged CODE by the server keep their <pre> fallback;
// fenced-language detection takes precedence for MARKDOWN bodies.
const fencedLanguage = computed(() => props.message.bodyFormat === BodyFormat.MARKDOWN
  ? extractFenceLanguage(props.message.bodyPreview ?? '')
  : null)
const isLegacyCode = computed(() => props.message.bodyFormat !== BodyFormat.MARKDOWN
  && (props.message.detectedType === 'CODE' || Boolean(props.message.detectedLanguage)))
const isCode = computed(() => Boolean(fencedLanguage.value) || isLegacyCode.value)
const isMarkdownView = computed(() => props.message.bodyFormat === BodyFormat.MARKDOWN
  && !props.message.sensitive && Boolean(props.message.bodyPreview))
const isLegacyCodeView = computed(() => isLegacyCode.value
  && !props.message.sensitive && Boolean(props.message.bodyPreview))
const typeLabel = computed(() => {
  if (props.message.sensitive) return '敏感'
  if (fencedLanguage.value) return fencedLanguage.value.shortLabel
  if (isLegacyCode.value) return props.message.detectedLanguage?.toUpperCase() || 'CODE'
  if (props.message.attachments.length && !props.message.bodyPreview) return '文件'
  if (props.message.bodyFormat === 'MARKDOWN') return 'MD'
  return '文本'
})
// Copying a body that is exactly one fenced code block yields the bare code —
// the cross-device "paste this command" contract. Prose Markdown keeps its
// full text.
const copyBody = computed(() => {
  const preview = props.message.bodyPreview ?? ''
  if (requiresDetailToCopy.value) return preview
  return unwrapSingleFencedCode(preview) ?? preview
})

function openDetail() {
  openSelectedDetail(props.message.id)
}
// Embedded links and other interactive elements must not also open the detail.
function onCardClick(event: MouseEvent) {
  const target = event.target
  if (target instanceof HTMLElement && target.closest('a, button, input, select, textarea, summary, label')) return
  openDetail()
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
  <article
    class="message-card panel"
    :class="{ selected, 'code-card': isCode }"
    :aria-current="selected ? 'true' : undefined"
    @click="onCardClick"
  >
    <header class="card-header">
      <div class="headline">
        <span class="type-badge">{{ typeLabel }}</span>
        <span
          v-if="message.lifecycle === 'TEMPORARY' && message.expiresAt"
          class="expiry-badge"
        >{{ relativeExpiry(message.expiresAt) }}</span>
        <span
          v-else-if="message.favorite"
          class="favorite-badge"
        ><Star aria-hidden="true" />收藏</span>
      </div>
      <QuickCopyButton
        v-if="hasBody"
        :body="copyBody"
        :requires-detail="requiresDetailToCopy"
        @open-detail="openDetail"
      />
    </header>

    <div class="body-region">
      <span
        v-if="message.sensitive"
        class="locked"
      ><strong><LockKeyhole aria-hidden="true" />敏感内容已锁定</strong><small>正文已保护，打开详情后可验证查看</small></span>
      <div
        v-else-if="message.bodyPreview"
        class="body-content"
      >
        <SafeMarkdown
          v-if="isMarkdownView"
          class="feed-markdown"
          :source="message.bodyPreview"
        />
        <pre
          v-else-if="isLegacyCodeView"
          class="code"
        >{{ message.bodyPreview }}</pre>
        <LinkifiedText
          v-else
          :text="message.bodyPreview"
        />
      </div>
      <span
        v-else
        class="attachment-only"
      >仅附件内容</span>
      <small
        v-if="message.bodyTruncated"
        class="truncated"
      >预览已截断 · 打开详情查看完整内容</small>
      <!-- Transparent cover keeps "click body opens detail" without nesting
           interactive elements: links sit above it and keep their own clicks. -->
      <button
        class="body-button"
        type="button"
        :aria-label="`打开内容 ${message.id}`"
        @click.stop="openDetail"
      />
    </div>

    <AttachmentGrid
      v-if="message.attachments.length"
      :files="message.attachments"
      :limit="3"
      :total="message.attachmentCount"
    />

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

    <footer>
      <div class="meta">
        <span v-if="message.sourceMessageId">转发内容</span>
        <time :datetime="message.createdAt">{{ new Date(message.createdAt).toLocaleString() }}</time>
        <span v-if="message.attachmentCount">{{ message.attachmentCount }} 个附件</span>
      </div>
      <div
        class="actions"
        @click.stop
      >
        <template v-if="trash">
          <button
            class="button"
            @click="run({ type: 'restore', message })"
          >
            恢复
          </button>
          <button
            class="button danger"
            @click="removeForever"
          >
            永久删除
          </button>
        </template>
        <template v-else>
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
.message-card{position:relative;display:grid;gap:.65rem;padding:.82rem .9rem;cursor:pointer;transition:border-color .15s ease,background .15s ease,box-shadow .15s ease}.message-card:hover{border-color:var(--border-strong);box-shadow:var(--shadow-md)}.message-card.selected{border-color:var(--accent-primary);background:color-mix(in srgb,var(--accent-primary-soft) 44%,var(--surface-raised));box-shadow:inset 3px 0 var(--accent-primary),var(--shadow-sm)}
.card-header,.headline,.tags,.actions,.meta{display:flex;flex-wrap:wrap;align-items:center}.card-header{justify-content:space-between;gap:.6rem}.headline,.tags,.actions,.meta{gap:.38rem}.type-badge,.expiry-badge,.favorite-badge{display:inline-flex;align-items:center;gap:.25rem;min-height:22px;border-radius:999px;padding:.16rem .46rem;font-size:.65rem;font-weight:750;letter-spacing:.035em}.favorite-badge svg{width:.72rem;height:.72rem}.type-badge{background:var(--surface-soft);color:var(--text-secondary)}.code-card .type-badge{background:color-mix(in srgb,var(--content-code) 13%,var(--surface-soft));color:var(--content-code)}.expiry-badge{background:color-mix(in srgb,var(--state-warning) 12%,var(--surface-soft));color:var(--state-warning)}.favorite-badge{background:var(--accent-primary-soft);color:var(--accent-primary)}
.body-region{position:relative;display:grid;gap:.38rem}
.body-button{position:absolute;inset:0;z-index:1;border:0;padding:0;background:transparent;cursor:pointer;border-radius:var(--radius-sm)}.body-button:focus-visible{outline:2px solid var(--focus-ring);outline-offset:3px}
.body-region :deep(a){position:relative;z-index:2}
.body-content pre{max-height:10.5rem;margin:0;overflow:hidden;overflow-wrap:anywhere;white-space:pre-wrap;font:inherit;line-height:1.5}.code{border-left:3px solid var(--content-code);border-radius:var(--radius-sm);padding:.7rem .75rem;background:color-mix(in srgb,var(--content-code) 8%,var(--surface-soft));font-family:var(--font-mono);font-size:.82rem}
.feed-markdown{max-height:11rem;overflow:hidden}
.locked{display:grid;gap:.18rem;border-radius:var(--radius-sm);padding:.7rem .75rem;background:var(--surface-soft);color:var(--text-secondary)}.locked strong{display:flex;align-items:center;gap:.35rem;color:var(--text-primary);font-size:.85rem}.locked strong svg{width:.9rem;height:.9rem}.locked small,.truncated{color:var(--text-tertiary);font-size:.7rem}.attachment-only{color:var(--text-tertiary);font-size:.8rem}.truncated{display:block}
footer{display:flex;justify-content:space-between;align-items:flex-end;gap:.7rem;border-top:1px solid var(--border-default);padding-top:.62rem}.meta{color:var(--text-tertiary);font-size:.7rem}.actions{justify-content:flex-end}.button{min-height:30px;padding:.28rem .52rem;font-size:.75rem;box-shadow:none}.error{margin:0;font-size:.78rem}
@media(max-width:600px){.message-card{padding:.78rem}.card-header{align-items:flex-start}footer{display:grid}.actions{justify-content:flex-start}.button{min-height:40px}}
@media(prefers-reduced-motion:reduce){.message-card{transition:none}}
</style>
