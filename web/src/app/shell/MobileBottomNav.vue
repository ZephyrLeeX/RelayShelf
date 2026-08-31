<script setup lang="ts">
import { Archive, Clock3, Search, Upload, UserRound } from '@lucide/vue'
defineProps<{ moreOpen: boolean }>()
defineEmits<{ openUploads: [], openMore: [] }>()
</script>

<template>
  <nav
    class="mobile-bottom-nav"
    aria-label="移动端主要区域"
  >
    <RouterLink to="/temporary">
      <Clock3 aria-hidden="true" />临时
    </RouterLink>
    <RouterLink to="/permanent">
      <Archive aria-hidden="true" />长期
    </RouterLink>
    <RouterLink to="/search">
      <Search aria-hidden="true" />搜索
    </RouterLink>
    <button
      type="button"
      @click="$emit('openUploads')"
    >
      <Upload aria-hidden="true" />上传
    </button>
    <button
      type="button"
      :class="{ active: moreOpen }"
      :aria-expanded="moreOpen"
      @click="$emit('openMore')"
    >
      <UserRound aria-hidden="true" />我的
    </button>
  </nav>
</template>

<style scoped>
.mobile-bottom-nav{display:none}.mobile-bottom-nav a,.mobile-bottom-nav button{min-width:0;border:0;background:transparent;text-decoration:none;color:var(--text-secondary);font-size:.68rem}.mobile-bottom-nav svg{display:block;width:1rem;height:1rem;margin:0 auto .2rem}.mobile-bottom-nav a.router-link-active,.mobile-bottom-nav button.active{color:var(--accent-primary-hover);font-weight:700}
@media(max-width:1179px){.mobile-bottom-nav{position:fixed;z-index:35;right:0;bottom:0;left:0;display:grid;grid-template-columns:repeat(5,1fr);padding:.35rem max(.35rem,env(safe-area-inset-right)) calc(.35rem + env(safe-area-inset-bottom)) max(.35rem,env(safe-area-inset-left));border-top:1px solid var(--border-default);background:color-mix(in srgb,var(--surface-raised) 96%,transparent);box-shadow:0 -8px 24px rgb(25 28 45/.08);backdrop-filter:blur(18px)}.mobile-bottom-nav a,.mobile-bottom-nav button{min-height:48px;padding:.25rem;display:grid;place-content:center;text-align:center}}
</style>
