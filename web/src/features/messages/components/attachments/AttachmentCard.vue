<script setup lang="ts">
import type { AttachmentSummary } from '@/api/generated'
import { formatBytes } from '@/shared/utils/bytes'
import AttachmentIcon from './AttachmentIcon.vue'
import AttachmentThumbnail from './AttachmentThumbnail.vue'

defineProps<{ file: AttachmentSummary }>()
const safeImages = new Set(['image/jpeg', 'image/png', 'image/gif', 'image/webp'])
</script>

<template>
  <div
    class="attachment-card"
    :class="{ image: safeImages.has(file.detectedMime) }"
  >
    <div class="visual">
      <AttachmentThumbnail
        v-if="safeImages.has(file.detectedMime)"
        :id="file.id"
        :mime="file.detectedMime"
        :alt="file.originalFilename"
      />
      <AttachmentIcon
        v-else
        :mime="file.detectedMime"
      />
    </div>
    <span class="file-copy">
      <strong>{{ file.originalFilename }}</strong>
      <small>{{ formatBytes(file.sizeBytes) }} · {{ file.detectedMime }}</small>
    </span>
  </div>
</template>

<style scoped>
.attachment-card{display:grid;grid-template-columns:48px minmax(0,1fr);align-items:center;gap:.6rem;min-width:0;padding:.5rem;border:1px solid var(--border-default);border-radius:var(--radius-sm);background:var(--surface-soft)}
.visual{width:48px;height:42px;overflow:hidden;border-radius:calc(var(--radius-sm) - 2px);background:var(--surface-raised)}
.file-copy{display:grid;min-width:0;gap:.14rem}.file-copy strong,.file-copy small{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.file-copy strong{font-size:.8rem}.file-copy small{color:var(--text-tertiary);font-size:.67rem}
.attachment-card.image{position:relative;display:block;min-height:116px;padding:0;overflow:hidden;background:var(--surface-soft)}.image .visual{width:100%;height:116px;border-radius:0}.image .file-copy{position:absolute;inset:auto 0 0;padding:1.45rem .55rem .5rem;color:#fff;background:linear-gradient(transparent,rgb(12 14 22 / .82))}.image .file-copy small{color:rgb(255 255 255 / .78)}
</style>
