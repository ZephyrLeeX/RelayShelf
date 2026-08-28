import { describe, expect, it } from 'vitest'
import type { AttachmentSummary } from '@/api/generated'
import { previewKind } from './preview'

const file = (detectedMime: string) => ({ id: 'a', originalFilename: 'unsafe.svg', clientMime: 'image/png', detectedMime, sizeBytes: 1, displayOrder: 0 }) as AttachmentSummary

describe('attachment preview policy', () => {
  it.each(['text/html', 'application/xhtml+xml', 'image/svg+xml', 'application/xml', 'text/xml'])('never actively renders %s', (mime) => {
    expect(previewKind(file(mime))).toBe('download')
  })
  it('uses detected MIME rather than client MIME', () => {
    expect(previewKind(file('application/octet-stream'))).toBe('download')
  })
  it('allows safe raster, PDF, and media transports', () => {
    expect(previewKind(file('image/png'))).toBe('image')
    expect(previewKind(file('application/pdf'))).toBe('pdf')
    expect(previewKind(file('audio/mpeg'))).toBe('audio')
    expect(previewKind(file('video/mp4'))).toBe('video')
  })
})
