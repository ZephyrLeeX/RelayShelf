<script setup lang="ts">
import { computed, nextTick, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import type { PreviewImage } from '../composables/useMessageImages'
const props = defineProps<{ images: PreviewImage[]; current: string }>()
const emit = defineEmits<{ close: []; select: [src: string] }>()
const index = computed(() => props.images.findIndex(image => image.src === props.current))
const image = computed(() => props.images[index.value])
const zoom = ref(1)
const download = computed(() => /^\/api\/v1\/attachments\/[^/]+\/preview$/.test(props.current) ? props.current.replace(/preview$/, 'download') : null)
const failed = ref(false)
const loading = ref(true)
const revision = ref(0)
const dialog = ref<HTMLElement>()
const previousFocus = document.activeElement as HTMLElement | null
const previousOverflow = document.body.style.overflow
function reset() { failed.value = false; loading.value = true; revision.value++ }
watch(() => props.current, () => { zoom.value = 1; reset() })
function move(delta: number) { emit('select', props.images[(index.value + delta + props.images.length) % props.images.length].src) }
function key(event: KeyboardEvent) {
  if (event.key === 'Escape') { event.preventDefault(); event.stopImmediatePropagation(); emit('close') }
  if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') { event.preventDefault(); move(event.key === 'ArrowLeft' ? -1 : 1) }
  if (event.key === 'Tab') {
    const buttons = [...dialog.value!.querySelectorAll<HTMLElement>('button:not([disabled]), a[href]')]
    const next = buttons[(buttons.indexOf(document.activeElement as HTMLElement) + (event.shiftKey ? -1 : 1) + buttons.length) % buttons.length]
    event.preventDefault(); next?.focus()
  }
}
onMounted(async () => { document.body.style.overflow = 'hidden'; document.addEventListener('keydown', key, true); await nextTick(); dialog.value?.querySelector('button')?.focus() })
onBeforeUnmount(() => { document.body.style.overflow = previousOverflow; document.removeEventListener('keydown', key, true); previousFocus?.focus() })
</script>

<template>
  <Teleport to="body">
    <div
      ref="dialog"
      class="image-preview backdrop"
      role="dialog"
      aria-modal="true"
      aria-label="图片预览"
      @click.stop.self="$emit('close')"
    >
      <header>
        <span>{{ image?.alt }} · {{ index + 1 }} / {{ images.length }}</span>
        <button
          type="button"
          aria-label="关闭图片预览"
          @click="$emit('close')"
        >
          关闭 ✕
        </button>
      </header>
      <div class="zoom-controls">
        <button
          type="button"
          aria-label="缩小"
          @click="zoom = Math.max(.25, zoom - .25)"
        >
          −
        </button>
        <button
          type="button"
          aria-label="重置缩放"
          @click="zoom = 1"
        >
          {{ Math.round(zoom * 100) }}%
        </button>
        <button
          type="button"
          aria-label="放大"
          @click="zoom = Math.min(4, zoom + .25)"
        >
          +
        </button>
        <a
          v-if="download"
          :href="download"
          :download="image?.alt"
        >下载</a>
      </div>
      <p
        v-if="failed"
        class="image-state"
        role="alert"
      >
        图片加载失败。<button
          type="button"
          @click="reset"
        >
          重试
        </button>
      </p>
      <p
        v-else-if="loading"
        class="image-state"
        role="status"
      >
        正在加载图片…
      </p>
      <img
        v-if="image && !failed"
        :key="`${current}-${revision}`"
        :src="image.src"
        :alt="image.alt"
        :style="{ transform: `scale(${zoom})` }"
        referrerpolicy="no-referrer"
        @load="loading = false"
        @error="failed = true; loading = false"
        @click.stop
      >
      <nav
        v-if="images.length > 1"
        aria-label="切换图片"
      >
        <button
          type="button"
          @click="move(-1)"
        >
          上一张
        </button>
        <button
          type="button"
          @click="move(1)"
        >
          下一张
        </button>
      </nav>
    </div>
  </Teleport>
</template>

<style scoped>
.image-preview{position:fixed;inset:0;z-index:90;display:flex;align-items:center;justify-content:center;padding:5rem 1rem;background:rgb(8 12 20 / .9);color:white;cursor:zoom-out}.image-preview header{position:absolute;top:max(1rem,env(safe-area-inset-top));left:1rem;right:1rem;display:flex;align-items:center;justify-content:space-between;gap:1rem}.image-preview header span{overflow-wrap:anywhere;min-width:0}.image-preview img{max-width:100%;max-height:calc(100dvh - 10rem);object-fit:contain;cursor:default}.image-preview button{flex-shrink:0;min-height:44px;border:1px solid #ffffff66;border-radius:8px;padding:.5rem .9rem;background:#232b38;color:white;cursor:pointer}.image-preview button:hover{background:#38465a}.image-preview button:focus-visible{outline:3px solid white;outline-offset:3px}.image-preview nav{position:absolute;bottom:max(1rem,env(safe-area-inset-bottom));display:flex;gap:1rem}.image-state{position:absolute;top:4rem;text-align:center}
.zoom-controls{position:absolute;bottom:4.5rem;z-index:1;display:flex;align-items:center;gap:.5rem}.zoom-controls a{color:white;padding:.6rem}.image-preview{overflow:hidden}.image-preview header,.image-preview nav{z-index:2}.image-state{z-index:2}
</style>
