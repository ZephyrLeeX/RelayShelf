<script setup lang="ts">
defineProps<{ uploadCount: number, activeTransfers: boolean }>()
defineEmits<{ openUploads: [] }>()
</script>

<template>
  <header class="mobile-header">
    <RouterLink
      class="brand"
      to="/temporary"
    >
      <img
        src="/favicon.svg"
        alt=""
        width="30"
        height="30"
      ><strong>RelayShelf</strong>
    </RouterLink>
    <button
      type="button"
      :aria-label="`打开上传任务，${uploadCount} 个任务`"
      @click="$emit('openUploads')"
    >
      <i :class="{ active: activeTransfers }" /><span>{{ uploadCount || '上传' }}</span>
    </button>
  </header>
</template>

<style scoped>
.mobile-header{display:none;align-items:center;justify-content:space-between;gap:1rem;padding:.65rem .8rem;border-bottom:1px solid var(--border-default);background:color-mix(in srgb,var(--surface-raised) 94%,transparent);backdrop-filter:blur(14px)}.brand{display:flex;align-items:center;gap:.55rem;text-decoration:none}.brand strong{font-size:.95rem}.mobile-header button{display:flex;align-items:center;gap:.4rem;min-height:38px;padding:.3rem .65rem;border:1px solid var(--border-default);border-radius:999px;background:var(--surface-soft);font-size:.72rem}.mobile-header i{width:.5rem;height:.5rem;border-radius:50%;background:var(--text-tertiary)}.mobile-header i.active{background:var(--accent-secondary);box-shadow:0 0 0 3px color-mix(in srgb,var(--accent-secondary) 17%,transparent)}
@media(max-width:1179px){.mobile-header{display:flex;position:sticky;top:0;z-index:25}}
</style>
