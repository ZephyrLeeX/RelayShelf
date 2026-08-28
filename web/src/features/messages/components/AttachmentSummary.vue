<script setup lang="ts">
import { onUnmounted, ref } from 'vue'
import type { AttachmentSummary } from '@/api/generated'
import { formatBytes } from '@/shared/utils/bytes'

const props = withDefaults(defineProps<{ file: AttachmentSummary; compact?: boolean; interactive?: boolean }>(), { compact: false, interactive: false })
const emit = defineEmits<{ view: [id: string] }>()
const safeImages = new Set(['image/jpeg', 'image/png', 'image/gif', 'image/webp'])
const imageFailed = ref(false)
const attempt = ref(0)
const revision = ref(0)
let timer = 0
const thumbnailUrl = () => `/api/v1/attachments/${encodeURIComponent(props.file.id)}/thumbnail?r=${revision.value}`
function thumbnailError() {
  const delays = [1_000, 3_000, 10_000]
  if (attempt.value >= delays.length) { imageFailed.value = true; return }
  timer = window.setTimeout(() => { attempt.value++; revision.value++ }, delays[attempt.value])
}
function icon() {
  if (props.file.detectedMime === 'application/pdf') return 'PDF'
  if (props.file.detectedMime.startsWith('audio/')) return 'AUD'
  if (props.file.detectedMime.startsWith('video/')) return 'VID'
  if (props.file.detectedMime.startsWith('text/')) return 'TXT'
  return 'FILE'
}
onUnmounted(() => clearTimeout(timer))
</script>

<template>
  <button
    v-if="interactive"
    class="attachment"
    :class="{ compact }"
    type="button"
    @click="emit('view', file.id)"
  >
    <img
      v-if="safeImages.has(file.detectedMime) && !imageFailed"
      :key="revision"
      :src="thumbnailUrl()"
      alt=""
      loading="lazy"
      @error="thumbnailError"
    >
    <span
      v-else
      class="file-icon"
    >{{ icon() }}</span>
    <span class="copy"><strong>{{ file.originalFilename }}</strong><small>{{ formatBytes(file.sizeBytes) }} · {{ file.detectedMime }}</small></span>
  </button>
  <div
    v-else
    class="attachment"
    :class="{ compact }"
  >
    <img
      v-if="safeImages.has(file.detectedMime) && !imageFailed"
      :key="revision"
      :src="thumbnailUrl()"
      alt=""
      loading="lazy"
      @error="thumbnailError"
    >
    <span
      v-else
      class="file-icon"
    >{{ icon() }}</span>
    <span class="copy"><strong>{{ file.originalFilename }}</strong><small>{{ formatBytes(file.sizeBytes) }}</small></span>
  </div>
</template>

<style scoped>
.attachment{display:grid;grid-template-columns:48px minmax(0,1fr);align-items:center;gap:.65rem;width:100%;min-width:0;padding:.55rem;border:1px solid var(--border);border-radius:var(--radius-sm);background:var(--surface-raised);text-align:left}.attachment:is(button):hover{border-color:var(--accent)}img,.file-icon{width:48px;height:42px;border-radius:calc(var(--radius-sm) - 2px)}img{object-fit:cover;background:var(--surface-soft)}.file-icon{display:grid;place-items:center;background:var(--surface-soft);color:var(--muted);font:700 .65rem/1 var(--font-mono);letter-spacing:.06em}.copy{display:grid;min-width:0;gap:.16rem}.copy strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:.86rem}.copy small{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--muted);font-size:.72rem}.compact{grid-template-columns:32px minmax(0,1fr);padding:.38rem;border:0;background:transparent}.compact img,.compact .file-icon{width:32px;height:30px}
</style>
