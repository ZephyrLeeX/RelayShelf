<script setup lang="ts">
import type { AttachmentSummary } from '@/api/generated'
import AttachmentCard from './AttachmentCard.vue'

withDefaults(defineProps<{ files: AttachmentSummary[]; limit?: number; total?: number }>(), { limit: 3, total: 0 })
</script>

<template>
  <div
    class="attachment-grid"
    :class="`count-${Math.min(files.length, limit)}`"
  >
    <AttachmentCard
      v-for="file in files.slice(0, limit)"
      :key="file.id"
      :file="file"
    />
    <div
      v-if="total > Math.min(files.length, limit)"
      class="attachment-more"
    >
      <strong>+{{ total - Math.min(files.length, limit) }}</strong>
      <span>更多附件</span>
    </div>
  </div>
</template>

<style scoped>
.attachment-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:.45rem}.attachment-grid.count-1{grid-template-columns:minmax(0,1fr)}.attachment-more{display:grid;place-content:center;justify-items:center;min-height:62px;border:1px dashed var(--border-strong);border-radius:var(--radius-sm);background:var(--surface-soft);color:var(--text-secondary)}.attachment-more strong{color:var(--accent-primary);font-size:1rem}.attachment-more span{font-size:.68rem}
@media(max-width:560px){.attachment-grid{grid-template-columns:minmax(0,1fr)}}
</style>
