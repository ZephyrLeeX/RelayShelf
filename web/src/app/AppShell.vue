<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
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
import StorageStatusBanner from '@/features/storage/StorageStatusBanner.vue'
import { setStorageRuntimeStatus } from '@/features/storage/runtime'
import ToastViewport from '@/shared/ui/ToastViewport.vue'

const auth = useAuthStore()
const ui = useUiStore()
const route = useRoute()
const router = useRouter()
const DETAIL_WIDTH_KEY = 'relayshelf:detail-width'
const detailWidth = ref(400)
const desktop = ref(false)
let preferredDetailWidth = 400
let desktopMedia: MediaQueryList | undefined
const uploadCount = computed(() => visibleUploads.value.filter((item) => item.status !== 'COMPLETED').length)
const shellMode = computed(() => route.meta.shell === 'full' ? 'full' : 'workspace')
const devices = useQuery({ queryKey: queryKeys.sessions.devices(), queryFn: () => DefaultService.listDevices() })
const storageStatus = useQuery({
  queryKey: queryKeys.storage.status(),
  queryFn: () => DefaultService.getStorageRuntimeStatus(),
  enabled: computed(() => Boolean(auth.user)),
  refetchInterval: 15_000,
})
watch(storageStatus.data, (status, previous) => {
  setStorageRuntimeStatus(status)
  if (previous?.healthy !== false && status?.healthy === false) uploadManager.pauseForStorage()
}, { immediate: true })

if (auth.device) startRealtime(queryClient, auth.device.id, () => {
  const redirect = router.currentRoute.value.fullPath
  auth.clear('guest')
  void router.replace({ name: 'login', query: { redirect } })
})
if (auth.user) void uploadManager.reconcile(auth.user.id)
// UploadManager stays independent from Pinia; auth bootstrap remains the
// bounded path used to refresh CSRF after an upload request expires.
uploadManager.setCsrfRefresh(() => auth.bootstrap(true))

function detailWidthBounds() {
  const workspaceWidth = Math.max(0, window.innerWidth - 220)
  return { min: Math.min(320, Math.max(280, workspaceWidth * .3)), max: Math.max(280, Math.min(560, workspaceWidth - 528)) }
}
function clampDetailWidth(width: number) {
  const bounds = detailWidthBounds()
  detailWidth.value = Math.round(Math.min(bounds.max, Math.max(bounds.min, width)))
}
function restoreDetailWidth() {
  const stored = Number.parseFloat(localStorage.getItem(DETAIL_WIDTH_KEY) ?? '')
  preferredDetailWidth = Number.isFinite(stored) ? stored : 400
  clampDetailWidth(preferredDetailWidth)
}
function onViewportChange() {
  desktop.value = desktopMedia?.matches ?? window.innerWidth >= 1180
  clampDetailWidth(preferredDetailWidth)
}
function resizeDetail(event: PointerEvent) {
  if (!desktop.value || event.button !== 0) return
  const startX = event.clientX
  const startWidth = detailWidth.value
  const target = event.currentTarget as HTMLElement
  target.setPointerCapture?.(event.pointerId)
  document.documentElement.classList.add('resizing-detail')
  const move = (moveEvent: PointerEvent) => {
    clampDetailWidth(startWidth + startX - moveEvent.clientX)
    preferredDetailWidth = detailWidth.value
  }
  const finish = () => {
    window.removeEventListener('pointermove', move)
    window.removeEventListener('pointerup', finish)
    window.removeEventListener('pointercancel', finish)
    document.documentElement.classList.remove('resizing-detail')
    localStorage.setItem(DETAIL_WIDTH_KEY, String(detailWidth.value))
  }
  window.addEventListener('pointermove', move)
  window.addEventListener('pointerup', finish)
  window.addEventListener('pointercancel', finish)
}
function resizeDetailByKeyboard(event: KeyboardEvent) {
  if (!desktop.value || !['ArrowLeft', 'ArrowRight', 'Home', 'End'].includes(event.key)) return
  event.preventDefault()
  const bounds = detailWidthBounds()
  if (event.key === 'Home') detailWidth.value = Math.round(bounds.min)
  else if (event.key === 'End') detailWidth.value = Math.round(bounds.max)
  else clampDetailWidth(detailWidth.value + (event.key === 'ArrowLeft' ? 24 : -24))
  preferredDetailWidth = detailWidth.value
  localStorage.setItem(DETAIL_WIDTH_KEY, String(detailWidth.value))
}
function protectUnload(event: BeforeUnloadEvent) {
  if (!hasActiveTransfers.value) return
  event.preventDefault()
  event.returnValue = ''
}
onMounted(() => {
  desktopMedia = window.matchMedia?.('(min-width: 1180px)')
  restoreDetailWidth()
  onViewportChange()
  desktopMedia?.addEventListener('change', onViewportChange)
  window.addEventListener('resize', onViewportChange)
  window.addEventListener('beforeunload', protectUnload)
})
onUnmounted(() => {
  desktopMedia?.removeEventListener('change', onViewportChange)
  window.removeEventListener('resize', onViewportChange)
  window.removeEventListener('beforeunload', protectUnload)
  setStorageRuntimeStatus(undefined)
})

