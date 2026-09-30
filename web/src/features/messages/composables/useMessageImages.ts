import { computed, inject, onBeforeUnmount, provide, ref, watch, type InjectionKey } from 'vue'
import MarkdownIt from 'markdown-it'
import { toApiError } from '@/shared/api/errors'
import { toast } from '@/shared/ui/toast'
import type { AttachmentSummary } from '@/api/generated'
import { previewURL, safeRasterMIMEs } from '@/features/files/preview'
export interface PreviewImage { src: string; alt: string }
const key: InjectionKey<(src: string) => void> = Symbol('message-images')
const markdown = new MarkdownIt({ html: false })
export function markdownImages(source: string): PreviewImage[] {
  return markdown.parse(source, {}).flatMap(token => token.children ?? [])
    .filter(token => token.type === 'image' && /^https?:\/\//i.test(String(token.attrGet('src') ?? '')))
    .map(token => ({ src: String(token.attrGet('src')), alt: token.content || '正文图片' }))
}
function collectImages(source: string, files: AttachmentSummary[]) {
  return [...markdownImages(source), ...files.filter(file => safeRasterMIMEs.has(file.detectedMime)).map(file => ({ src: previewURL(file.id), alt: file.originalFilename }))]
    .filter((image, index, all) => all.findIndex(item => item.src === image.src) === index)
}
export function useMessageImages(source: () => string, files: () => AttachmentSummary[], load?: () => Promise<{ source: string; files: AttachmentSummary[] }>, boundary: () => string = () => '', revision: () => unknown = () => undefined) {
  const initialImages = computed(() => collectImages(source(), files()))
  const completeImages = ref<PreviewImage[] | null>(null)
  const images = computed(() => completeImages.value ?? initialImages.value)
  const current = ref<string | null>(null)
  let generation = 0
  let loading = false
  let previousSources = initialImages.value.map(image => image.src)
  function reconcile() {
    // Removing the current image consistently closes the viewer.
    if (current.value && !images.value.some(image => image.src === current.value)) current.value = null
  }
  function reset() {
    generation++; loading = false; current.value = null; completeImages.value = null
  }
  async function loadComplete() {
    if (!load || loading) return
    const version = generation
    loading = true
    try {
      const result = await load()
      if (version === generation) {
        completeImages.value = collectImages(result.source, result.files)
        reconcile()
      }
    } catch (error) {
      if (version === generation && [401, 403, 404, 410].includes(toApiError(error).status)) { reset(); return }
      if (version === generation) {
        completeImages.value = null
        reconcile()
        toast.warning('完整图片列表加载失败，当前仅显示已加载的图片。关闭后可重试。')
      }
    } finally {
      if (version === generation) loading = false
    }
  }
  async function open(src: string) {
    if (!images.value.some(image => image.src === src)) return
    current.value = src
    if (!completeImages.value) await loadComplete()
  }
  provide(key, open)
  watch(boundary, reset, { flush: 'sync' })
  // Revisions are refresh hints for truncated bodies, never a reason to close.
  // Object replacement and metadata do not define image identity.
  watch(() => JSON.stringify([source(), initialImages.value.map(image => image.src), revision()]), () => {
    generation++; loading = false
    const oldSources = previousSources
    previousSources = initialImages.value.map(image => image.src)
    if (current.value && oldSources.includes(current.value) && !initialImages.value.some(image => image.src === current.value)) {
      current.value = null
    }
    if (load && current.value) {
      // Retain the full gallery while refreshing a truncated card's content.
      void loadComplete()
    } else {
      completeImages.value = null
      reconcile()
    }
  }, { flush: 'sync' })
  onBeforeUnmount(reset)
  return { images, current, open }
}
export function useOpenMessageImage() { return inject(key, undefined) }
