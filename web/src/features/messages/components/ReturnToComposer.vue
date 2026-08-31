<script setup lang="ts">
import { ArrowUp } from '@lucide/vue'
import { onUnmounted, ref, watch } from 'vue'

/**
 * Floating "back to composer" action for long feeds. Visibility follows the
 * composer element itself through IntersectionObserver with a viewport root,
 * which works for both the window-scrolled mobile layout and the desktop
 * shell where `main.content` is its own scroll container. Clicking scrolls
 * the composer into view and focuses the textarea, honouring
 * prefers-reduced-motion.
 */
const props = defineProps<{ target?: HTMLElement | null }>()
const visible = ref(false)
let observer: IntersectionObserver | undefined

function disconnect() {
  observer?.disconnect()
  observer = undefined
  visible.value = false
}

watch(() => props.target, (element) => {
  disconnect()
  if (!element || typeof IntersectionObserver === 'undefined') return
  observer = new IntersectionObserver((entries) => {
    visible.value = !entries.some((entry) => entry.isIntersecting)
  })
  observer.observe(element)
}, { immediate: true })

onUnmounted(disconnect)

function jump() {
  const target = props.target
  if (!target) return
  const reducedMotion = window.matchMedia('(prefers-reduced-motion: reduce)').matches
  target.scrollIntoView({ behavior: reducedMotion ? 'auto' : 'smooth', block: 'start' })
  // focus() scrolls the newly focused element into view on its own, which
  // fights the smooth scroll above; the composer is already being revealed.
  const textarea = target.querySelector('textarea')
  if (textarea instanceof HTMLTextAreaElement) textarea.focus({ preventScroll: true })
}
</script>

<template>
  <button
    v-show="visible"
    class="return-to-composer"
    type="button"
    aria-label="回到发送框"
    title="回到发送框"
    @click="jump"
  >
    <ArrowUp aria-hidden="true" />
  </button>
</template>

<style scoped>
.return-to-composer{position:fixed;z-index:40;right:1rem;bottom:calc(4.9rem + env(safe-area-inset-bottom));display:inline-grid;place-items:center;width:44px;height:44px;border:1px solid var(--border-default);border-radius:999px;background:var(--surface-raised);color:var(--accent-primary);box-shadow:var(--shadow-md);cursor:pointer}
.return-to-composer svg{width:1.15rem;height:1.15rem}
.return-to-composer:hover{border-color:var(--accent-primary);background:var(--accent-primary-soft);color:var(--accent-primary-hover)}
.return-to-composer:focus-visible{outline:2px solid var(--focus-ring);outline-offset:2px}
@media(min-width:1180px){.return-to-composer{right:calc(400px + 1.75rem);bottom:1.25rem}}
</style>
