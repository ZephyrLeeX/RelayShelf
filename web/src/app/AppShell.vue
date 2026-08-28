<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { queryClient } from './queryClient'
import { startRealtime } from './realtime'
import { useAuthStore } from '@/features/auth/store'
import { useTagsQuery } from '@/features/tags/queries'
import SessionsPanel from '@/features/sessions/SessionsPanel.vue'
import UploadQueue from '@/features/uploads/components/UploadQueue.vue'
import { uploadManager } from '@/features/uploads/manager'
import { hasActiveTransfers, uploadState, visibleUploads } from '@/features/uploads/store'
import PWAUpdatePrompt from './PWAUpdatePrompt.vue'

const auth = useAuthStore()
const route = useRoute()
const router = useRouter()
const search = ref(typeof route.query.q === 'string' ? route.query.q : '')
const sessionsOpen = ref(false)
const uploadCount = computed(() => visibleUploads.value.filter((item) => item.status !== 'COMPLETED').length)
const tags = useTagsQuery()

watch(() => route.query.q, (value) => {
  search.value = typeof value === 'string' ? value : ''
})

if (auth.device) startRealtime(queryClient, auth.device.id, () => {
  const redirect = router.currentRoute.value.fullPath
  auth.clear('guest')
  void router.replace({ name: 'login', query: { redirect } })
})
if (auth.user) void uploadManager.reconcile(auth.user.id)

function protectUnload(event: BeforeUnloadEvent) {
  if (!hasActiveTransfers.value) return
  event.preventDefault()
  event.returnValue = ''
}
onMounted(() => window.addEventListener('beforeunload', protectUnload))
onUnmounted(() => window.removeEventListener('beforeunload', protectUnload))

function submitSearch() {
  const q = search.value.trim()
  void router.push({ name: 'search', query: q ? { q } : {} })
}
async function logout() {
  try {
    await auth.logout()
  } catch {
    // Local private state is cleared even if the network cannot confirm logout.
  } finally {
    await router.replace('/login')
  }
}
</script>

<template>
  <div class="app-shell">
    <header class="topbar">
      <RouterLink
        class="brand"
        to="/temporary"
      >
        <img
          src="/favicon.svg"
          alt=""
          width="30"
          height="30"
        ><strong>RelayShelf</strong>
      </RouterLink>
      <form
        class="search"
        role="search"
        @submit.prevent="submitSearch"
      >
        <label
          class="sr-only"
          for="global-search"
        >搜索内容</label><input
          id="global-search"
          v-model="search"
          class="input"
          placeholder="搜索内容、文件名或标签"
        ><button
          class="button"
          type="submit"
        >
          搜索
        </button>
      </form>
      <button
        class="upload-entry"
        type="button"
        :aria-label="`打开上传任务，${uploadCount} 个进行中`"
        @click="uploadState.queueOpen = true"
      >
        <span
          class="upload-mark"
          :class="{ active: hasActiveTransfers }"
        />
        <span>上传</span><small v-if="visibleUploads.length">{{ visibleUploads.length }}</small>
      </button>
      <button
        class="account"
        type="button"
        @click="sessionsOpen = true"
      >
        <span>{{ auth.user?.displayName || auth.user?.username }}</span><small>{{ auth.device?.name }}</small>
      </button>
    </header>
    <nav
      class="primary"
      aria-label="主要区域"
    >
      <RouterLink to="/temporary">
        Temporary
      </RouterLink><RouterLink to="/permanent">
        Permanent
      </RouterLink>
    </nav>
    <div class="body">
      <aside class="secondary">
        <nav aria-label="次要区域">
          <RouterLink to="/favorites">
            收藏
          </RouterLink><RouterLink to="/trash">
            回收站
          </RouterLink>
        </nav>
        <section>
          <h2>标签</h2><nav>
            <RouterLink
              v-for="tag in tags.data.value"
              :key="tag.id"
              :to="`/tags/${tag.id}`"
            >
              <i :style="{ backgroundColor: tag.color }" />{{ tag.name }}
            </RouterLink>
          </nav><p
            v-if="!tags.isPending.value && !tags.data.value?.length"
            class="muted"
          >
            尚无标签
          </p>
        </section>
        <details class="mobile-tags">
          <summary>标签</summary><nav>
            <RouterLink
              v-for="tag in tags.data.value"
              :key="tag.id"
              :to="`/tags/${tag.id}`"
            >
              {{ tag.name }}
            </RouterLink>
          </nav>
        </details>
        <RouterLink
          v-if="auth.user?.isAdmin"
          to="/admin"
        >
          管理
        </RouterLink>
        <button
          class="logout"
          @click="logout"
        >
          退出登录
        </button>
      </aside>
      <main class="content">
        <RouterView />
      </main>
    </div>
    <SessionsPanel
      v-if="sessionsOpen"
      @close="sessionsOpen = false"
    />
    <UploadQueue
      v-if="uploadState.queueOpen"
      @close="uploadState.queueOpen = false"
    />
    <PWAUpdatePrompt />
  </div>
