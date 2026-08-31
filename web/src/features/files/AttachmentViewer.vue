<script setup lang="ts">
import { computed, nextTick, onMounted, onUnmounted, ref } from 'vue'
import type { AttachmentSummary } from '@/api/generated'
import { downloadURL, previewKind, previewURL, safeRasterMIMEs } from './preview'
import TextPreview from './TextPreview.vue'

const props = defineProps<{ files: AttachmentSummary[]; currentId: string }>()
const emit = defineEmits<{ close: []; select: [id: string] }>()
const zoom = ref(1)
const viewer = ref<HTMLElement>()
let returnFocusTo: HTMLElement | null = null
const current = computed(() => props.files.find((file) => file.id === props.currentId))
const images = computed(() => props.files.filter((file) => safeRasterMIMEs.has(file.detectedMime)))
const imageIndex = computed(() => images.value.findIndex((file) => file.id === props.currentId))
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
              −
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
              ＋
            </button>
          </template>
          <a
            class="tool download"
            :href="downloadURL(current.id)"
          >下载</a><button
            class="tool close"
            aria-label="关闭"
            @click="emit('close')"
          >
            ×
          </button>
        </nav>
      </header>
      <main v-if="current">
        <img
          v-if="previewKind(current) === 'image'"
          class="original"
          :src="previewURL(current.id)"
          :alt="current.originalFilename"
          :style="{ transform: `scale(${zoom})` }"
        >
        <iframe
          v-else-if="previewKind(current) === 'pdf'"
          :src="previewURL(current.id)"
          title="PDF 预览"
        />
        <audio
          v-else-if="previewKind(current) === 'audio'"
          controls
          preload="metadata"
          :src="previewURL(current.id)"
        />
        <video
          v-else-if="previewKind(current) === 'video'"
          controls
          preload="metadata"
          :src="previewURL(current.id)"
        />
        <TextPreview
          v-else-if="previewKind(current) === 'text'"
          :file="current"
        />
        <section
          v-else
          class="unsupported"
        >
          <span>FILE</span><h2>此文件仅支持下载</h2><p>Office、HTML、SVG、XML 和未知类型不会在本站内直接打开。</p><a
            class="button primary"
            :href="downloadURL(current.id)"
          >下载文件</a>
        </section>
        <button
          v-if="previewKind(current) === 'image' && images.length > 1"
          class="previous"
          aria-label="上一张"
          @click="navigate(-1)"
        >
          ‹
        </button><button
          v-if="previewKind(current) === 'image' && images.length > 1"
          class="next"
          aria-label="下一张"
          @click="navigate(1)"
        >
          ›
        </button>
      </main>
    </div>
  </Teleport>
</template>

<style scoped>
.viewer{position:fixed;inset:0;z-index:90;display:grid;grid-template-rows:auto minmax(0,1fr);background:rgb(12 18 16 / .96);color:#f3f7f5}.viewer header{display:flex;justify-content:space-between;align-items:center;gap:1rem;padding:.65rem 1rem;border-bottom:1px solid rgb(255 255 255 / .14)}header>div{display:grid;min-width:0}header strong{overflow:hidden;text-overflow:ellipsis;white-space:nowrap}header small{color:#aebbb5}nav{display:flex;align-items:center;gap:.35rem}.tool{display:grid;place-items:center;min-width:42px;height:42px;padding:0 .65rem;border:1px solid rgb(255 255 255 / .18);border-radius:8px;background:rgb(255 255 255 / .08);color:inherit;text-decoration:none}.reset,.download{font-size:.8rem}.close{font-size:1.4rem}main{position:relative;display:grid;place-items:center;overflow:auto}.original{max-width:calc(100vw - 7rem);max-height:calc(100vh - 6rem);object-fit:contain;transition:transform .12s;transform-origin:center}iframe{width:100%;height:100%;border:0;background:white}audio{width:min(620px,90vw)}video{max-width:90vw;max-height:80vh}.previous,.next{position:fixed;top:50%;width:48px;height:64px;border:0;border-radius:10px;background:rgb(255 255 255 / .1);color:white;font-size:2rem}.previous{left:1rem}.next{right:1rem}.unsupported{text-align:center;max-width:430px;padding:2rem}.unsupported span{display:grid;place-items:center;width:72px;height:72px;margin:auto;border:1px solid rgb(255 255 255 / .2);border-radius:16px;font:700 .8rem var(--font-mono)}.unsupported p{color:#aebbb5;line-height:1.55}.unsupported .button{display:inline-flex}.text-preview{width:100%;height:100%}
@media(max-width:600px){.viewer header{padding:.5rem}.viewer header small{display:none}.tool{min-width:40px;padding:0 .45rem}.download{display:none}.original{max-width:100vw;max-height:calc(100vh - 60px)}.previous{left:.35rem}.next{right:.35rem}}
</style>
