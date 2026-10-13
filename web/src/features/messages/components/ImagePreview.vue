<script setup lang="ts">
import { computed, nextTick, onMounted, onBeforeUnmount, ref, watch } from 'vue'
import type { PreviewImage } from '../composables/useMessageImages'

const props = defineProps<{ images: PreviewImage[]; current: string }>()
const emit = defineEmits<{ close: []; select: [src: string] }>()
const index = computed(() => props.images.findIndex(image => image.src === props.current))
const image = computed(() => props.images[index.value])
const download = computed(() => /^\/api\/v1\/attachments\/[^/]+\/preview$/.test(props.current) ? props.current.replace(/preview$/, 'download') : null)
const dialog = ref<HTMLElement>()
const stage = ref<HTMLElement>()
const original = ref<HTMLImageElement>()
const loading = ref(true)
const failed = ref(false)
const revision = ref(0)
const width = ref(0)
const height = ref(0)
const viewport = ref({ width: 0, height: 0 })
const scale = ref(1)
const offset = ref({ x: 0, y: 0 })
const mode = ref<'fit' | 'original' | 'custom'>('fit')
const fitScale = computed(() => width.value && height.value ? Math.min(1, viewport.value.width / width.value, viewport.value.height / height.value) : 1)
const ready = computed(() => !loading.value && !failed.value)
const canPan = computed(() => ready.value && scale.value > fitScale.value + .0001)
const atFit = computed(() => Math.abs(scale.value - fitScale.value) < .0001)
const percent = computed(() => `${Math.round(scale.value * 1000) / 10}%`)
const imageStyle = computed(() => ({ width: `${width.value}px`, height: `${height.value}px`, transform: `translate(${offset.value.x}px, ${offset.value.y}px) scale(${scale.value})`, visibility: ready.value ? 'visible' as const : 'hidden' as const }))

function clampOffset() {
  const x = Math.max(0, (width.value * scale.value - viewport.value.width) / 2)
  const y = Math.max(0, (height.value * scale.value - viewport.value.height) / 2)
  offset.value = { x: Math.max(-x, Math.min(x, offset.value.x)), y: Math.max(-y, Math.min(y, offset.value.y)) }
}
function fit() { mode.value = 'fit'; scale.value = fitScale.value; offset.value = { x: 0, y: 0 } }
function actual() { mode.value = 'original'; scale.value = 1; offset.value = { x: 0, y: 0 } }
function toggleSize() { if (!ready.value) return; if (mode.value === 'fit') actual(); else fit() }
function measure() {
  if (!stage.value || !stage.value.clientWidth || !stage.value.clientHeight) return
  viewport.value = { width: stage.value.clientWidth, height: stage.value.clientHeight }
  if (mode.value === 'fit') fit()
  else { scale.value = Math.max(fitScale.value, scale.value); clampOffset() }
}
function zoomTo(value: number, x = 0, y = 0) {
  if (!ready.value) return
  const next = Math.max(fitScale.value, Math.min(8, value))
  const ratio = next / scale.value
  offset.value = { x: x - (x - offset.value.x) * ratio, y: y - (y - offset.value.y) * ratio }
  scale.value = next
  mode.value = atFit.value ? 'fit' : 'custom'
  clampOffset()
}
function wheel(event: WheelEvent) {
  const rect = stage.value!.getBoundingClientRect()
  zoomTo(scale.value * Math.exp(-event.deltaY * .002), event.clientX - rect.left - rect.width / 2, event.clientY - rect.top - rect.height / 2)
}
function reset() {
  revision.value++; loading.value = true; failed.value = false
  width.value = 0; height.value = 0; fit(); clearGesture()
}
watch(() => props.current, reset, { flush: 'sync' })
function loaded(event: Event) {
  // Detached images can finish after navigation/retry; only the live element owns state.
  if (event.target !== original.value || original.value?.getAttribute('src') !== props.current || original.value?.dataset.revision !== String(revision.value)) return
  width.value = original.value.naturalWidth; height.value = original.value.naturalHeight
  loading.value = false; failed.value = false; measure(); fit()
}
function error(event: Event) {
  if (event.target !== original.value || original.value?.getAttribute('src') !== props.current || original.value?.dataset.revision !== String(revision.value)) return
  failed.value = true; loading.value = false; clearGesture()
}
function move(delta: number) {
  if (props.images.length > 1) emit('select', props.images[(index.value + delta + props.images.length) % props.images.length].src)
}

