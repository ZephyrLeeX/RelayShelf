<script setup lang="ts">
import { FileUp } from '@lucide/vue'
import type { UploadItem } from '@/features/uploads/types'
import { uploadStatusLabel } from '@/features/uploads/labels'
import { formatBytes } from '@/shared/utils/bytes'

defineProps<{ uploads: UploadItem[]; blocking: boolean }>()
defineEmits<{ remove: [clientId: string] }>()
</script>

<template>
  <section
    v-if="uploads.length"
    class="attachments"
    aria-label="本条内容的附件"
  >
    <ul class="selected-files">
      <li
        v-for="item in uploads"
        :key="item.clientId"
      >
        <FileUp
          class="file-mark"
          aria-hidden="true"
        />
        <div>
          <strong>{{ item.filename }}</strong>
          <small>
            {{ formatBytes(item.size) }} · {{ uploadStatusLabel(item.status) }}<template v-if="item.status === 'UPLOADING'"> · {{ Math.round(item.progress * 100) }}%</template>
          </small>
        </div>
        <button
          class="button remove"
          type="button"
          @click="$emit('remove', item.clientId)"
        >
          移除
        </button>
      </li>
    </ul>
    <p
      v-if="blocking"
      class="warning"
    >
      等待附件上传完成。失败的附件需要重试或移除后才能发送。
    </p>
  </section>
</template>

<style scoped>
.attachments{padding:.7rem .85rem;border-bottom:1px solid var(--border-default);background:color-mix(in srgb,var(--surface-soft) 56%,transparent)}
.selected-files{list-style:none;margin:0;padding:0;display:grid;gap:.4rem}.selected-files li{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:.65rem;padding:.5rem .55rem;border-radius:var(--radius-sm);background:var(--surface-raised)}
.file-mark{box-sizing:border-box;width:28px;height:28px;padding:6px;border-radius:.45rem;background:color-mix(in srgb,var(--accent-secondary) 14%,transparent);color:var(--accent-secondary)}.selected-files div{min-width:0;display:grid}.selected-files strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:.86rem}.selected-files small{color:var(--text-secondary);font-size:.72rem}.remove{min-height:30px;padding:.25rem .55rem;box-shadow:none;font-size:.74rem}.warning{margin:.55rem .2rem 0;color:var(--state-warning);font-size:.78rem}
</style>
