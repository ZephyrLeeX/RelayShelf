<script setup lang="ts">
defineProps<{ device?: string, uploadCount: number, active: boolean }>()
defineEmits<{ openUploads: [] }>()
</script>

<template>
  <button
    class="status-card"
    type="button"
    @click="$emit('openUploads')"
  >
    <span class="status-head"><i :class="{ active }" /><strong>{{ active ? '正在中继' : '中继待命' }}</strong></span>
    <span class="device">{{ device || '当前浏览器' }}</span>
    <span class="queue">{{ uploadCount ? `${uploadCount} 个上传任务` : '上传队列为空' }}<b aria-hidden="true">↗</b></span>
  </button>
</template>

<style scoped>
.status-card{display:grid;gap:.35rem;width:100%;padding:.85rem;border:1px solid color-mix(in srgb,var(--accent-secondary) 28%,var(--border-default));border-radius:var(--radius);background:linear-gradient(145deg,color-mix(in srgb,var(--accent-secondary) 9%,var(--surface-raised)),var(--surface-raised));box-shadow:var(--shadow-sm);text-align:left}
.status-head{display:flex;align-items:center;gap:.5rem;font-size:.78rem}.status-head i{width:.55rem;height:.55rem;border-radius:50%;background:var(--text-tertiary)}.status-head i.active{background:var(--accent-secondary);box-shadow:0 0 0 4px color-mix(in srgb,var(--accent-secondary) 16%,transparent)}.device{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--text-secondary);font-size:.72rem}.queue{display:flex;justify-content:space-between;color:var(--text-tertiary);font-size:.68rem}.queue b{color:var(--accent-secondary)}
</style>
