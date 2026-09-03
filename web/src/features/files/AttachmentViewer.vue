<script setup lang="ts">
import { ChevronLeft, ChevronRight, Download, X, ZoomIn, ZoomOut } from '@lucide/vue'
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import type { AttachmentSummary } from '@/api/generated'
import { downloadURL, previewKind, previewURL, safeRasterMIMEs } from './preview'
import TextPreview from './TextPreview.vue'
import { apiErrorFromResponse, isStorageUnavailable } from '@/shared/api/errors'
import { notifyStorageUnavailable, storageAvailable } from '@/features/storage/runtime'

const props = defineProps<{ files: AttachmentSummary[]; currentId: string }>()
const emit = defineEmits<{ close: []; select: [id: string] }>()
const zoom = ref(1)
const fileError = ref('')
const previewReady = ref(false)
const viewer = ref<HTMLElement>()
let returnFocusTo: HTMLElement | null = null
let previewController: AbortController | undefined
const current = computed(() => props.files.find((file) => file.id === props.currentId))
const images = computed(() => props.files.filter((file) => safeRasterMIMEs.has(file.detectedMime)))
const imageIndex = computed(() => images.value.findIndex((file) => file.id === props.currentId))
watch(() => [props.currentId, current.value?.detectedMime] as const, async () => {
  previewController?.abort()
  previewController = new AbortController()
  fileError.value = ''
  previewReady.value = false
  if (!current.value || !['image', 'pdf', 'audio', 'video'].includes(previewKind(current.value))) return
  try {
    const response = await fetch(previewURL(current.value.id), {
      credentials: 'include', headers: { Range: 'bytes=0-0' }, signal: previewController.signal,
    })
    if (!response.ok && response.status !== 206) {
      const error = await apiErrorFromResponse(response)
      if (isStorageUnavailable(error)) { storageFailed(); return }
      throw error
    }
    await response.body?.cancel()
    previewReady.value = true
  } catch (cause) {
    if (cause instanceof DOMException && cause.name === 'AbortError') return
    if (isStorageUnavailable(cause)) storageFailed()
    else mediaFailed()
  }
}, { immediate: true })
function mediaFailed() {
  fileError.value = '文件预览暂不可用，请稍后重试。'
}
function storageFailed() {
  fileError.value = '存储服务当前不可用，请在服务恢复后重试。'
  notifyStorageUnavailable()
}
async function download(event: MouseEvent) {
  event.preventDefault()
  if (!current.value) return
  fileError.value = ''
  if (!storageAvailable.value) { storageFailed(); return }
  const url = downloadURL(current.value.id)
  try {
    const response = await fetch(url, { credentials: 'include', headers: { Range: 'bytes=0-0' } })
    if (!response.ok && response.status !== 206) {
      const error = await apiErrorFromResponse(response)
      if (isStorageUnavailable(error)) { storageFailed(); return }
      throw error
    }
    const link = document.createElement('a')
    link.href = url
    link.download = current.value.originalFilename
    link.click()
  } catch (cause) {
    if (isStorageUnavailable(cause)) storageFailed()
    else fileError.value = '文件下载失败，请稍后重试。'
  }
}
function navigate(delta: number) {
  if (imageIndex.value < 0 || images.value.length < 2) return
  const index = (imageIndex.value + delta + images.value.length) % images.value.length
  zoom.value = 1; emit('select', images.value[index].id)
}
function key(event: KeyboardEvent) {
  if (event.key === 'Escape') emit('close')
  if (event.key === 'ArrowLeft') navigate(-1)
  if (event.key === 'ArrowRight') navigate(1)
}
onMounted(async () => {
  document.addEventListener('keydown', key)
  returnFocusTo = document.activeElement instanceof HTMLElement ? document.activeElement : null
  await nextTick()
  viewer.value?.focus()
})
onUnmounted(() => {
  previewController?.abort()
  document.removeEventListener('keydown', key)
  returnFocusTo?.focus()
})
</script>

