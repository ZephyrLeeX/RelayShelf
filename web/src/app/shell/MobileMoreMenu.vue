<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref } from 'vue'
import { useAuthStore } from '@/features/auth/store'
import { useTagsQuery } from '@/features/tags/queries'
import AccountButton from './AccountButton.vue'
import ThemeToggle from './ThemeToggle.vue'

const emit = defineEmits<{ close: [], openSessions: [], logout: [] }>()
const auth = useAuthStore()
const tags = useTagsQuery()
const sheet = ref<HTMLElement>()
let returnFocusTo: HTMLElement | null = null
function onKey(event: KeyboardEvent) { if (event.key === 'Escape') emit('close') }
onMounted(async () => {
  document.addEventListener('keydown', onKey)
  returnFocusTo = document.activeElement instanceof HTMLElement ? document.activeElement : null
  await nextTick()
  sheet.value?.focus()
})
onUnmounted(() => {
  document.removeEventListener('keydown', onKey)
  returnFocusTo?.focus()
})
</script>

<template>
  <Teleport to="body">
    <div
      class="more-backdrop"
      @click.self="emit('close')"
    >
      <section
        ref="sheet"
        class="more-sheet panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="more-title"
        tabindex="-1"
      >
        <div class="grab" /><header>
          <h2 id="more-title">
            我的
          </h2><button
            type="button"
            aria-label="关闭"
            @click="emit('close')"
          >
            ×
          </button>
        </header>
        <AccountButton
          :name="auth.user?.displayName || auth.user?.username"
          :device="auth.device?.name"
          @open="emit('openSessions')"
        />
        <nav aria-label="我的菜单">
          <p class="menu-label">
            标签
          </p>
          <RouterLink
            v-for="tag in tags.data.value"
            :key="tag.id"
            :to="`/tags/${tag.id}`"
            @click="emit('close')"
          >
            <i :style="{ backgroundColor:tag.color }" />{{ tag.name }}
          </RouterLink>
          <span
            v-if="!tags.isPending.value && !tags.data.value?.length"
            class="empty-tags"
          >尚无标签</span>
          <p class="menu-label">
            账户
          </p>
          <RouterLink
            to="/favorites"
            @click="emit('close')"
          >
            收藏
          </RouterLink>
          <RouterLink
            to="/trash"
            @click="emit('close')"
          >
            回收站
          </RouterLink>
          <button
            type="button"
            @click="emit('openSessions')"
          >
            设备与会话
          </button>
          <RouterLink
            v-if="auth.user?.isAdmin"
            to="/admin"
            @click="emit('close')"
          >
            管理
          </RouterLink>
        </nav>
        <div class="theme-row">
          <span>外观</span><ThemeToggle />
        </div>
        <button
          class="logout"
          type="button"
          @click="emit('logout')"
        >
          退出登录
        </button>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.more-backdrop{position:fixed;inset:0;z-index:60;display:none;align-items:flex-end;padding:1rem;background:var(--surface-overlay)}.more-sheet{width:min(100%,560px);max-height:min(78vh,680px);margin:0 auto;padding:.55rem 1rem 1rem;overflow:auto;border-radius:var(--radius-lg)}.grab{width:38px;height:4px;margin:.1rem auto .55rem;border-radius:999px;background:var(--border-strong)}header{display:flex;align-items:center;justify-content:space-between}h2{margin:.3rem 0;font-size:1.1rem}header button{width:40px;height:40px;border:0;background:transparent;font-size:1.3rem}nav{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:.4rem;padding:1rem 0;border-top:1px solid var(--border-default);border-bottom:1px solid var(--border-default)}nav a,nav button{display:flex;align-items:center;gap:.5rem;min-height:44px;padding:.55rem .7rem;border:0;border-radius:var(--radius-sm);background:var(--surface-soft);text-decoration:none;font-size:.8rem}nav i{width:.5rem;height:.5rem;border-radius:50%}.menu-label{grid-column:1/-1;margin:.45rem 0 0;color:var(--text-tertiary);font:700 .65rem/1 var(--font-mono);letter-spacing:.1em}.menu-label:first-child{margin-top:0}.empty-tags{grid-column:1/-1;padding:.65rem;color:var(--text-tertiary);font-size:.76rem}.theme-row{display:flex;align-items:center;justify-content:space-between;gap:.75rem;padding:1rem 0}.theme-row>span{font-size:.78rem;color:var(--text-secondary)}.logout{width:100%;min-height:44px;border:0;border-radius:var(--radius-sm);background:transparent;color:var(--state-danger)}
@media(max-width:1179px){.more-backdrop{display:flex}}@media(prefers-reduced-motion:no-preference){.more-sheet{animation:sheet-in .16s ease-out}@keyframes sheet-in{from{transform:translateY(18px);opacity:.8}}}
</style>
