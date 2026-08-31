<script setup lang="ts">
import { onUnmounted, ref } from 'vue'
import AttachmentIcon from './AttachmentIcon.vue'

defineProps<{ id: string; mime: string; alt?: string }>()
const attempt = ref(0)
const revision = ref(0)
const failed = ref(false)
let timer = 0

function thumbnailError() {
  const delays = [1_000, 3_000, 10_000]
  if (attempt.value >= delays.length) {
    failed.value = true
    return
  }
  timer = window.setTimeout(() => {
    attempt.value++
    revision.value++
  }, delays[attempt.value])
}

onUnmounted(() => window.clearTimeout(timer))
</script>

<template>
  <img
    v-if="!failed"
    :key="revision"
    :src="`/api/v1/attachments/${encodeURIComponent(id)}/thumbnail?r=${revision}`"
    :alt="alt ?? ''"
    loading="lazy"
    @error="thumbnailError"
  >
  <AttachmentIcon
    v-else
    :mime="mime"
  />
</template>

<style scoped>
img{display:block;width:100%;height:100%;object-fit:cover;background:var(--surface-soft)}
</style>
