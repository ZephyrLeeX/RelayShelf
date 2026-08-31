<script setup lang="ts">
import { Check, Copy, ExternalLink } from '@lucide/vue'
import { onUnmounted, ref } from 'vue'

const props = defineProps<{ body: string | null; requiresDetail?: boolean }>()
const emit = defineEmits<{ openDetail: [] }>()
const copied = ref(false)
let resetTimer = 0

async function activate() {
  if (props.requiresDetail) {
    emit('openDetail')
    return
  }
  if (!props.body) return
  await navigator.clipboard.writeText(props.body)
  copied.value = true
  window.clearTimeout(resetTimer)
  resetTimer = window.setTimeout(() => { copied.value = false }, 1_600)
}

onUnmounted(() => window.clearTimeout(resetTimer))
</script>

<template>
  <button
    class="quick-copy"
    type="button"
    :aria-label="requiresDetail ? '打开详情后复制正文' : '复制正文'"
    @click.stop="activate"
  >
    <Check
      v-if="copied"
      aria-hidden="true"
    />
    <ExternalLink
      v-else-if="requiresDetail"
      aria-hidden="true"
    />
    <Copy
      v-else
      aria-hidden="true"
    />
    {{ copied ? '已复制' : requiresDetail ? '打开并复制' : '复制' }}
  </button>
</template>

<style scoped>
.quick-copy{display:inline-flex;align-items:center;gap:.35rem;min-height:32px;border:1px solid var(--border-default);border-radius:999px;padding:.3rem .62rem;background:var(--surface-raised);color:var(--text-secondary);font-size:.75rem;font-weight:650;white-space:nowrap;box-shadow:var(--shadow-sm)}.quick-copy svg{width:.9rem;height:.9rem}
.quick-copy:hover{border-color:var(--accent-primary);color:var(--accent-primary);background:var(--accent-primary-soft)}
.quick-copy:focus-visible{outline:2px solid var(--focus-ring);outline-offset:2px}
</style>
