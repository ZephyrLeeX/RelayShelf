<script setup lang="ts">
import type { AttachmentSummary as Attachment } from '@/api/generated'
import AttachmentSummary from './AttachmentSummary.vue'

withDefaults(defineProps<{ files: Attachment[]; limit?: number; total?: number; interactive?: boolean; removable?: boolean }>(), { limit: 0, total: 0, interactive: false, removable: false })
defineEmits<{ view: [id: string]; remove: [id: string] }>()
</script>

<template>
  <div class="attachment-list">
    <div
      v-for="file in (limit ? files.slice(0, limit) : files)"
      :key="file.id"
      class="row"
    >
      <AttachmentSummary
        :file="file"
        :compact="Boolean(limit)"
        :interactive="interactive"
        @view="$emit('view', $event)"
      />
      <button
        v-if="removable"
        class="button danger remove"
        type="button"
        @click="$emit('remove', file.id)"
      >
        移除
      </button>
    </div>
    <p
      v-if="total > files.slice(0, limit || files.length).length"
      class="more"
    >
      还有 {{ total - files.slice(0, limit || files.length).length }} 个附件
    </p>
  </div>
</template>

<style scoped>.attachment-list{display:grid;gap:.4rem}.row{display:flex;align-items:center;gap:.45rem}.row>:first-child{flex:1;min-width:0}.remove{min-height:38px}.more{margin:.15rem .45rem;color:var(--muted);font-size:.78rem}</style>
