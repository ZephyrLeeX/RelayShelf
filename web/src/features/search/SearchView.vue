<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { Search, SlidersHorizontal } from '@lucide/vue'
import { useRoute, useRouter } from 'vue-router'
import { useQueryClient } from '@tanstack/vue-query'
import { DefaultService, Lifecycle, type SearchConditions } from '@/api/generated'
import MessageFeed from '@/features/messages/components/MessageFeed.vue'
import { useTagsQuery } from '@/features/tags/queries'
import { displayError } from '@/shared/api/errors'
import { conditionError, conditionsQuery, readConditions, searchFilters, queryError } from './conditions'
import { savedSearchKey, useSavedSearches } from './savedQueries'

const route = useRoute(), router = useRouter(), client = useQueryClient()
const tags = useTagsQuery(), saved = useSavedSearches()
const form = reactive(readConditions(route.query))
const name = ref(''), error = ref(''), status = ref(''), busy = ref(false)
const loadedId = ref(''), now = ref(Date.now()), opening = ref(0)
const savedId = computed(() => typeof route.query.saved === 'string' ? route.query.saved : '')
const current = computed(() => saved.data.value?.find(v => v.id === savedId.value))
const applied = computed(() => readConditions(route.query))
const validation = computed(() => conditionError(conditions()))
const appliedError = computed(() => {
  if (queryError(route.query)) return queryError(route.query)
  if (savedId.value && saved.error.value) return '无法读取保存视图，请重试；搜索已暂停'
  if (savedId.value && !saved.isPending.value && !current.value) return '保存视图已删除或不可用'
  if (savedId.value && (opening.value > 0 || loadedId.value !== savedId.value || route.query.time === undefined)) return '正在读取保存视图…'
  if (current.value?.invalidReason) return current.value.invalidReason
  const invalid = conditionError(applied.value)
  if (invalid) return invalid
  if (applied.value.tagIds.length && tags.error.value) return '无法验证标签，请重试；搜索已暂停'
  if (applied.value.tagIds.length && !tags.data.value) return '正在验证标签…'
  if (applied.value.tagIds.some(id => !tags.data.value?.some(tag => tag.id === id))) return '标签已删除或不可用，请重新选择并搜索；保存视图需明确更新条件'
  return ''
})
const filters = computed(() => searchFilters(applied.value, now.value))
watch(() => route.query, () => { Object.assign(form, readConditions(route.query)); now.value = Date.now() })
watch([savedId, () => saved.data.value, () => route.query], async () => {
  if (!savedId.value) { loadedId.value = ''; return }
  if (!current.value || (loadedId.value === savedId.value && route.query.time !== undefined)) return
  loadedId.value = savedId.value; name.value = current.value.name; now.value = Date.now()
  opening.value++
  try { await router.replace({ name: 'search', query: { ...conditionsQuery(current.value.conditions), saved: savedId.value } }) }
  finally { opening.value-- }
}, { immediate: true })
function conditions(): SearchConditions {
  return { ...form, q: form.q.trim(), tagIds: [...form.tagIds], from: form.time === 'custom' ? form.from : '', to: form.time === 'custom' ? form.to : '' }
}
function submit() {
  if (conditionError(conditions())) return
  now.value = Date.now()
  void router.push({ name: 'search', query: { ...conditionsQuery(conditions()), saved: savedId.value || undefined } })
}
async function manage(action: 'create' | 'rename' | 'update' | 'delete') {
  error.value = ''; status.value = ''; busy.value = true
  try {
    if (action === 'delete') {
      await DefaultService.deleteSavedSearch(savedId.value)
      await router.replace({ name: 'search', query: {} }); status.value = '视图已删除'
    } else {
      const value = action === 'rename' ? current.value?.conditions : conditions()
      if (!value || (action !== 'rename' && conditionError(value))) throw new Error('请先修正筛选条件')
      if (!name.value.trim()) throw new Error('请输入视图名称')
      const request = { name: action === 'update' ? current.value!.name : name.value.trim(), conditions: value }
      const item = action === 'create' ? await DefaultService.createSavedSearch(request) : await DefaultService.updateSavedSearch(savedId.value, request)
      client.setQueryData(savedSearchKey, (items: typeof saved.data.value) => [item, ...(items ?? []).filter(v => v.id !== item.id)])
      loadedId.value = item.id; name.value = item.name; now.value = Date.now()
      await router.replace({ name: 'search', query: { ...conditionsQuery(item.conditions), saved: item.id } })
      status.value = action === 'update' ? '当前视图条件已更新' : action === 'rename' ? '已重命名，条件保持不变' : '视图已保存'
    }
    await client.invalidateQueries({ queryKey: savedSearchKey })
  } catch (e) { error.value = e instanceof Error && !(typeof e === 'object' && 'status' in e) ? e.message : displayError(e) }
  finally { busy.value = false }
}
</script>

