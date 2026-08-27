<script setup lang="ts">
defineProps<{ message: string }>()
defineEmits<{ retry: [] }>()
</script>
<template>
  <div
    class="inline-error"
    role="alert"
  >
    <span>{{ message }}</span><button
      class="button"
      type="button"
      @click="$emit('retry')"
    >
      重试
    </button>
  </div>
</template>
<style scoped>.inline-error { display:flex; gap:.75rem; align-items:center; justify-content:space-between; color:var(--danger); }</style>
