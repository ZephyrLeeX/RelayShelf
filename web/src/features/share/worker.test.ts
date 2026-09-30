import { Blob } from 'node:buffer'
import { readFileSync } from 'node:fs'
import { runInNewContext } from 'node:vm'
import { describe, expect, it } from 'vitest'

function worker() {
  const handlers: Record<string, (event: Record<string, unknown>) => void> = {}
  runInNewContext(readFileSync('public/share-target.js', 'utf8'), {
    self: { location: { origin: 'https://relay.test' }, addEventListener: (name: string, handler: typeof handlers[string]) => { handlers[name] = handler } },
    URL, Response, Blob, Map, Date, crypto: { randomUUID: () => 'opaque-token' }, setTimeout: () => {},
  })
  return handlers
}
async function post(handlers: ReturnType<typeof worker>, data: FormData) {
  let response: Promise<Response> | undefined
  const file = [...data.values()].some((value) => typeof value !== 'string')
  const body = file ? '--boundary\r\nContent-Disposition: form-data; name="text"; filename="secret.txt"\r\nContent-Type: text/plain\r\n\r\nprivate\r\n--boundary--\r\n' : new URLSearchParams([...data.entries()] as [string, string][]).toString()
  const contentType = file ? 'multipart/form-data; boundary=boundary' : 'application/x-www-form-urlencoded'
  handlers.fetch!({ request: new Request('https://relay.test/share', { method: 'POST', body, headers: { 'Content-Type': contentType } }), resultingClientId: 'client-a', stopImmediatePropagation: () => {}, respondWith: (result: Promise<Response>) => { response = result } })
  return response!
}
function claim(handlers: ReturnType<typeof worker>, id = 'client-a', origin = 'https://relay.test') {
  let result: unknown = 'no-reply'
  handlers.message!({ data: { type: 'relayshelf:take-share', token: 'opaque-token' }, source: { url: `${origin}/share`, id }, ports: [{ postMessage: (value: unknown) => { result = value } }] })
  return result
}
describe('share worker boundary', () => {
  it('intercepts only share POST and uses a one-time, client-bound memory handoff', async () => {
    const handlers = worker()
    const data = new FormData(); data.set('text', 'private'); data.set('url', 'https://example.com')
    const response = await post(handlers, data)
    expect(response.status).toBe(303)
    expect(response.headers.get('location')).toBe('https://relay.test/share#opaque-token')
    expect(claim(handlers, 'foreign-client')).toBeNull()
    expect(claim(handlers, 'client-a', 'https://evil.test')).toBe('no-reply')
    expect(claim(handlers)).toEqual({ text: 'private', url: 'https://example.com' })
    expect(claim(handlers)).toBeNull()
    handlers.fetch!({ request: new Request('https://relay.test/api/v1/messages'), respondWith: () => { throw new Error('private API intercepted') } })
  })
  it('rejects files and oversized input without logging or caching it', async () => {
    const handlers = worker()
    const files = new FormData(); files.set('text', new File(['private'], 'secret.txt'))
    expect((await post(handlers, files)).headers.get('location')).toContain('#unavailable')
    const large = new FormData(); large.set('text', 'x'.repeat(1100000))
    expect((await post(handlers, large)).headers.get('location')).toContain('#unavailable')
    expect(claim(handlers)).toBeNull()
  })
})