type Point = { x: number; y: number }
const pointers = new Map<number, Point>()
let press: Point | null = null
// Keep this through pointerup and navigation: browsers dispatch click afterwards.
let suppressClick = false
let swipe: Point | null = null
let pinched = false
let distance = 0
let center: Point | null = null
function clearGesture() { pointers.clear(); press = null; swipe = null; pinched = false; distance = 0; center = null }
function pinchGeometry() {
  const [a, b] = [...pointers.values()]
  return { distance: Math.hypot(b.x - a.x, b.y - a.y), center: { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 } }
}
function backdropClick(event: MouseEvent) {
  if (suppressClick || pointers.size) return
  // The image has pointer-events:none; hit-test its transformed display bounds.
  const rect = ready.value ? original.value?.getBoundingClientRect() : null
  if (rect && event.clientX >= rect.left && event.clientX <= rect.right && event.clientY >= rect.top && event.clientY <= rect.bottom) return
  emit('close')
}
function trackMovement(event: PointerEvent) {
  if (press && Math.hypot(event.clientX - press.x, event.clientY - press.y) > 5) suppressClick = true
}
function pointerDown(event: PointerEvent) {
  if (!pointers.size) suppressClick = false
  if (!ready.value || (event.pointerType === 'mouse' && event.button !== 0)) return
  stage.value?.setPointerCapture?.(event.pointerId)
  pointers.set(event.pointerId, { x: event.clientX, y: event.clientY })
  if (pointers.size === 1) press = { x: event.clientX, y: event.clientY }
  if (pointers.size === 1) swipe = event.pointerType !== 'mouse' && atFit.value ? { x: event.clientX, y: event.clientY } : null
  if (pointers.size === 2) {
    pinched = true; suppressClick = true; swipe = null
    const geometry = pinchGeometry(); distance = geometry.distance; center = geometry.center
  }
}
function pointerMove(event: PointerEvent) {
  const previous = pointers.get(event.pointerId)
  if (!previous) return
  trackMovement(event)
  pointers.set(event.pointerId, { x: event.clientX, y: event.clientY })
  if (pointers.size === 2) {
    const geometry = pinchGeometry()
    const rect = stage.value!.getBoundingClientRect()
    if (distance > 0 && center) {
      zoomTo(scale.value * geometry.distance / distance, center.x - rect.left - rect.width / 2, center.y - rect.top - rect.height / 2)
      offset.value = { x: offset.value.x + geometry.center.x - center.x, y: offset.value.y + geometry.center.y - center.y }
      clampOffset()
    }
    distance = geometry.distance; center = geometry.center
  } else if (pointers.size === 1 && canPan.value && !swipe) {
    offset.value = { x: offset.value.x + event.clientX - previous.x, y: offset.value.y + event.clientY - previous.y }; clampOffset()
  }
}
function pointerUp(event: PointerEvent) {
  if (!pointers.has(event.pointerId)) return
  trackMovement(event)
  if (event.type !== 'pointerup') suppressClick = true
  if (event.type === 'pointerup' && pointers.size === 1 && swipe && !pinched && atFit.value) {
    const dx = event.clientX - swipe.x, dy = event.clientY - swipe.y
    if (Math.abs(dx) > 50 && Math.abs(dx) > Math.abs(dy) * 1.5) move(dx < 0 ? 1 : -1)
  }
  pointers.delete(event.pointerId)
  swipe = null
  if (!pointers.size) clearGesture()
}

const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null
let restoreScroll = () => {}
let observer: ResizeObserver | undefined
function focusFirst() { dialog.value?.querySelector<HTMLButtonElement>('button')?.focus({ preventScroll: true }) }
function containFocus(event: FocusEvent) { if (event.target instanceof Node && !dialog.value?.contains(event.target)) focusFirst() }
function key(event: KeyboardEvent) {
  if (['Escape', 'ArrowLeft', 'ArrowRight', 'Tab'].includes(event.key)) { event.preventDefault(); event.stopImmediatePropagation() }
  if (event.key === 'Escape') emit('close')
  if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') move(event.key === 'ArrowLeft' ? -1 : 1)
  if (event.key === 'Tab') {
    const buttons = [...dialog.value!.querySelectorAll<HTMLElement>('button:not([disabled]), a[href]')]
    const next = buttons[(buttons.indexOf(document.activeElement as HTMLElement) + (event.shiftKey ? -1 : 1) + buttons.length) % buttons.length]
    next?.focus({ preventScroll: true })
  }
}
onMounted(async () => {
  const body = document.body
  const saved = { overflow: body.style.overflow, position: body.style.position, top: body.style.top, left: body.style.left, width: body.style.width }
  const x = window.scrollX, y = window.scrollY
  Object.assign(body.style, { overflow: 'hidden', position: 'fixed', top: `-${y}px`, left: `-${x}px`, width: '100%' })
  restoreScroll = () => { Object.assign(body.style, saved); window.scrollTo(x, y) }
  document.addEventListener('keydown', key, true)
  document.addEventListener('focusin', containFocus)
  window.addEventListener('resize', measure)
  if (typeof ResizeObserver !== 'undefined') { observer = new ResizeObserver(measure); if (stage.value) observer.observe(stage.value) }
  measure(); await nextTick(); focusFirst()
})
onBeforeUnmount(() => {
  observer?.disconnect(); window.removeEventListener('resize', measure)
  document.removeEventListener('keydown', key, true); document.removeEventListener('focusin', containFocus)
  restoreScroll(); previousFocus?.focus({ preventScroll: true })
})
</script>

