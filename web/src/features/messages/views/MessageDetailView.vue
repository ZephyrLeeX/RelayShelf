<script setup lang="ts">
import { useRoute, useRouter } from 'vue-router'
import MessageInspector from '../components/detail/MessageInspector.vue'

defineProps<{ id: string }>()
const route = useRoute()
const router = useRouter()

function close() {
  const from = typeof route.query.from === 'string' && route.query.from.startsWith('/') ? route.query.from : ''
  if (from) void router.push(from)
  else void router.replace('/temporary')
}
</script>

<template>
  <article class="legacy-detail panel">
    <MessageInspector
      :id="id"
      @close="close"
    />
  </article>
</template>

<style scoped>
.legacy-detail{width:min(760px,100%);margin:0 auto;overflow:hidden}
</style>
