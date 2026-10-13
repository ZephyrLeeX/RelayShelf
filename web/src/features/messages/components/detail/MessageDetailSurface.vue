<script setup lang="ts">
import { nextTick, onUnmounted, ref, watch } from 'vue'
import { useDetailSelection } from '@/app/composables/useDetailSelection'
import MessageInspector from './MessageInspector.vue'

const { selectedMessageId, closeDetail } = useDetailSelection()
const detailSheet = ref<HTMLElement>()
let returnFocusTo: HTMLElement | null = null
let previousBodyOverflow: string | undefined

function restoreScroll() {
  if (previousBodyOverflow === undefined) return
  document.body.style.overflow = previousBodyOverflow
  previousBodyOverflow = undefined
}

function trapFocus(event: KeyboardEvent) {
  if (event.key !== 'Tab' || !detailSheet.value) return
  if (document.querySelector('.viewer, .backdrop, .queue-backdrop, .more-backdrop')) return
  const controls = [...detailSheet.value.querySelectorAll<HTMLElement>('button:not(:disabled), a[href], input:not(:disabled), select:not(:disabled), textarea:not(:disabled), summary, [tabindex="0"]')]
    .filter(element => element.getClientRects().length > 0)
  const first = controls[0], last = controls.at(-1)
  if (!first || !last) { event.preventDefault(); detailSheet.value.focus(); return }
  if (event.shiftKey && (document.activeElement === first || document.activeElement === detailSheet.value)) {
    event.preventDefault(); last.focus()
  } else if (!event.shiftKey && (document.activeElement === last || document.activeElement === detailSheet.value)) {
    event.preventDefault(); first.focus()
  }
}

watch(selectedMessageId, async (id, previousId) => {
  if (id) {
    if (!previousId) {
      returnFocusTo = document.activeElement instanceof HTMLElement ? document.activeElement : null
      previousBodyOverflow = document.body.style.overflow
      document.body.style.overflow = 'hidden'
    }
    await nextTick()
    detailSheet.value?.focus({ preventScroll: true })
    return
  }
  if (previousId) {
    restoreScroll()
    await nextTick()
    returnFocusTo?.focus({ preventScroll: true })
    returnFocusTo = null
  }
}, { immediate: true })

onUnmounted(restoreScroll)
</script>

<template>
  <aside
    v-show="selectedMessageId"
    class="detail-surface"
    :class="{ selected: selectedMessageId }"
    aria-label="内容详情"
  >
    <button
      v-if="selectedMessageId"
      class="detail-scrim"
      type="button"
      aria-label="关闭内容详情"
      tabindex="-1"
      @click="closeDetail"
    />
    <section
      v-if="selectedMessageId"
      ref="detailSheet"
      class="detail-sheet"
      role="dialog"
      aria-modal="true"
      aria-labelledby="detail-title"
      tabindex="-1"
      @keydown="trapFocus"
    >
      <div
        class="drag-handle"
        aria-hidden="true"
      />
      <MessageInspector
        :id="selectedMessageId"
        :key="selectedMessageId"
        @close="closeDetail"
      />
    </section>
  </aside>
</template>

<style scoped>
.detail-surface{display:none}.detail-surface.selected{position:fixed;inset:0;z-index:60;display:block;overflow:hidden;background:transparent}.detail-scrim{position:absolute;inset:0;width:100%;height:100%;border:0;background:rgb(10 14 24 / .5)}.detail-sheet{position:absolute;top:0;right:0;bottom:0;width:min(560px,100vw);min-width:0;overflow:auto;overscroll-behavior:contain;border-left:1px solid var(--border-default);background:var(--surface-raised);box-shadow:var(--shadow-floating)}.drag-handle{display:none}
@media(max-width:1179px){.detail-sheet{top:auto;left:0;width:100%;height:min(86dvh,820px);border:0;border-radius:22px 22px 0 0}.drag-handle{position:sticky;top:0;z-index:4;display:block;width:42px;height:4px;margin:8px auto -4px;border-radius:999px;background:var(--border-strong)}}
@media(prefers-reduced-motion:no-preference){.detail-sheet{animation:drawer-in .18s ease-out}@keyframes drawer-in{from{transform:translateX(24px);opacity:.8}}}
@media(prefers-reduced-motion:no-preference) and (max-width:1179px){.detail-sheet{animation:sheet-in .18s ease-out}@keyframes sheet-in{from{transform:translateY(24px);opacity:.8}}}
</style>
