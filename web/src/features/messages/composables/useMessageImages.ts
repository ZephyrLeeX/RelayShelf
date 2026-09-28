import { computed, inject, provide, ref, watch, type InjectionKey } from 'vue'
import MarkdownIt from 'markdown-it'
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
export function useMessageImages(source: () => string, files: () => AttachmentSummary[], load?: () => Promise<{ source: string; files: AttachmentSummary[] }>) {
  const initialImages = computed(() => collectImages(source(), files()))
  const completeImages = ref<PreviewImage[] | null>(null)
  const images = computed(() => completeImages.value ?? initialImages.value)
  const current = ref<string | null>(null)
  let generation = 0
  let loading = false
  async function open(src: string) {
    current.value = src
    if (!load || completeImages.value || loading) return
    const version = generation
    loading = true
    try {
      const result = await load()
      if (version === generation) completeImages.value = collectImages(result.source, result.files)
    } catch {
      if (version === generation) toast.warning('完整图片列表加载失败，当前仅显示已加载的图片。关闭后可重试。')
    } finally {
      if (version === generation) loading = false
    }
  }
  provide(key, open)
  watch(initialImages, () => { generation++; loading = false; current.value = null; completeImages.value = null })
  return { images, current, open }
}
export function useOpenMessageImage() { return inject(key, undefined) }
