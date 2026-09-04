<script setup lang="ts">
import { ChevronRight } from '@lucide/vue'
defineProps<{ name?: string, device?: string, compact?: boolean }>()
defineEmits<{ open: [] }>()
</script>

<template>
  <button
    class="account-button"
    :class="{ compact }"
    type="button"
    @click="$emit('open')"
  >
    <span
      class="avatar"
      aria-hidden="true"
    >{{ name?.slice(0, 1).toUpperCase() || 'R' }}</span>
    <span class="identity"><strong>{{ name || 'RelayShelf' }}</strong><small>{{ device || '设备与会话' }}</small></span>
    <ChevronRight
      class="chevron"
      aria-hidden="true"
    />
  </button>
</template>

<style scoped>
.account-button{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:.65rem;width:100%;min-height:50px;padding:.45rem;border:0;border-radius:var(--radius);background:transparent;text-align:left}
.account-button:hover{background:var(--surface-soft)}.avatar{display:grid;place-items:center;width:34px;height:34px;border-radius:10px;background:var(--accent-primary-soft);color:var(--accent-primary-hover);font-weight:800}.identity{display:grid;min-width:0}.identity strong,.identity small{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.identity strong{font-size:.82rem}.identity small{margin-top:.12rem;color:var(--text-tertiary);font-size:.68rem}.chevron{width:1rem;height:1rem;color:var(--text-tertiary)}.compact{width:auto;grid-template-columns:auto minmax(0,1fr)}.compact .identity small,.compact .chevron{display:none}
</style>
