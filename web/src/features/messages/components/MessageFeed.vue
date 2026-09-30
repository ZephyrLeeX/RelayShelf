<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import type { MessageSummary } from '@/api/generated'
import type { MessageFilters } from '@/shared/api/queryKeys'
import InlineError from '@/shared/ui/InlineError.vue'
import MessageCard from './MessageCard.vue'
import { useMessageFeed } from '../queries'
import { useBatchMessages } from '../composables/useBatchMessages'
import BatchTagOptions from './BatchTagOptions.vue'
import { toast } from '@/shared/ui/toast'

const props = withDefaults(defineProps<{ filters: MessageFilters; emptyText: string; enabled?: boolean }>(), { enabled: true })
const query = useMessageFeed(() => props.filters, () => props.enabled)
const sentinel = ref<HTMLElement>()
let observer: IntersectionObserver | undefined
const items = computed(() => {
  const seen = new Set<string>()
  return (query.data.value?.pages.flatMap((page) => page.items) ?? []).filter((item: MessageSummary) => !seen.has(item.id) && Boolean(seen.add(item.id)))
})
const batch = useBatchMessages(items, () => props.filters)
const addingTags = ref(false)
const tagIds = ref<string[]>([])
watch(batch.active, () => { addingTags.value = false; tagIds.value = [] })
const isRefreshing = computed(() => query.isFetching.value && !query.isPending.value && !query.isFetchingNextPage.value)
// A short first page can leave the sentinel visible throughout loading.
watch(() => items.value.length, () => {
  if (sentinel.value && observer) {
    observer.unobserve(sentinel.value)
    observer.observe(sentinel.value)
  }
}, { flush: 'post' })
watch(() => query.errorUpdateCount.value, (count, previousCount) => {
  if (count > previousCount && query.isRefetchError.value && items.value.length) {
    toast.error('同步失败，当前显示的是已有内容')
  }
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
      <button
        class="button"
        :disabled="batch.busy.value || !enabled"
        @click="batch.active.value ? batch.reset() : batch.active.value = true"
      >
        {{ batch.active.value ? '退出多选' : '多选' }}
      </button>
      <span
        role="status"
        aria-live="polite"
      >{{ isRefreshing ? '正在同步最新内容…' : '当前分区的全部内容' }}</span>
    </div>
    <section
      v-if="batch.active.value"
      class="batch-toolbar panel"
      aria-label="批量操作"
    >
      <div class="batch-actions">
        <strong>已选 {{ batch.count.value }} 项</strong>
        <button
          class="button"
          :disabled="batch.busy.value"
          @click="batch.selectLoaded"
        >
          全选已加载内容
        </button>
        <button
          class="button"
          :disabled="batch.busy.value"
          @click="batch.clear"
        >
          取消选择
        </button>
      </div>
      <div class="batch-actions">
        <template v-if="filters.trash">
          <button
            class="button primary"
            :disabled="batch.busy.value || !batch.count.value"
            @click="batch.run('restore')"
          >
            批量恢复
          </button>
        </template>
        <template v-else>
          <button
            class="button"
            :disabled="batch.busy.value || !batch.count.value"
            @click="batch.run('permanent')"
          >
            批量转长期
          </button>
          <button
            class="button"
            :disabled="batch.busy.value || !batch.count.value"
            :aria-expanded="addingTags"
            @click="addingTags = !addingTags"
          >
            批量添加标签
          </button>
          <button
            class="button danger"
            :disabled="batch.busy.value || !batch.count.value"
            @click="batch.run('trash')"
          >
            批量移入回收站
          </button>
        </template>
      </div>
      <template v-if="addingTags && !filters.trash">
        <BatchTagOptions
          v-model="tagIds"
          :disabled="batch.busy.value"
        />
        <button
          class="button primary"
          :disabled="batch.busy.value || !batch.count.value || !tagIds.length"
          @click="batch.run('tags', tagIds)"
        >
          追加所选标签
        </button>
      </template>
      <div
        v-if="batch.total.value"
        role="status"
        aria-live="polite"
      >
        {{ batch.busy.value ? '正在处理' : '处理完成' }} {{ batch.completed.value }}/{{ batch.total.value }} · 成功 {{ batch.succeeded.value }} · 失败 {{ batch.failures.value.length }}
        <progress
          :value="batch.completed.value"
          :max="batch.total.value"
          aria-label="批量处理进度"
        />
      </div>
      <template v-if="batch.failures.value.length">
        <ul
          class="batch-failures"
          aria-label="逐项失败结果"
        >
          <li
            v-for="failure in batch.failures.value"
            :key="failure.message.id"
          >
            {{ failure.message.title || failure.message.id }}：{{ failure.error }}
          </li>
        </ul>
        <p class="muted">
          失败项仍保留选择。重试会读取最新版本，并保留最新标签。
        </p>
        <button
          class="button"
          :disabled="batch.busy.value"
          @click="batch.retryFailed"
        >
          只重试失败项
        </button>
      </template>
    </section>
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
    <div
      v-for="message in items"
      :key="message.id"
      class="feed-item"
      :class="{ 'batch-selected': batch.active.value && batch.selected.value.has(message.id) }"
    >
      <label
        v-if="batch.active.value"
        class="selection-control"
      >
        <input
          type="checkbox"
          :checked="batch.selected.value.has(message.id)"
          :disabled="batch.busy.value"
          @change="batch.toggle(message)"
        >
        选择 {{ message.title || message.id }}
      </label>
      <MessageCard
        :message="message"
        :trash="filters.trash"
        :batch-mode="batch.active.value"
      />
    </div>
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
.batch-toolbar{display:grid;gap:.65rem;padding:.85rem;min-width:0}.batch-actions{display:flex;flex-wrap:wrap;align-items:center;gap:.5rem}.batch-actions strong{font-size:.85rem}.batch-toolbar>button{justify-self:start}.batch-toolbar p{margin:0;font-size:.8rem}.batch-toolbar progress{display:block;width:100%;margin-top:.4rem}.batch-failures{margin:0;padding-left:1.2rem;max-height:180px;overflow:auto;font-size:.8rem;overflow-wrap:anywhere}.feed-item{min-width:0}.selection-control{display:flex;align-items:center;gap:.5rem;padding:.5rem;min-height:44px;overflow-wrap:anywhere;cursor:pointer}.selection-control input{width:18px;height:18px;flex-shrink:0}.batch-selected{border-radius:var(--radius);outline:2px solid var(--accent-primary)}.batch-toolbar .button{min-height:40px}@media(max-width:600px){.batch-actions .button{flex:1 1 auto}.feed-toolbar{flex-wrap:wrap}}
.feed { display:grid;min-width:0;gap:.65rem; }.feed-toolbar{display:flex;align-items:baseline;justify-content:space-between;gap:1rem;padding:.1rem .15rem .3rem;border-bottom:1px solid var(--border-default)}.feed-toolbar strong{font-size:.82rem}.feed-toolbar span{color:var(--text-tertiary);font-size:.7rem;text-align:right}.loading,.empty,.end { padding:1.5rem; text-align:center; }.empty { color:var(--muted); }.spinner { display:inline-block; width:1rem; height:1rem; margin-right:.5rem; border:2px solid var(--border); border-top-color:var(--accent); border-radius:50%; animation:spin .7s linear infinite; }.sentinel { height:1px; } @keyframes spin { to { transform:rotate(360deg); } } @media(prefers-reduced-motion:reduce){.spinner{animation:none}}
</style>