<template>
  <section class="search-page">
    <header>
      <h1>搜索</h1><p class="muted">
        通过标题、普通正文、文件名、标签和时间找回内容；敏感正文不参与搜索。
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
          v-model="form.q"
          placeholder="搜索标题、正文、文件名或标签"
          autocomplete="off"
        >
      </label>
      <div class="filter-row">
        <label class="field">区域<select v-model="form.lifecycle"><option value="">全部</option><option :value="Lifecycle.TEMPORARY">临时区</option><option :value="Lifecycle.PERMANENT">长期区</option></select></label>
        <label class="field">时间<select
          v-model="form.time"
          aria-label="时间"
        ><option value="all">全部</option><option value="24h">24 小时</option><option value="7d">最近 7 天</option><option value="30d">最近 30 天</option><option value="custom">自定义范围</option></select></label>
        <label class="toggle"><input
          v-model="form.favorite"
          type="checkbox"
        > 仅收藏</label>
        <button
          class="button primary submit"
          type="submit"
        >
          搜索
        </button>
      </div>
      <div
        v-if="form.time === 'custom'"
        class="advanced"
      >
        <label class="field">起始时间（包含）<input
          v-model="form.from"
          type="datetime-local"
          required
        ></label>
        <label class="field">结束时间（不包含）<input
          v-model="form.to"
          type="datetime-local"
          required
        ></label>
        <label class="field">时区<select
          v-model="form.timezone"
          aria-label="时区"
        ><option value="Asia/Shanghai">中国标准时间（UTC+08:00）</option><option value="UTC">UTC（+00:00）</option></select></label>
        <p class="muted">
          按创建时间筛选：包含起点，不包含终点；相对时间为滚动的 24 小时 / 7 天 / 30 天。
        </p>
      </div>
      <details>
        <summary><SlidersHorizontal aria-hidden="true" />更多筛选</summary><div class="advanced">
          <label class="field">内容类型<select
            v-model="form.type"
            aria-label="内容类型"
          ><option value="">全部</option><option value="TEXT">文本</option><option value="MARKDOWN">Markdown</option><option value="CODE">代码</option></select></label><fieldset>
            <legend>标签</legend><label
              v-for="tag in tags.data.value"
              :key="tag.id"
            ><input
              v-model="form.tagIds"
              type="checkbox"
              :value="tag.id"
            > {{ tag.name }}</label>
            <label
              v-for="id in form.tagIds.filter(id => !tags.data.value?.some(tag => tag.id === id))"
              :key="id"
              class="error"
            ><input
              v-model="form.tagIds"
              type="checkbox"
              :value="id"
            >不可用标签（取消勾选后更新视图）</label>
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
    <section
      class="filters panel"
      aria-label="保存搜索"
    >
      <h2>{{ current ? `当前视图：${current.name}` : '保存搜索' }}</h2>
      <p class="muted">
        仅保存条件，打开时重新查询；修改筛选不会自动更新保存视图。
      </p>
      <nav aria-label="已保存视图">
        <RouterLink
          v-for="item in saved.data.value"
          :key="item.id"
          :to="{ name: 'search', query: { saved: item.id } }"
        >
          {{ item.name }}{{ item.invalidReason ? '（条件失效）' : '' }}
        </RouterLink>
      </nav>
      <p
        v-if="saved.error.value"
        class="error"
        role="alert"
      >
        保存视图读取失败 <button
          class="button"
          @click="saved.refetch()"
        >
          重试
        </button>
      </p>
      <label class="field">视图名称<input
        v-model="name"
        maxlength="100"
        placeholder="例如：本周收藏代码"
      ></label>
      <div class="saved-actions">
        <button
          class="button"
          :disabled="busy || !!validation || !name.trim()"
          @click="manage('create')"
        >
          另存为新视图
        </button>
        <template v-if="current">
          <button
            class="button"
            :disabled="busy || !name.trim()"
            @click="manage('rename')"
          >
            重命名
          </button>
          <button
            class="button primary"
            :disabled="busy || !!validation"
            @click="manage('update')"
          >
            更新当前视图条件
          </button>
          <button
            class="button danger"
            :disabled="busy"
            @click="manage('delete')"
          >
            删除视图
          </button>
        </template>
      </div>
      <p
        v-if="error"
        class="error"
        role="alert"
      >
        {{ error }}
      </p><p
        v-if="status"
        role="status"
      >
        {{ status }}
      </p>
    </section>
    <p
      v-if="appliedError"
      class="error"
      role="alert"
    >
      {{ appliedError }} <button
        v-if="tags.error.value"
        class="button"
        @click="tags.refetch()"
      >
        重试标签
      </button>
    </p>
    <MessageFeed
      v-else
      :filters="filters"
      empty-text="没有找到匹配内容"
    />
  </section>