<template>
  <Teleport to="body">
    <div
      ref="dialog"
      class="image-preview backdrop"
      role="dialog"
      aria-modal="true"
      aria-label="图片预览"
      @click.stop.self="backdropClick"
      @pointerdown.self="suppressClick = false"
    >
      <header>
        <span>{{ image?.alt }} · {{ index + 1 }} / {{ images.length }}</span>
        <button
          type="button"
          aria-label="关闭图片预览"
          @click="emit('close')"
        >
          关闭 ✕
        </button>
      </header>
      <div
        ref="stage"
        class="image-stage"
        :class="{ pannable: canPan }"
        :aria-busy="loading"
        @click.stop.self="backdropClick"
        @wheel.prevent="wheel"
        @dblclick.prevent="toggleSize"
        @pointerdown="pointerDown"
        @pointermove="pointerMove"
        @pointerup="pointerUp"
        @pointercancel="pointerUp"
        @lostpointercapture="pointerUp"
      >
        <img
          v-if="image && !failed"
          :key="`${current}-${revision}`"
          ref="original"
          :data-revision="revision"
          class="original"
          :src="image.src"
          :alt="image.alt"
          :style="imageStyle"
          referrerpolicy="no-referrer"
          draggable="false"
          @load="loaded"
          @error="error"
        >
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
      </div>
      <footer>
        <div class="zoom-controls">
          <button
            type="button"
            aria-label="缩小"
            :disabled="!ready || atFit"
            @click="zoomTo(scale / 1.25)"
          >
            −
          </button>
          <output aria-label="缩放比例">{{ ready ? percent : '—' }}</output>
          <button
            type="button"
            aria-label="放大"
            :disabled="!ready || scale >= 8"
            @click="zoomTo(scale * 1.25)"
          >
            +
          </button>
          <button
            type="button"
            :disabled="!ready"
            :aria-pressed="mode === 'fit'"
            @click="fit"
          >
            适应窗口
          </button>
          <button
            type="button"
            :disabled="!ready"
            :aria-pressed="mode === 'original'"
            @click="actual"
          >
            原始尺寸
          </button>
          <a
            v-if="download"
            :href="download"
            :download="image?.alt"
          >下载原图</a>
        </div>
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
          <span>{{ index + 1 }} / {{ images.length }}</span>
          <button
            type="button"
            @click="move(1)"
          >
            下一张
          </button>
        </nav>
      </footer>
    </div>
  </Teleport>
</template>

<style scoped>
.image-preview{position:fixed;inset:0;z-index:90;display:grid;grid-template-rows:auto minmax(0,1fr) auto;gap:.5rem;box-sizing:border-box;padding:max(.65rem,env(safe-area-inset-top)) max(.65rem,env(safe-area-inset-right)) max(.65rem,env(safe-area-inset-bottom)) max(.65rem,env(safe-area-inset-left));background:rgb(12 18 16 / .96);color:#f3f7f5;overflow:hidden;overscroll-behavior:contain}.image-preview header{display:flex;align-items:center;justify-content:space-between;gap:1rem;min-width:0}.image-preview header span{overflow:hidden;text-overflow:ellipsis;white-space:nowrap;min-width:0}.image-stage{position:relative;display:grid;place-items:center;min-width:0;min-height:0;overflow:hidden;touch-action:none;user-select:none}.image-stage.pannable{cursor:grab}.image-stage.pannable:active{cursor:grabbing}.image-preview img{position:absolute;max-width:none;max-height:none;object-fit:contain;pointer-events:none;transform-origin:center}.image-preview button,.image-preview a{box-sizing:border-box;flex-shrink:0;min-height:44px;border:1px solid rgb(255 255 255 / .22);border-radius:8px;padding:.5rem .65rem;background:rgb(255 255 255 / .08);color:inherit;cursor:pointer;text-decoration:none;font-size:.85rem}.image-preview button:hover,.image-preview a:hover{background:rgb(255 255 255 / .18)}.image-preview button:disabled{opacity:.45;cursor:default}.image-preview button:focus-visible,.image-preview a:focus-visible{outline:3px solid white;outline-offset:2px}.image-preview button[aria-pressed=true]{border-color:#aebbb5;background:rgb(255 255 255 / .18)}footer{display:grid;gap:.4rem}.zoom-controls,.image-preview nav{display:flex;flex-wrap:wrap;justify-content:center;align-items:center;gap:.4rem}.zoom-controls output{min-width:3.4rem;text-align:center;font-variant-numeric:tabular-nums}.image-state{z-index:1;text-align:center}.image-state button{margin-left:.5rem}
</style>
