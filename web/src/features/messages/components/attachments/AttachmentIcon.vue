<script setup lang="ts">
import { computed } from 'vue'

const props = defineProps<{ mime: string }>()
const kind = computed(() => {
  if (props.mime === 'application/pdf') return { label: 'PDF', tone: 'document' }
  if (props.mime.startsWith('text/')) return { label: 'TXT', tone: 'document' }
  if (props.mime.startsWith('audio/')) return { label: 'AUD', tone: 'media' }
  if (props.mime.startsWith('video/')) return { label: 'VID', tone: 'media' }
  if (props.mime === 'application/vnd.android.package-archive') return { label: 'APK', tone: 'archive' }
  if (/^(application\/(zip|gzip|x-7z-compressed|x-rar-compressed|x-tar))$/.test(props.mime)) return { label: 'ZIP', tone: 'archive' }
  return { label: 'FILE', tone: 'file' }
})
</script>

<template>
  <span
    class="attachment-icon"
    :class="kind.tone"
    aria-hidden="true"
  >{{ kind.label }}</span>
</template>

<style scoped>
.attachment-icon{display:grid;place-items:center;width:100%;height:100%;border-radius:inherit;background:var(--surface-soft);color:var(--text-secondary);font:750 .66rem/1 var(--font-mono);letter-spacing:.06em}
.document{background:color-mix(in srgb,var(--content-document) 13%,var(--surface-soft));color:var(--content-document)}
.archive{background:color-mix(in srgb,var(--content-archive) 14%,var(--surface-soft));color:var(--content-archive)}
.media{background:color-mix(in srgb,var(--accent-secondary) 14%,var(--surface-soft));color:var(--accent-secondary)}
</style>
