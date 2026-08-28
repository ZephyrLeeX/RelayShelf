<script setup lang="ts">
import { computed } from 'vue'
import { visibleUploads } from '../store'

const resumable = computed(() => visibleUploads.value.filter((item) => item.status === 'PAUSED' && !item.file).length)
</script>

<template>
  <p
    v-if="resumable"
    class="resume-note"
  >
    {{ resumable }} 个上传等待重新选择原文件。
  </p>
</template>

<style scoped>.resume-note{margin:0;padding:.65rem .75rem;border-radius:var(--radius-sm);background:var(--accent-soft);color:var(--accent-strong);font-size:.82rem}</style>
