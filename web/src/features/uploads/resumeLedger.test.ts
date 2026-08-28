import { describe, expect, it } from 'vitest'
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
})
