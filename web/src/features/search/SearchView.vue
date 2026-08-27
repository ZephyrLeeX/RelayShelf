<script setup lang="ts">
import { computed, ref, watch } from 'vue'
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
      <label class="field main">搜索词<input
        v-model="q"
        placeholder="至少 2 个 Unicode 字符"
      ></label>
      <label class="field">区域<select v-model="lifecycle"><option value="">全部</option><option :value="Lifecycle.TEMPORARY">Temporary</option><option :value="Lifecycle.PERMANENT">Permanent</option></select></label>
      <label class="field">时间<select v-model="time"><option value="all">全部</option><option value="24h">24 小时</option><option value="7d">7 天</option><option value="30d">30 天</option></select></label>
      <label class="toggle"><input
        v-model="favorite"
        type="checkbox"
      > 仅收藏</label>
      <details>
        <summary>更多筛选</summary><label class="field">精确内容类型<input
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
      </details>
      <p
        v-if="validation"
        class="error"
        role="alert"
      >
        {{ validation }}
      </p>
      <button
        class="button primary"
        type="submit"
      >
        应用搜索
      </button>
    </form>
    <MessageFeed
      :filters="filters"
      :enabled="!appliedValidation"
      empty-text="没有找到匹配内容"
    />
  </section>
</template>

<style scoped>
.search-page{display:grid;gap:1rem}h1,p{margin:.2rem 0}.filters{padding:1rem;display:grid;grid-template-columns:2fr 1fr 1fr;gap:.8rem;align-items:end}.toggle{align-self:center}.filters details,.filters .error{grid-column:1/-1}.filters details[open]{display:grid;grid-template-columns:1fr 2fr;gap:1rem}.filters summary{grid-column:1/-1;cursor:pointer}.filters fieldset{display:flex;gap:.7rem;flex-wrap:wrap;border:1px solid var(--border);border-radius:var(--radius-sm)}.filters .button{justify-self:end}
@media(max-width:650px){.filters{grid-template-columns:1fr}.filters details[open]{grid-template-columns:1fr}.filters .button{justify-self:stretch}}
</style>
