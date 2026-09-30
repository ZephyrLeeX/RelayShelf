// Imported before Workbox routes. Share bodies never reach the network or cache.
const shares = new Map()
const lifetime = 60_000
self.addEventListener('fetch', (event) => {
  const url = new URL(event.request.url)
  if (url.origin !== self.location.origin || url.pathname !== '/share' || event.request.method !== 'POST') return
  event.stopImmediatePropagation()
  event.respondWith((async () => {
    try {
      // Read with a bound, including multipart overhead; files are not supported.
      const reader = event.request.body?.getReader()
      if (!reader) throw new Error('empty')
      const chunks = []
      let size = 0
      while (true) {
        const { value, done } = await reader.read()
        if (done) break
        size += value.byteLength
        if (size > 1024 * 1024 + 16 * 1024) {
          await reader.cancel()
          throw new Error('large')
        }
        chunks.push(value)
      }
      const data = await new Response(new Blob(chunks), { headers: { 'Content-Type': event.request.headers.get('Content-Type') || '' } }).formData()
      const payload = {}
      for (const [key, value] of data) {
        if (!['title', 'text', 'url'].includes(key) || typeof value !== 'string' || key in payload) throw new Error('invalid')
        payload[key] = value
      }
      for (const [key, item] of shares) if (Date.now() - item.created > lifetime) shares.delete(key)
      if (shares.size >= 8) throw new Error('busy')
      const token = crypto.randomUUID()
      shares.set(token, { payload, created: Date.now(), clientId: event.resultingClientId })
      setTimeout(() => shares.delete(token), lifetime)
      return Response.redirect(`${url.origin}/share#${token}`, 303)
    } catch {
      return Response.redirect(`${url.origin}/share#unavailable`, 303)
    }
  })())
})
self.addEventListener('message', (event) => {
  if (event.data?.type !== 'relayshelf:take-share' || !event.ports[0] || !event.source?.url) return
  const source = new URL(event.source.url)
  if (source.origin !== self.location.origin || source.pathname !== '/share') return
  const item = shares.get(event.data.token)
  if (!item || Date.now() - item.created > lifetime || (item.clientId && item.clientId !== event.source.id)) {
    event.ports[0].postMessage(null)
    return
  }
  shares.delete(event.data.token)
  event.ports[0].postMessage(item.payload)
})
