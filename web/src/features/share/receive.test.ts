import { beforeEach, describe, expect, it, vi } from 'vitest'
import { clearShare, parseShare, pendingShare, receiveShare, shareNotice } from './receive'

beforeEach(() => clearShare())
describe('share parsing', () => {
  it('keeps title, text and URL without duplicating a complete link', () => {
    expect(parseShare({ title: '标题', text: '查看 https://example.com/a', url: 'https://example.com/a' })).toEqual({ title: '标题', body: '查看 https://example.com/a' })
    expect(parseShare({ text: 'https://example.com/abc', url: 'https://example.com/a' }).body).toBe('https://example.com/abc\n\nhttps://example.com/a')
    expect(parseShare({ title: '仅标题' })).toEqual({ title: '', body: '仅标题' })
    expect(parseShare({ text: '文字', url: 'https://example.com/?a=1&b=2#x' }).body).toContain('?a=1&b=2#x')
  })
  it.each([
    'https://en.wikipedia.org/wiki/Function_(mathematics)',
    'https://example.com/search?q=hello!',
  ])('preserves legitimate trailing punctuation in %s when deduplicating', (url) => {
    expect(parseShare({ text: `查看 ${url}`, url }).body).toBe(`查看 ${url}`)
    expect(parseShare({ text: `查看 ${url}。`, url }).body).toBe(`查看 ${url}。`)
  })
  it.each(['.', ',', ';', '!', '?', ')', ']', '。', ').'])('ignores surrounding punctuation %s when deduplicating', (punctuation) => {
    const url = 'https://example.com/a'
    const text = `查看 (${url}${punctuation}`
    expect(parseShare({ text, url }).body).toBe(text)
  })
  it.each([101, 200])('accepts a title with %i emoji code points', (length) => {
    const title = '😀'.repeat(length)
    expect(parseShare({ title, text: '正文' })).toEqual({ title, body: '正文' })
  })
  it('rejects a title with 201 emoji code points', () => {
    expect(() => parseShare({ title: '😀'.repeat(201), text: '正文' })).toThrow('large')
  })
  it('rejects empty, oversized titles and oversized UTF-8 bodies', () => {
    expect(() => parseShare({ text: new File(['x'], 'x') })).toThrow()
    expect(() => parseShare({ title: 'x'.repeat(201) })).toThrow()
    expect(() => parseShare({ text: '中'.repeat(400000) })).toThrow()
  })
})
describe('memory handoff before login', () => {
  it('strips URL token and retains data until explicitly consumed without browser storage', async () => {
    const replace = vi.spyOn(history, 'replaceState')
    const local = vi.spyOn(Storage.prototype, 'setItem')
    class Channel {
      port1 = { onmessage: null as ((event: { data: unknown }) => void) | null, close: vi.fn() }
      port2 = { deliver: (data: unknown) => this.port1.onmessage?.({ data }) }
    }
    vi.stubGlobal('MessageChannel', Channel)
    Object.defineProperty(navigator, 'serviceWorker', { configurable: true, value: { controller: { postMessage: (_message: unknown, ports: Channel['port2'][]) => ports[0]!.deliver({ title: '共享标题', text: '私密内容' }) } } })
    await receiveShare('opaque-token')
    expect(replace).toHaveBeenCalledWith(history.state, '', '/share')
    expect(pendingShare.value?.body).toBe('私密内容')
    expect(local).not.toHaveBeenCalled()
    await receiveShare('another')
    expect(pendingShare.value?.body).toBe('私密内容')
    expect(shareNotice.value).toContain('未能接收')
    clearShare()
    expect(pendingShare.value).toBeNull()
    vi.unstubAllGlobals()
  })
  it('reports missing/expired handoff instead of sending', async () => {
    Object.defineProperty(navigator, 'serviceWorker', { configurable: true, value: undefined })
    await receiveShare('expired')
    expect(pendingShare.value).toBeNull()
    expect(shareNotice.value).toContain('重新分享')
  })
})
