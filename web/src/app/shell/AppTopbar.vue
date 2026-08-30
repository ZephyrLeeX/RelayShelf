<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/features/auth/store'
import AccountButton from './AccountButton.vue'
import ThemeToggle from './ThemeToggle.vue'

defineEmits<{ openSessions: [] }>()
const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const search = ref(typeof route.query.q === 'string' ? route.query.q : '')

watch(() => route.query.q, (value) => { search.value = typeof value === 'string' ? value : '' })
function submitSearch() {
  const q = search.value.trim()
  void router.push({ name: 'search', query: q ? { q } : {} })
}
</script>

<template>
  <header class="app-topbar">
    <form
      class="global-search"
      role="search"
      @submit.prevent="submitSearch"
    >
      <label
        class="sr-only"
        for="global-search"
      >搜索内容</label>
      <span aria-hidden="true">⌕</span>
      <input
        id="global-search"
        v-model="search"
        placeholder="搜索内容、文件名或标签"
      >
      <kbd>Enter</kbd>
    </form>
    <div class="topbar-actions">
      <ThemeToggle />
      <AccountButton
        compact
        :name="auth.user?.displayName || auth.user?.username"
        :device="auth.device?.name"
        @open="$emit('openSessions')"
      />
    </div>
  </header>
</template>

<style scoped>
.app-topbar{grid-column:2/-1;display:flex;align-items:center;justify-content:space-between;gap:1rem;padding:.75rem 1rem;border-bottom:1px solid var(--border-default);background:color-mix(in srgb,var(--surface-raised) 91%,transparent);backdrop-filter:blur(16px)}
.global-search{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:.55rem;width:min(100%,560px);min-height:42px;padding:0 .75rem;border:1px solid var(--border-default);border-radius:12px;background:var(--surface-soft);color:var(--text-tertiary)}.global-search:focus-within{border-color:var(--accent-primary);box-shadow:0 0 0 3px var(--accent-primary-soft)}.global-search input{min-width:0;border:0;outline:0;background:transparent;color:var(--text-primary)}.global-search kbd{padding:.15rem .35rem;border:1px solid var(--border-default);border-radius:4px;background:var(--surface-raised);font:600 .62rem var(--font-mono)}
.topbar-actions{display:flex;align-items:center;gap:.6rem}
</style>