<template>
  <Teleport to="body">
    <div
      ref="viewer"
      class="viewer"
      role="dialog"
      aria-modal="true"
      aria-label="附件查看器"
      tabindex="-1"
    >
      <header v-if="current">
        <div><strong>{{ current.originalFilename }}</strong><small>{{ current.detectedMime }}</small></div><nav>
          <template v-if="previewKind(current) === 'image'">
            <button
              class="tool"
              aria-label="缩小"
              @click="zoom = Math.max(.25, zoom - .25)"
            >
              <ZoomOut aria-hidden="true" />
            </button><button
              class="tool reset"
              @click="zoom = 1"
            >
              {{ Math.round(zoom * 100) }}%
            </button><button
              class="tool"
              aria-label="放大"
              @click="zoom = Math.min(4, zoom + .25)"
            >
              <ZoomIn aria-hidden="true" />
            </button>
          </template>
          <a
            class="tool download"
            :href="downloadURL(current.id)"
            :aria-label="`下载 ${current.originalFilename}`"
            @click="download"
          ><Download aria-hidden="true" />下载</a><button
            class="tool close"
            aria-label="关闭"
            @click="emit('close')"
          >
            <X aria-hidden="true" />
          </button>
        </nav>
      </header>
      <main v-if="current">
        <section
          v-if="fileError"
          class="file-error"
          role="alert"
        >
          <strong>文件暂时无法读取</strong><p>{{ fileError }}</p>
        </section>
        <img
          v-else-if="previewReady && previewKind(current) === 'image'"
          class="original"
          :src="previewURL(current.id)"
          :alt="current.originalFilename"
          :style="{ transform: `scale(${zoom})` }"
          @error="mediaFailed"
        >
        <iframe
          v-else-if="previewReady && previewKind(current) === 'pdf'"
          :src="previewURL(current.id)"
          title="PDF 预览"
          @error="mediaFailed"
        />
        <audio
          v-else-if="previewReady && previewKind(current) === 'audio'"
          controls
          preload="metadata"
          :src="previewURL(current.id)"
          @error="mediaFailed"
        />
        <video
          v-else-if="previewReady && previewKind(current) === 'video'"
          controls
          preload="metadata"
          :src="previewURL(current.id)"
          @error="mediaFailed"
        />
        <TextPreview
          v-else-if="previewKind(current) === 'text'"
          :file="current"
        />
        <section
          v-else
          class="unsupported"
        >
          <span>文件</span><h2>此文件仅支持下载</h2><p>Office、HTML、SVG、XML 和未知类型不会在本站内直接打开。</p><a
            class="button primary"
            :href="downloadURL(current.id)"
            @click="download"
          >下载文件</a>
        </section>
        <button
          v-if="previewKind(current) === 'image' && images.length > 1"
          class="previous"
          aria-label="上一张"
          @click="navigate(-1)"
        >
          <ChevronLeft aria-hidden="true" />
        </button><button
          v-if="previewKind(current) === 'image' && images.length > 1"
          class="next"
          aria-label="下一张"
          @click="navigate(1)"
        >
          <ChevronRight aria-hidden="true" />
        </button>
      </main>
    </div>
  </Teleport>
</template>

<style scoped>
.viewer{position:fixed;inset:0;z-index:90;display:grid;grid-template-rows:auto minmax(0,1fr);background:rgb(12 18 16 / .96);color:#f3f7f5}.viewer header{display:flex;justify-content:space-between;align-items:center;gap:1rem;padding:.65rem 1rem;border-bottom:1px solid rgb(255 255 255 / .14)}header>div{display:grid;min-width:0}header strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}header small{color:#aebbb5}nav{display:flex;align-items:center;gap:.35rem}.tool{display:grid;place-items:center;min-width:42px;height:42px;padding:0 .65rem;border:1px solid rgb(255 255 255 / .18);border-radius:8px;background:rgb(255 255 255 / .08);color:inherit;text-decoration:none}.tool svg,.previous svg,.next svg{width:1.1rem;height:1.1rem}.reset,.download{font-size:.8rem}.download{display:flex;gap:.35rem}main{position:relative;display:grid;place-items:center;overflow:auto}.file-error{max-width:440px;padding:1.4rem;border:1px solid rgb(255 190 90 / .35);border-radius:12px;background:rgb(255 190 90 / .1);text-align:center}.file-error p{color:#d7dedb}.original{max-width:calc(100vw - 7rem);max-height:calc(100vh - 6rem);object-fit:contain;transition:transform .12s;transform-origin:center}iframe{width:100%;height:100%;border:0;background:white}audio{width:min(620px,90vw)}video{max-width:90vw;max-height:80vh}.previous,.next{position:fixed;top:50%;display:grid;place-items:center;width:48px;height:64px;border:0;border-radius:10px;background:rgb(255 255 255 / .1);color:white}.previous{left:1rem}.next{right:1rem}.unsupported{text-align:center;max-width:430px;padding:2rem}.unsupported span{display:grid;place-items:center;width:72px;height:72px;margin:auto;border:1px solid rgb(255 255 255 / .2);border-radius:16px;font:700 .8rem var(--font-mono)}.unsupported p{color:#aebbb5;line-height:1.55}.unsupported .button{display:inline-flex}.text-preview{width:100%;height:100%}
@media(max-width:600px){.viewer header{padding:.5rem}.viewer header small{display:none}.tool{min-width:40px;padding:0 .45rem}.download{display:none}.original{max-width:100vw;max-height:calc(100vh - 60px)}.previous{left:.35rem}.next{right:.35rem}}
</style>
