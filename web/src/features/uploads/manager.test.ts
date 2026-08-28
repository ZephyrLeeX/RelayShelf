import { beforeEach, describe, expect, it, vi } from 'vitest'
import { UploadStatus, type UploadSession } from '@/api/generated'
import { UploadManager, partRange } from './manager'
import { uploadState } from './store'
import type { ChunkRequest, ChunkTransport } from './types'

function session(overrides: Partial<UploadSession> = {}): UploadSession {
  return { id: 'upload-a', originalFilename: 'file.bin', expectedSize: 9, clientMime: null, chunkSize: 4, partCount: 3, status: UploadStatus.UPLOADING, expiresAt: '2026-09-01T00:00:00Z', completedParts: [], createdAt: '2026-08-28T00:00:00Z', updatedAt: '2026-08-28T00:00:00Z', ...overrides }
}

class ControlledTransport implements ChunkTransport {
  requests: ChunkRequest[] = []
  resolvers: Array<() => void> = []
  upload(request: ChunkRequest) { this.requests.push(request); return new Promise<void>((resolve) => this.resolvers.push(resolve)) }
}

describe('UploadManager', () => {
  beforeEach(() => { uploadState.items = []; vi.stubGlobal('crypto', { randomUUID: () => 'client-a', subtle: crypto.subtle }) })

  it.each([
    [0, 4, 0, []], [1, 4, 1, [[0, 0, 1]]], [8, 4, 2, [[0, 0, 4], [1, 4, 8]]], [9, 4, 3, [[0, 0, 4], [1, 4, 8], [2, 8, 9]]],
  ])('calculates exact part boundaries for %i bytes', (size, chunk, count, expected) => {
    expect(Array.from({ length: count }, (_, part) => { const range = partRange(part, chunk, size); return [part, range.start, range.end] })).toEqual(expected)
  })

  it('does not mark a 100% progress event durable before HTTP 204', async () => {
    const transport = new ControlledTransport()
    const api = { create: vi.fn().mockResolvedValue(session()), get: vi.fn(), complete: vi.fn().mockResolvedValue(session({ status: UploadStatus.COMPLETED, completedParts: [0, 1, 2] })) }
    const manager = new UploadManager(api, transport)
    await manager.addFiles([new File([new Uint8Array(9)], 'file.bin', { lastModified: 1 })])
    await Promise.resolve(); await Promise.resolve()
    transport.requests[0].onProgress(4)
    expect(manager.items[0].completedParts).toEqual([])
    transport.resolvers[0]()
    await Promise.resolve(); await Promise.resolve()
    expect(manager.items[0].completedParts).toContain(0)
  })

  it('pause aborts active requests without completing their parts', async () => {
    const transport = new ControlledTransport()
    const api = { create: vi.fn().mockResolvedValue(session()), get: vi.fn(), complete: vi.fn() }
    const manager = new UploadManager(api, transport)
    await manager.addFiles([new File([new Uint8Array(9)], 'file.bin', { lastModified: 1 })])
    await Promise.resolve(); await Promise.resolve()
    const clientId = manager.items[0].clientId
    manager.pause(clientId)
    expect(manager.items[0].status).toBe('PAUSED')
    expect(transport.requests[0].signal.aborted).toBe(true)
    expect(manager.items[0].completedParts).toEqual([])
  })
})