</template>

<style scoped>
.saved-actions,nav{display:flex;flex-wrap:wrap;gap:.6rem}h2{font-size:1rem;margin:0}nav a{overflow-wrap:anywhere}.search-page{container-type:inline-size;display:grid;gap:1rem;min-width:0}h1,p{margin:.2rem 0}.filters{display:grid;gap:.85rem;min-width:0;padding:1rem}.main-search{display:grid;grid-template-columns:auto minmax(0,1fr);align-items:center;gap:.65rem;min-width:0;min-height:48px;padding:0 .9rem;border:1px solid var(--border-default);border-radius:12px;background:var(--surface-soft);color:var(--text-tertiary)}.main-search:focus-within{border-color:var(--accent-primary);box-shadow:0 0 0 3px var(--accent-primary-soft)}.main-search svg{width:1.05rem;height:1.05rem}.main-search input{min-width:0;width:100%;border:0;outline:0;background:transparent;color:var(--text-primary)}.main-search input::placeholder{color:var(--text-tertiary)}.filter-row{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr) auto auto;align-items:end;gap:.75rem;min-width:0}.toggle{display:flex;align-items:center;gap:.35rem;min-height:42px;white-space:nowrap}.submit{min-width:82px}.filters details,.filters .error{min-width:0}.filters summary{display:inline-flex;align-items:center;gap:.4rem;cursor:pointer;color:var(--text-secondary);font-size:.8rem;font-weight:650}.filters summary svg{width:.9rem;height:.9rem}.advanced{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,2fr);gap:1rem;margin-top:.8rem}.filters fieldset{display:flex;min-width:0;gap:.7rem;flex-wrap:wrap;border:1px solid var(--border-default);border-radius:var(--radius-sm)}.filters .error{margin:0;font-size:.78rem}
@container(max-width:650px){.filters{padding:.8rem}.main-search{min-height:46px;padding-inline:.75rem}.filter-row{grid-template-columns:minmax(0,1fr) minmax(0,1fr)}.toggle{min-height:40px}.submit{width:100%}.advanced{grid-template-columns:minmax(0,1fr)}}
@container(max-width:390px){.filter-row{grid-template-columns:minmax(0,1fr)}.toggle{min-height:auto}.submit{margin-top:.1rem}}
</style>
