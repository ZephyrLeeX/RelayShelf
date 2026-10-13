<script setup lang="ts">
import type { MessageSummary } from '@/api/generated'
import MessageInspector from './detail/MessageInspector.vue'

defineProps<{ message: MessageSummary; trash?: boolean; batchMode?: boolean }>()
</script>

<template>
  <article
    class="message-card panel"
    :data-message-id="message.id"
  >
    <MessageInspector
      :id="message.id"
      :summary="message"
      :batch-mode="batchMode"
      inline
    />
  </article>
</template>

<style scoped>
.message-card{min-width:0;overflow:hidden}
</style>
