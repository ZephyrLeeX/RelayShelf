<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { useDetailSelection } from '@/app/composables/useDetailSelection'
import MessageInspector from './MessageInspector.vue'

const { selectedMessageId, closeDetail } = useDetailSelection()
const detailSheet = ref<HTMLElement>()
let returnFocusTo: HTMLElement | null = null
const surface = ref<HTMLElement>()
let listScrollTop = 0
let listWindowScroll = 0

watch(selectedMessageId, async (id, previousId) => {
  if (id) {
    if (!previousId) {
      returnFocusTo = document.activeElement instanceof HTMLElement ? document.activeElement : null
      listScrollTop = surface.value?.closest('main')?.scrollTop ?? 0
      listWindowScroll = window.scrollY
    }
    await nextTick()
    surface.value?.closest('main')?.scrollTo?.(0, 0)
    window.scrollTo?.(0, 0)
    detailSheet.value?.focus({ preventScroll: true })
    return
  }
  if (previousId) {
    await nextTick()
    returnFocusTo?.focus({ preventScroll: true })
    surface.value?.closest('main')?.scrollTo?.(0, listScrollTop)
    window.scrollTo?.(0, listWindowScroll)
    returnFocusTo = null
  }
}, { immediate: true })

</script>

<template>
  <div
    v-show="selectedMessageId"
    ref="surface"
    class="detail-surface panel"
    :class="{ selected: selectedMessageId }"
    aria-label="内容详情"
  >
    <section
      v-if="selectedMessageId"
      ref="detailSheet"
      class="detail-sheet"
      role="dialog"
      aria-labelledby="detail-title"
      tabindex="-1"
    >
      <MessageInspector
        :id="selectedMessageId"
        :key="selectedMessageId"
        @close="closeDetail"
      />
    </section>
  </div>
</template>

<style scoped>
.detail-surface{min-width:0;max-width:100%;overflow:hidden;background:var(--surface-raised)}.detail-sheet{min-width:0;outline:none}
</style>
