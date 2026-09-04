<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Search, SlidersHorizontal } from '@lucide/vue'
import { useRoute, useRouter } from 'vue-router'
import { Lifecycle } from '@/api/generated'
import MessageFeed from '@/features/messages/components/MessageFeed.vue'
import { useTagsQuery } from '@/features/tags/queries'
import { hasShortSearchToken } from './validation'

const route = useRoute()
const router = useRouter()
const tags = useTagsQuery()
const q = ref(stringQuery('q'))
const lifecycle = ref(stringQuery('lifecycle'))
const favorite = ref(stringQuery('favorite') === 'true')
const selectedTags = ref(arrayQuery('tagId'))
const type = ref(stringQuery('type'))
const time = ref(stringQuery('time') || 'all')

function stringQuery(key: string) { const value = route.query[key]; return typeof value === 'string' ? value : '' }
function arrayQuery(key: string) { const value = route.query[key]; return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : typeof value === 'string' ? [value] : [] }
const validation = computed(() => hasShortSearchToken(q.value) ? '每个搜索词至少 2 个字符' : '')
function afterFor(value: string) {
  const durations: Record<string, number> = { '24h': 1, '7d': 7, '30d': 30 }
  return durations[value] ? new Date(Date.now() - durations[value] * 86_400_000).toISOString() : undefined
}
const appliedValidation = computed(() => hasShortSearchToken(stringQuery('q')))
const filters = computed(() => ({ search: {
  q: stringQuery('q').trim() || undefined,
  lifecycle: stringQuery('lifecycle') === Lifecycle.TEMPORARY || stringQuery('lifecycle') === Lifecycle.PERMANENT ? stringQuery('lifecycle') as Lifecycle : undefined,
  favorite: stringQuery('favorite') === 'true' || undefined,
  tagIds: arrayQuery('tagId').length ? arrayQuery('tagId') : undefined,
  type: stringQuery('type').trim() || undefined,
  createdAfter: afterFor(stringQuery('time')),
} }))

function submit() {
  if (validation.value) return
  void router.push({ name: 'search', query: {
    ...(q.value.trim() && { q: q.value.trim() }),
    ...(lifecycle.value && { lifecycle: lifecycle.value }),
    ...(favorite.value && { favorite: 'true' }),
    ...(selectedTags.value.length && { tagId: selectedTags.value }),
    ...(type.value.trim() && { type: type.value.trim() }),
    ...(time.value !== 'all' && { time: time.value }),
  } })
}
watch(() => route.query, () => {
  q.value = stringQuery('q'); lifecycle.value = stringQuery('lifecycle'); favorite.value = stringQuery('favorite') === 'true'; selectedTags.value = arrayQuery('tagId'); type.value = stringQuery('type'); time.value = stringQuery('time') || 'all'
})
</script>

<template>
  <section class="search-page">
    <header>
      <h1>搜索</h1><p class="muted">
        通过正文、文件名、标签和时间找回内容。
      </p>
    </header>
    <form
      class="filters panel"
      @submit.prevent="submit"
    >
      <label class="main-search">
        <span class="sr-only">搜索词</span>
        <Search aria-hidden="true" />
        <input
          v-model="q"
          placeholder="搜索内容、文件名或标签"
          autocomplete="off"
        >
      </label>
      <div class="filter-row">
        <label class="field">区域<select v-model="lifecycle"><option value="">全部</option><option :value="Lifecycle.TEMPORARY">临时区</option><option :value="Lifecycle.PERMANENT">长期区</option></select></label>
        <label class="field">时间<select v-model="time"><option value="all">全部</option><option value="24h">24 小时</option><option value="7d">7 天</option><option value="30d">30 天</option></select></label>
        <label class="toggle"><input
          v-model="favorite"
          type="checkbox"
        > 仅收藏</label>
        <button
          class="button primary submit"
          type="submit"
        >
          搜索
        </button>
      </div>
      <details>
        <summary><SlidersHorizontal aria-hidden="true" />更多筛选</summary><div class="advanced">
          <label class="field">精确内容类型<input
            v-model="type"
            placeholder="例如 CODE"
          ></label><fieldset>
            <legend>标签</legend><label
              v-for="tag in tags.data.value"
              :key="tag.id"
            ><input
              v-model="selectedTags"
              type="checkbox"
              :value="tag.id"
            > {{ tag.name }}</label>
          </fieldset>
        </div>
      </details>
      <p
        v-if="validation"
        class="error"
        role="alert"
      >
        {{ validation }}
      </p>
    </form>
    <MessageFeed
      :filters="filters"
      :enabled="!appliedValidation"
      empty-text="没有找到匹配内容"
    />
  </section>
</template>

<style scoped>
.search-page{container-type:inline-size;display:grid;gap:1rem;min-width:0}h1,p{margin:.2rem 0}.filters{display:grid;gap:.85rem;min-width:0;padding:1rem}.main-search{display:grid;grid-template-columns:auto minmax(0,1fr);align-items:center;gap:.65rem;min-width:0;min-height:48px;padding:0 .9rem;border:1px solid var(--border-default);border-radius:12px;background:var(--surface-soft);color:var(--text-tertiary)}.main-search:focus-within{border-color:var(--accent-primary);box-shadow:0 0 0 3px var(--accent-primary-soft)}.main-search svg{width:1.05rem;height:1.05rem}.main-search input{min-width:0;width:100%;border:0;outline:0;background:transparent;color:var(--text-primary)}.main-search input::placeholder{color:var(--text-tertiary)}.filter-row{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr) auto auto;align-items:end;gap:.75rem;min-width:0}.toggle{display:flex;align-items:center;gap:.35rem;min-height:42px;white-space:nowrap}.submit{min-width:82px}.filters details,.filters .error{min-width:0}.filters summary{display:inline-flex;align-items:center;gap:.4rem;cursor:pointer;color:var(--text-secondary);font-size:.8rem;font-weight:650}.filters summary svg{width:.9rem;height:.9rem}.advanced{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,2fr);gap:1rem;margin-top:.8rem}.filters fieldset{display:flex;min-width:0;gap:.7rem;flex-wrap:wrap;border:1px solid var(--border-default);border-radius:var(--radius-sm)}.filters .error{margin:0;font-size:.78rem}
@container(max-width:650px){.filters{padding:.8rem}.main-search{min-height:46px;padding-inline:.75rem}.filter-row{grid-template-columns:minmax(0,1fr) minmax(0,1fr)}.toggle{min-height:40px}.submit{width:100%}.advanced{grid-template-columns:minmax(0,1fr)}}
@container(max-width:390px){.filter-row{grid-template-columns:minmax(0,1fr)}.toggle{min-height:auto}.submit{margin-top:.1rem}}
</style>