</template>

<style scoped>
.app-shell { min-height:100vh; }
.topbar { height:68px; position:sticky; top:0; z-index:20; display:grid; grid-template-columns:auto minmax(240px,560px) auto auto; gap:1rem; align-items:center; padding:.65rem max(1rem, calc((100vw - 1180px)/2)); background:color-mix(in srgb, var(--surface-raised) 92%, transparent); border-bottom:1px solid var(--border); backdrop-filter:blur(14px); }
.brand { display:flex; align-items:center; gap:.55rem; text-decoration:none; font-size:1.08rem; }.search { display:flex; gap:.45rem; }.account { border:0; background:transparent; text-align:right; }.account span,.account small { display:block; }.account small { color:var(--muted); max-width:180px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
.upload-entry{display:flex;align-items:center;gap:.4rem;border:1px solid var(--border);border-radius:999px;background:var(--surface-raised);min-height:38px;padding:.35rem .65rem}.upload-entry small{display:grid;place-items:center;min-width:1.25rem;height:1.25rem;border-radius:999px;background:var(--surface-soft);font-size:.7rem}.upload-mark{width:.55rem;height:.55rem;border-radius:50%;background:var(--muted)}.upload-mark.active{background:var(--accent);box-shadow:0 0 0 4px var(--accent-soft)}
.primary { display:flex; justify-content:center; gap:.5rem; padding:.75rem; border-bottom:1px solid var(--border); background:var(--surface-raised); }.primary a { min-width:130px; padding:.65rem 1.25rem; border-radius:999px; text-align:center; text-decoration:none; font-weight:650; }.primary a.router-link-active { background:var(--accent-soft); color:var(--accent-strong); }
.body { max-width:1180px; margin:0 auto; display:grid; grid-template-columns:190px minmax(0,1fr); gap:2rem; padding:1.5rem 1rem 4rem; }.secondary { position:sticky; top:155px; height:max-content; display:grid; gap:1.5rem; }.secondary nav { display:grid; gap:.25rem; }.secondary a,.logout { display:flex; align-items:center; gap:.5rem; min-height:40px; padding:.5rem .6rem; border:0; border-radius:var(--radius-sm); background:transparent; text-decoration:none; }.secondary a.router-link-active { background:var(--accent-soft); }.secondary h2 { font-size:.78rem; color:var(--muted); text-transform:uppercase; letter-spacing:.08em; }.secondary i { width:.55rem; height:.55rem; border-radius:50%; }.logout { color:var(--muted); }
.mobile-tags { display:none; }.content { min-width:0; max-width:820px; width:100%; }
@media(max-width:720px){.topbar{height:auto; grid-template-columns:1fr auto auto; padding:.6rem .75rem}.brand strong{display:none}.search{grid-row:2;grid-column:1/-1}.account span{display:none}.upload-entry>span:not(.upload-mark){display:none}.primary{position:sticky;top:113px;z-index:19;padding:.45rem}.primary a{min-width:0;flex:1}.body{display:block;padding:.8rem .7rem 5.5rem}.secondary{position:fixed;bottom:0;left:0;right:0;top:auto;z-index:25;display:flex;justify-content:space-around;gap:0;background:var(--surface-raised);border-top:1px solid var(--border);padding:.25rem}.secondary section,.secondary>a,.logout{display:none}.secondary>nav{display:flex;width:75%;justify-content:space-around}.secondary nav a{min-height:44px}.mobile-tags{display:block;position:relative;padding:.6rem}.mobile-tags summary{cursor:pointer}.mobile-tags nav{position:absolute;right:0;bottom:3rem;min-width:180px;max-height:45vh;overflow:auto;padding:.5rem;background:var(--surface-raised);border:1px solid var(--border);border-radius:var(--radius);box-shadow:var(--shadow)}.content{max-width:none}}
</style>
