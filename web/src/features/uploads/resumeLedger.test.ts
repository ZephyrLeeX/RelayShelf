import { describe, expect, it, vi } from 'vitest'
import { ResumeLedger } from './resumeLedger'

describe('ResumeLedger', () => {
  it('isolates upload metadata by user and persists no filenames or bytes', () => {
    const ledger = new ResumeLedger()
    ledger.upsert('alice', { uploadId: 'up-a', lastModified: 12, fingerprint: 'sample', createdAt: 'now' })
    expect(ledger.read('alice')).toHaveLength(1)
    expect(ledger.read('bob')).toEqual([])
    const raw = localStorage.getItem('relayshelf.upload-resume.v1:alice')!
    expect(raw).not.toContain('filename')
    expect(raw).not.toContain('body')
  })

  it('keeps persistence best-effort: a throwing localStorage never propagates', () => {
    const ledger = new ResumeLedger()
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new DOMException('denied', 'SecurityError') })
    expect(() => ledger.upsert('alice', { uploadId: 'up-a', lastModified: 12, createdAt: 'now' })).not.toThrow()
    expect(() => ledger.remove('alice', 'up-a')).not.toThrow()
    expect(ledger.available).toBe(false)
  })

  it('reports an unreadable ledger as empty without throwing', () => {
    const ledger = new ResumeLedger()
    ledger.upsert('alice', { uploadId: 'up-a', lastModified: 12, createdAt: 'now' })
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => { throw new DOMException('denied', 'SecurityError') })
    expect(ledger.read('alice')).toEqual([])
    expect(ledger.available).toBe(false)
  })
})
