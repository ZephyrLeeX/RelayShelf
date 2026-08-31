<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { useDetailSelection } from '@/app/composables/useDetailSelection'
import MessageInspector from './MessageInspector.vue'

const { selectedMessageId, closeDetail } = useDetailSelection()
const detailSheet = ref<HTMLElement>()
const compact = ref(false)
let returnFocusTo: HTMLElement | null = null
let compactMedia: MediaQueryList | undefined

watch(selectedMessageId, async (id, previousId) => {
  if (id) {
    if (!previousId) returnFocusTo = document.activeElement instanceof HTMLElement ? document.activeElement : null
    await nextTick()
    detailSheet.value?.focus()
    return
  }
  if (previousId) {
    returnFocusTo?.focus()
    returnFocusTo = null
  }
}, { immediate: true })

function onCompactChange(event: MediaQueryListEvent) { compact.value = event.matches }

onMounted(() => {
  compactMedia = window.matchMedia?.('(max-width: 1179px)')
  compact.value = compactMedia?.matches ?? false
  compactMedia?.addEventListener('change', onCompactChange)
})
onUnmounted(() => {
  compactMedia?.removeEventListener('change', onCompactChange)
})
</script>

<template>
  <aside
    class="detail-surface"
    :class="{ selected: selectedMessageId }"
    aria-label="内容详情"
  >
    <button
      v-if="selectedMessageId"
      class="detail-scrim"
      type="button"
      aria-label="关闭内容详情"
      @click="closeDetail"
    />
    <section
      v-if="selectedMessageId"
      ref="detailSheet"
      class="detail-sheet"
      role="dialog"
      :aria-modal="compact ? 'true' : undefined"
      aria-labelledby="detail-title"
      tabindex="-1"
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
    <div
      v-else
      class="detail-empty"
    >
      <div aria-hidden="true">
        ⌁
      </div>
      <strong>选择一条内容查看详情</strong>
      <p>正文、附件与内容操作会显示在这里。</p>
    </div>
  </aside>
</template>

<style scoped>
.detail-surface{position:relative;min-width:0;overflow:hidden;border-left:1px solid var(--border-default);background:var(--surface-raised)}.detail-sheet{height:100%;overflow:auto}.detail-scrim,.drag-handle{display:none}.detail-empty{display:grid;place-content:center;justify-items:center;height:100%;padding:2rem;color:var(--text-tertiary);text-align:center}.detail-empty>div{display:grid;place-items:center;width:48px;height:48px;margin-bottom:.8rem;border:1px solid var(--border-default);border-radius:14px;background:var(--surface-soft);color:var(--accent-primary);font-size:1.4rem}.detail-empty strong{color:var(--text-secondary);font-size:.85rem}.detail-empty p{max-width:16rem;margin:.35rem 0;font-size:.75rem;line-height:1.5}
@media(max-width:1179px){.detail-surface{display:none}.detail-surface.selected{position:fixed;inset:0;z-index:60;display:block;overflow:hidden;border:0;background:transparent}.detail-scrim{display:block;position:absolute;inset:0;width:100%;height:100%;border:0;background:rgb(10 14 24 / .5)}.detail-sheet{position:absolute;left:0;right:0;bottom:0;height:min(86vh,820px);overflow:auto;border-radius:22px 22px 0 0;background:var(--surface-raised);box-shadow:var(--shadow-floating)}.drag-handle{position:sticky;top:0;z-index:4;display:block;width:42px;height:4px;margin:8px auto -4px;border-radius:999px;background:var(--border-strong)}.detail-empty{display:none}}
@media(prefers-reduced-motion:no-preference) and (max-width:1179px){.detail-sheet{animation:sheet-in .18s ease-out}@keyframes sheet-in{from{transform:translateY(24px);opacity:.8}}}
</style>
