import { ref } from 'vue'

export interface SharedDraft { title: string; body: string }
export const pendingShare = ref<SharedDraft | null>(null)
export const shareNotice = ref('')

export function parseShare(data: Record<string, unknown>): SharedDraft {
  const field = (name: string) => typeof data[name] === 'string' ? data[name].trim() : ''
  const title = field('title')
  const text = field('text')
  const url = field('url')
  // Compare complete URL tokens, never a substring (e.g. /a vs /abc).
  const links = text.match(/https?:\/\/[^\s<>"']+/g) ?? []
  // Preserve punctuation belonging to the URL; only ignore punctuation after it.
  const hasUrl = links.some((link) => link === url || (link.startsWith(url) && /^[.,;!?)\]。]+$/.test(link.slice(url.length))))
  const body = [text, url && !hasUrl && text !== url ? url : ''].filter(Boolean).join('\n\n') || title
  if (!body) throw new Error('empty')
  if ([...title].length > 200 || new TextEncoder().encode(body).byteLength > 1024 * 1024) throw new Error('large')
  return { title: title === body ? '' : title, body }
}

export function clearShare() {
  pendingShare.value = null
  shareNotice.value = ''
}

export async function receiveShare(token: string) {
  // Strip the token before auth routing; no payload is ever present in the URL.
  history.replaceState(history.state, '', '/share')
  if (!token) return
  try {
    const worker = navigator.serviceWorker?.controller
    if (!worker) throw new Error('unavailable')
    const data = await new Promise<Record<string, unknown>>((resolve, reject) => {
      const channel = new MessageChannel()
      const timer = setTimeout(() => { channel.port1.close(); reject(new Error('timeout')) }, 5000)
      channel.port1.onmessage = (event) => {
        clearTimeout(timer)
        channel.port1.close()
        if (!event.data || typeof event.data !== 'object') reject(new Error('unavailable'))
        else resolve(event.data)
      }
      worker.postMessage({ type: 'relayshelf:take-share', token }, [channel.port2])
    })
    // Never silently replace another pending share.
    if (pendingShare.value) throw new Error('busy')
    pendingShare.value = parseShare(data)
    shareNotice.value = ''
  } catch {
    shareNotice.value = '分享未能接收（可能已过期或内容过大）。请重新分享，或复制后粘贴到编辑器。'
  }
}
