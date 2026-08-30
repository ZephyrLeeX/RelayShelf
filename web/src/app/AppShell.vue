<script setup lang="ts">
import { computed, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { queryClient } from './queryClient'
import { startRealtime } from './realtime'
import { useUiStore } from './stores/ui'
import AppSidebar from './shell/AppSidebar.vue'
import AppTopbar from './shell/AppTopbar.vue'
import MobileHeader from './shell/MobileHeader.vue'
import MobileBottomNav from './shell/MobileBottomNav.vue'
import MobileMoreMenu from './shell/MobileMoreMenu.vue'
import { useAuthStore } from '@/features/auth/store'
import SessionsPanel from '@/features/sessions/SessionsPanel.vue'
import UploadQueue from '@/features/uploads/components/UploadQueue.vue'
import { uploadManager } from '@/features/uploads/manager'
import { hasActiveTransfers, visibleUploads } from '@/features/uploads/store'
import PWAUpdatePrompt from './PWAUpdatePrompt.vue'

const auth = useAuthStore()
const ui = useUiStore()
const router = useRouter()
const uploadCount = computed(() => visibleUploads.value.filter((item) => item.status !== 'COMPLETED').length)

if (auth.device) startRealtime(queryClient, auth.device.id, () => {
  const redirect = router.currentRoute.value.fullPath
  auth.clear('guest')
  void router.replace({ name: 'login', query: { redirect } })
})
if (auth.user) void uploadManager.reconcile(auth.user.id)
// UploadManager stays independent from Pinia; auth bootstrap remains the
// bounded path used to refresh CSRF after an upload request expires.
uploadManager.setCsrfRefresh(() => auth.bootstrap(true))

function protectUnload(event: BeforeUnloadEvent) {
  if (!hasActiveTransfers.value) return
  event.preventDefault()
  event.returnValue = ''
}
onMounted(() => window.addEventListener('beforeunload', protectUnload))
onUnmounted(() => window.removeEventListener('beforeunload', protectUnload))

async function logout() {
  ui.mobileMoreOpen = false
  try {
    await auth.logout()
  } catch {
    // Local private state is cleared even if the network cannot confirm logout.
  } finally {
    await router.replace('/login')
  }
}
function openSessions() {
  ui.mobileMoreOpen = false
  ui.sessionsOpen = true
}
</script>

<template>
  <div class="app-shell">
    <AppSidebar
      :upload-count="uploadCount"
      :active-transfers="hasActiveTransfers"
      @open-uploads="ui.uploadQueueOpen = true"
      @open-sessions="openSessions"
      @logout="logout"
    />
    <AppTopbar @open-sessions="openSessions" />
    <MobileHeader
      :upload-count="uploadCount"
      :active-transfers="hasActiveTransfers"
      @open-uploads="ui.uploadQueueOpen = true"
    />
    <div class="workspace">
      <main class="content">
        <RouterView />
      </main>
      <aside
        class="detail-placeholder"
        aria-label="内容详情"
      >
        <div aria-hidden="true">
          ⌁
        </div>
        <strong>内容详情</strong>
        <p>选择一条内容后在此查看详情。</p>
      </aside>
    </div>
    <MobileBottomNav
      @open-uploads="ui.uploadQueueOpen = true"
      @open-more="ui.mobileMoreOpen = true"
    />
    <MobileMoreMenu
      v-if="ui.mobileMoreOpen"
      @close="ui.mobileMoreOpen = false"
      @open-sessions="openSessions"
      @logout="logout"
    />
    <SessionsPanel
      v-if="ui.sessionsOpen"
      @close="ui.sessionsOpen = false"
    />
    <UploadQueue
      v-if="ui.uploadQueueOpen"
      @close="ui.uploadQueueOpen = false"
    />
    <PWAUpdatePrompt />
  </div>
</template>

<style scoped>
.app-shell{display:grid;grid-template-columns:220px minmax(560px,1fr) clamp(400px,31vw,480px);grid-template-rows:72px minmax(0,1fr);min-height:100vh;height:100vh;background:var(--surface-base);overflow:hidden}
.workspace{grid-column:2/-1;display:grid;grid-template-columns:minmax(560px,1fr) clamp(400px,31vw,480px);min-height:0}.content{min-width:0;overflow:auto;padding:1.5rem clamp(1rem,3vw,2.5rem) 4rem}.detail-placeholder{display:grid;place-content:center;justify-items:center;min-width:0;padding:2rem;border-left:1px solid var(--border-default);background:var(--surface-raised);color:var(--text-tertiary);text-align:center}.detail-placeholder div{display:grid;place-items:center;width:48px;height:48px;margin-bottom:.8rem;border:1px solid var(--border-default);border-radius:14px;background:var(--surface-soft);color:var(--accent-primary);font-size:1.4rem}.detail-placeholder strong{color:var(--text-secondary);font-size:.85rem}.detail-placeholder p{max-width:16rem;margin:.35rem 0;font-size:.75rem;line-height:1.5}
@media(max-width:1179px){.app-shell{display:block;height:auto;min-height:100vh;overflow:visible}.app-sidebar,.app-topbar{display:none}.workspace{display:block}.content{min-height:calc(100vh - 62px);padding:.9rem .75rem calc(5.5rem + env(safe-area-inset-bottom));overflow:visible}.detail-placeholder{display:none}}
</style>
