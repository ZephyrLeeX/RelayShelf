import { onUnmounted, ref } from 'vue'
import { toast } from '@/shared/ui/toast'

export function useClipboardFeedback() {
  const copied = ref(false)
  let resetTimer = 0

  async function copyText(text: string | null | undefined) {
    if (!text) return false
    try {
      await navigator.clipboard.writeText(text)
      copied.value = true
      window.clearTimeout(resetTimer)
      resetTimer = window.setTimeout(() => { copied.value = false }, 1_600)
      toast.success('正文已复制到剪贴板', 1_600)
      return true
    } catch {
      copied.value = false
      toast.error('复制失败，请检查浏览器的剪贴板权限。')
      return false
    }
  }

  onUnmounted(() => window.clearTimeout(resetTimer))
  return { copied, copyText }
}