async function logout() {
  ui.mobileMoreOpen = false
  try {
    await auth.logout()
  } catch {
    // Local private state is cleared even if the network cannot confirm logout.
  } finally {
    setStorageRuntimeStatus(undefined)
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
      :storage="storageStatus.data.value"
      @open-uploads="openUploads"
      @open-sessions="openSessions"
      @logout="logout"
    />
    <AppTopbar />
    <MobileHeader
      :upload-count="uploadCount"
      :active-transfers="hasActiveTransfers"
      @open-uploads="openUploads"
    />
    <div class="workspace-shell">
      <StorageStatusBanner />
      <div
        class="workspace"
        :class="shellMode"
        :style="shellMode === 'workspace' ? { '--detail-width': `${detailWidth}px` } : undefined"
      >
        <main class="content">
          <div
            class="content-frame"
            :class="{ constrained: shellMode === 'workspace' }"
          >
            <RouterView />
          </div>
        </main>
        <div
          v-if="shellMode === 'workspace'"
          class="column-resizer"
          role="separator"
          aria-label="调整内容详情栏宽度"
          aria-orientation="vertical"
          :aria-valuenow="detailWidth"
          :aria-valuemin="Math.round(detailWidthBounds().min)"
          :aria-valuemax="Math.round(detailWidthBounds().max)"
          tabindex="0"
          @pointerdown="resizeDetail"
          @keydown="resizeDetailByKeyboard"
        />
        <MessageDetailSurface v-if="shellMode === 'workspace'" />
      </div>
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
    <ToastViewport />
  </div>
</template>

<style scoped>
.app-shell{display:grid;grid-template-columns:220px minmax(0,1fr);grid-template-rows:72px minmax(0,1fr);min-height:100vh;height:100vh;background:var(--surface-base);overflow:hidden}
.workspace-shell{grid-column:2;display:flex;min-width:0;min-height:0;flex-direction:column}.workspace{display:grid;grid-template-columns:minmax(0,1fr) 8px minmax(0,var(--detail-width,400px));min-width:0;min-height:0;flex:1}.workspace.full{grid-template-columns:minmax(0,1fr)}.content{min-width:0;overflow:auto;padding:1.5rem clamp(1rem,3vw,2.5rem) 4rem}.content-frame{width:100%;min-width:0;margin-inline:auto}.content-frame.constrained{max-width:1040px}.column-resizer{position:relative;z-index:7;min-width:0;cursor:col-resize;touch-action:none}.column-resizer::before{content:'';position:absolute;top:0;bottom:0;left:3px;width:1px;background:var(--border-default)}.column-resizer::after{content:'';position:absolute;inset:0 -3px}.column-resizer:hover::before,.column-resizer:focus-visible::before{width:2px;background:var(--accent-primary)}
@media(max-width:1179px){.app-shell{display:block;width:100%;height:auto;min-height:100vh;overflow-x:clip}.app-sidebar,.app-topbar{display:none}.workspace{display:block;min-width:0}.column-resizer{display:none}.content{min-width:0;min-height:calc(100vh - 62px);padding:.9rem clamp(.65rem,3vw,.9rem) calc(5.5rem + env(safe-area-inset-bottom));overflow:visible}}
</style>
