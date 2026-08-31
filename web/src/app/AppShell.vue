<script setup lang="ts">
import { computed, onMounted, onUnmounted } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { useRoute, useRouter } from 'vue-router'
import { DefaultService } from '@/api/generated'
import { queryKeys } from '@/shared/api/queryKeys'
import { queryClient } from './queryClient'
import { realtimeConnectionState, startRealtime } from './realtime'
import { useUiStore } from './stores/ui'
import AppSidebar from './shell/AppSidebar.vue'
import AppTopbar from './shell/AppTopbar.vue'
import MobileHeader from './shell/MobileHeader.vue'
import MobileBottomNav from './shell/MobileBottomNav.vue'
import MobileMoreMenu from './shell/MobileMoreMenu.vue'
import MessageDetailSurface from '@/features/messages/components/detail/MessageDetailSurface.vue'
import { useAuthStore } from '@/features/auth/store'
import SessionsPanel from '@/features/sessions/SessionsPanel.vue'
import UploadQueue from '@/features/uploads/components/UploadQueue.vue'
import { uploadManager } from '@/features/uploads/manager'
import { hasActiveTransfers, visibleUploads } from '@/features/uploads/store'
import PWAUpdatePrompt from './PWAUpdatePrompt.vue'

const auth = useAuthStore()
const ui = useUiStore()
const route = useRoute()
const router = useRouter()
const uploadCount = computed(() => visibleUploads.value.filter((item) => item.status !== 'COMPLETED').length)
const shellMode = computed(() => route.meta.shell === 'full' ? 'full' : 'workspace')
const devices = useQuery({ queryKey: queryKeys.sessions.devices(), queryFn: () => DefaultService.listDevices() })
const adminStatus = useQuery({
  queryKey: queryKeys.admin.status(),
  queryFn: () => DefaultService.getAdminStatus(),
  enabled: computed(() => auth.user?.isAdmin === true),
  refetchInterval: 30_000,
})

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
  ui.uploadQueueOpen = false
  ui.sessionsOpen = true
}
function openUploads() {
  ui.mobileMoreOpen = false
  ui.sessionsOpen = false
  ui.uploadQueueOpen = true
}
function openMobileMore() {
  ui.sessionsOpen = false
  ui.uploadQueueOpen = false
  ui.mobileMoreOpen = true
}
</script>

<template>
  <div class="app-shell">
    <AppSidebar
      :upload-count="uploadCount"
      :active-transfers="hasActiveTransfers"
      :device-count="devices.data.value?.length"
      :realtime-state="realtimeConnectionState"
      :storage="adminStatus.data.value?.storage"
      @open-uploads="openUploads"
      @open-sessions="openSessions"
      @logout="logout"
    />
    <AppTopbar @open-sessions="openSessions" />
    <MobileHeader
      :upload-count="uploadCount"
      :active-transfers="hasActiveTransfers"
      @open-uploads="openUploads"
    />
    <div
      class="workspace"
      :class="shellMode"
    >
      <main class="content">
        <div
          class="content-frame"
          :class="{ constrained: shellMode === 'workspace' }"
        >
          <RouterView />
        </div>
      </main>
      <MessageDetailSurface v-if="shellMode === 'workspace'" />
    </div>
    <MobileBottomNav
      :more-open="ui.mobileMoreOpen"
      @open-uploads="openUploads"
      @open-more="openMobileMore"
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
.app-shell{display:grid;grid-template-columns:220px minmax(0,1fr);grid-template-rows:72px minmax(0,1fr);min-height:100vh;height:100vh;background:var(--surface-base);overflow:hidden}
.workspace{grid-column:2;display:grid;grid-template-columns:minmax(560px,1fr) minmax(360px,400px);min-height:0}.workspace.full{grid-template-columns:minmax(0,1fr)}.content{min-width:0;overflow:auto;padding:1.5rem clamp(1rem,3vw,2.5rem) 4rem}.content-frame{width:100%;margin-inline:auto}.content-frame.constrained{max-width:1040px}
@media(max-width:1179px){.app-shell{display:block;height:auto;min-height:100vh;overflow:visible}.app-sidebar,.app-topbar{display:none}.workspace{display:block}.content{min-height:calc(100vh - 62px);padding:.9rem .75rem calc(5.5rem + env(safe-area-inset-bottom));overflow:visible}}
</style>
