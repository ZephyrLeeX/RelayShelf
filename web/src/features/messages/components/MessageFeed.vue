<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import type { MessageSummary } from '@/api/generated'
import type { MessageFilters } from '@/shared/api/queryKeys'
import InlineError from '@/shared/ui/InlineError.vue'
import MessageCard from './MessageCard.vue'
import { useMessageFeed } from '../queries'

const props = withDefaults(defineProps<{ filters: MessageFilters; emptyText: string; enabled?: boolean }>(), { enabled: true })
const query = useMessageFeed(() => props.filters, () => props.enabled)
const sentinel = ref<HTMLElement>()
let observer: IntersectionObserver | undefined
const items = computed(() => {
  const seen = new Set<string>()
  return (query.data.value?.pages.flatMap((page) => page.items) ?? []).filter((item: MessageSummary) => !seen.has(item.id) && Boolean(seen.add(item.id)))
})
onMounted(() => {
  observer = new IntersectionObserver((entries) => {
    if (entries.some((entry) => entry.isIntersecting) && query.hasNextPage.value && !query.isFetchingNextPage.value) void query.fetchNextPage()
  }, { rootMargin: '240px' })
  if (sentinel.value) observer.observe(sentinel.value)
})
onUnmounted(() => observer?.disconnect())
</script>

<template>
  <div
    class="feed"
    :aria-busy="query.isPending.value"
  >
    <div class="feed-toolbar">
      <strong>内容流</strong>
      <span>当前分区的全部内容</span>
    </div>
    <p
      v-if="query.isFetching.value && !query.isPending.value"
      class="refresh muted"
    >
      正在同步…
    </p>
    <div
      v-if="query.isPending.value"
      class="loading"
    >
      <span class="spinner" />正在加载内容…
    </div>
    <InlineError
      v-else-if="query.isError.value && !items.length"
      :message="'内容加载失败，请重试。'"
      @retry="query.refetch()"
    />
    <p
      v-else-if="!items.length"
      class="empty panel"
    >
      {{ emptyText }}
    </p>
    <MessageCard
      v-for="message in items"
      :key="message.id"
      :message="message"
      :trash="filters.trash"
    />
    <div
      ref="sentinel"
      class="sentinel"
      aria-hidden="true"
    />
    <div
      v-if="query.isFetchingNextPage.value"
      class="loading"
    >
      <span class="spinner" />正在加载更多…
    </div>
    <InlineError
      v-else-if="query.isFetchNextPageError.value"
      message="更多内容加载失败，已加载的内容仍保留。"
      @retry="query.fetchNextPage()"
    />
    <p
      v-else-if="items.length && !query.hasNextPage.value"
      class="end muted"
    >
      已显示全部内容
    </p>
  </div>
</template>

<style scoped>
.feed { display:grid; gap:.65rem; }.feed-toolbar{display:flex;align-items:baseline;justify-content:space-between;gap:1rem;padding:.1rem .15rem .3rem;border-bottom:1px solid var(--border-default)}.feed-toolbar strong{font-size:.82rem}.feed-toolbar span{color:var(--text-tertiary);font-size:.7rem}.refresh { margin:0; text-align:right; font-size:.8rem; }.loading,.empty,.end { padding:1.5rem; text-align:center; }.empty { color:var(--muted); }.spinner { display:inline-block; width:1rem; height:1rem; margin-right:.5rem; border:2px solid var(--border); border-top-color:var(--accent); border-radius:50%; animation:spin .7s linear infinite; }.sentinel { height:1px; } @keyframes spin { to { transform:rotate(360deg); } } @media(prefers-reduced-motion:reduce){.spinner{animation:none}}
</style>
