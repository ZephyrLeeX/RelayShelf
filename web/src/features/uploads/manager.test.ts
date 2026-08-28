import { beforeEach, describe, expect, it, vi } from 'vitest'
import { UploadStatus, type UploadSession } from '@/api/generated'
import { setAuthExpiredHandler } from '@/shared/api/authExpiry'
import { setCsrfToken } from '@/shared/api/configure'
import { UploadManager, partRange } from './manager'
import { ResumeLedger } from './resumeLedger'
import { uploadState } from './store'
import { fingerprintFile } from './fingerprint'
import type { ChunkRequest, ChunkTransport, UploadItem } from './types'

// jsdom's Blob lacks arrayBuffer(); the production fingerprint stays intact
// while tests substitute a deterministic content-sensitive stub via FileReader.
vi.mock('./fingerprint', async () => {
  const readBlob = (blob: Blob) => new Promise<ArrayBuffer>((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(reader.result as ArrayBuffer)
    reader.onerror = () => reject(reader.error)
    reader.readAsArrayBuffer(blob)
  })
  return { fingerprintFile: async (file: File) => `stub-${Array.from(new Uint8Array(await readBlob(file))).join('-')}` }
})

function session(overrides: Partial<UploadSession> = {}): UploadSession {
  return { id: 'upload-a', originalFilename: 'file.bin', expectedSize: 9, clientMime: null, chunkSize: 4, partCount: 3, status: UploadStatus.UPLOADING, expiresAt: '2026-09-01T00:00:00Z', completedParts: [], createdAt: '2026-08-28T00:00:00Z', updatedAt: '2026-08-28T00:00:00Z', ...overrides }
}

class ControlledTransport implements ChunkTransport {
  requests: ChunkRequest[] = []
  resolvers: Array<() => void> = []
  upload(request: ChunkRequest) {
    this.requests.push(request)
    return new Promise<void>((resolve, reject) => {
      const abort = () => reject(new DOMException('Aborted', 'AbortError'))
      if (request.signal.aborted) abort()
      else request.signal.addEventListener('abort', abort, { once: true })
      this.resolvers.push(resolve)
    })
  }
}

// Rejects the first N calls for a part with a transport error, then either
// hangs like a slow network or resolves, so each test observes exactly the
// attempts it scripts.
class ScriptedTransport implements ChunkTransport {
  requests: ChunkRequest[] = []
  constructor(private readonly failures: Array<{ part: number; status: number; code: string }>, private readonly resolveWhenDrained = false) {}
  upload(request: ChunkRequest) {
    this.requests.push(request)
    const index = this.failures.findIndex((failure) => failure.part === request.partNumber)
    if (index >= 0) {
      const [failure] = this.failures.splice(index, 1)
      return Promise.reject({ ...failure })
    }
    return this.resolveWhenDrained ? Promise.resolve() : new Promise<void>(() => {})
  }
}

function file9(name = 'file.bin', bytes = new Uint8Array(9), lastModified = 1) {
  return new File([bytes], name, { lastModified })
}

async function flush() {
  for (let index = 0; index < 6; index++) await Promise.resolve()
}

type FailedUpload = Extract<UploadItem, { status: 'FAILED' }>
const asFailed = (item: UploadItem) => item as FailedUpload

describe('UploadManager', () => {
  let sequence = 0
  beforeEach(() => {
    uploadState.items = []
    uploadState.ledgerWarning = false
    sequence = 0
    vi.stubGlobal('crypto', { randomUUID: () => `client-${sequence++}`, subtle: crypto.subtle })
    setCsrfToken(undefined)
  })

  it.each([
    [0, 4, 0, []], [1, 4, 1, [[0, 0, 1]]], [8, 4, 2, [[0, 0, 4], [1, 4, 8]]], [9, 4, 3, [[0, 0, 4], [1, 4, 8], [2, 8, 9]]],
    [8 * 1024 * 1024, 8 * 1024 * 1024, 1, [[0, 0, 8 * 1024 * 1024]]],
    [8 * 1024 * 1024 + 1, 8 * 1024 * 1024, 2, [[0, 0, 8 * 1024 * 1024], [1, 8 * 1024 * 1024, 8 * 1024 * 1024 + 1]]],
  ])('calculates exact part boundaries for %i bytes', (size, chunk, count, expected) => {
    expect(Array.from({ length: count }, (_, part) => { const range = partRange(part, chunk, size); return [part, range.start, range.end] })).toEqual(expected)
  })

  it('does not mark a 100% progress event durable before HTTP 204', async () => {
    const transport = new ControlledTransport()
    const api = { create: vi.fn().mockResolvedValue(session()), get: vi.fn(), complete: vi.fn().mockResolvedValue(session({ status: UploadStatus.COMPLETED, completedParts: [0, 1, 2] })) }
    const manager = new UploadManager(api, transport)
    await manager.addFiles([file9()])
    await flush()
    transport.requests[0].onProgress(4)
    expect(manager.items[0].completedParts).toEqual([])
    transport.resolvers[0]()
    await flush()
    expect(manager.items[0].completedParts).toContain(0)
  })

  it('pause aborts active requests without completing their parts', async () => {
    const transport = new ControlledTransport()
    const api = { create: vi.fn().mockResolvedValue(session()), get: vi.fn(), complete: vi.fn() }
    const manager = new UploadManager(api, transport)
    await manager.addFiles([file9()])
    await flush()
    const clientId = manager.items[0].clientId
    manager.pause(clientId)
    expect(manager.items[0].status).toBe('PAUSED')
    expect(transport.requests[0].signal.aborted).toBe(true)
    expect(manager.items[0].completedParts).toEqual([])
  })

  it('never uploads a chunk for a 0-byte file and completes directly', async () => {
    const transport = new ControlledTransport()
    const api = { create: vi.fn().mockResolvedValue(session({ expectedSize: 0, partCount: 0, completedParts: [] })), get: vi.fn(), complete: vi.fn().mockResolvedValue(session({ expectedSize: 0, partCount: 0, status: UploadStatus.COMPLETED })) }
    const manager = new UploadManager(api, transport)
    await manager.addFiles([new File([new Uint8Array(0)], 'empty.bin')])
    await flush()
    expect(transport.requests).toHaveLength(0)
    expect(api.complete).toHaveBeenCalledWith('upload-a')
    expect(manager.items[0].status).toBe('COMPLETED')
    expect(manager.items[0].progress).toBe(1)
  })

  it('remembers a Pause issued while CREATING: the created session is persisted, nothing uploads, and resume reconciles first', async () => {
    const transport = new ControlledTransport()
    let resolveCreate!: (value: UploadSession) => void
    const api = { create: vi.fn(() => new Promise<UploadSession>((resolve) => { resolveCreate = resolve })), get: vi.fn().mockResolvedValue(session({ completedParts: [0, 1] })), complete: vi.fn() }
    const manager = new UploadManager(api, transport)
    await manager.reconcile('user-1')
    await manager.addFiles([file9()])
    const clientId = manager.items[0].clientId
    expect(manager.items[0].status).toBe('CREATING')
    manager.pause(clientId)
    expect(manager.items[0].status).toBe('PAUSED')
    resolveCreate(session({ completedParts: [0, 1] }))
    await flush()
    expect(manager.items[0].status).toBe('PAUSED')
    expect(manager.items[0].serverUploadId).toBe('upload-a')
    expect(transport.requests).toHaveLength(0)
    expect(new ResumeLedger().read('user-1').map((entry) => entry.uploadId)).toEqual(['upload-a'])
    await manager.resume(clientId)
    expect(api.get).toHaveBeenCalledWith('upload-a')
    expect(transport.requests).toHaveLength(1)
    expect(transport.requests.map((request) => request.partNumber)).toEqual([2])
  })

  it('resume schedules only the parts the server has not recorded', async () => {
    const transport = new ControlledTransport()
    const api = { create: vi.fn().mockResolvedValue(session()), get: vi.fn().mockResolvedValue(session({ completedParts: [0, 1] })), complete: vi.fn() }
    const manager = new UploadManager(api, transport)
    await manager.addFiles([file9()])
    await flush()
    const clientId = manager.items[0].clientId
    manager.pause(clientId)
    const scheduledBeforeResume = transport.requests.length
    await manager.resume(clientId)
    expect(transport.requests.slice(scheduledBeforeResume).map((request) => request.partNumber)).toEqual([2])
  })

  it('retries a retryable 503 chunk failure a bounded number of times and then completes', async () => {
    vi.useFakeTimers()
    try {
      const transport = new ScriptedTransport([
        { part: 0, status: 503, code: 'HTTP_ERROR' },
        { part: 0, status: 503, code: 'HTTP_ERROR' },
      ], true)
      const api = { create: vi.fn().mockResolvedValue(session({ chunkSize: 9, partCount: 1, expectedSize: 9 })), get: vi.fn(), complete: vi.fn().mockResolvedValue(session({ status: UploadStatus.COMPLETED, completedParts: [0] })) }
      const manager = new UploadManager(api, transport)
      void manager.addFiles([file9()])
      for (let guard = 0; guard < 12 && manager.items[0]?.status !== 'COMPLETED'; guard++) await vi.advanceTimersByTimeAsync(9_000)
      expect(transport.requests.filter((request) => request.partNumber === 0)).toHaveLength(3)
      expect(manager.items[0].status).toBe('COMPLETED')
    } finally { vi.useRealTimers() }
  })

  it('treats Pause during retry backoff as a normal cancellation without an unhandled rejection', async () => {
    vi.useFakeTimers()
    const unhandled: unknown[] = []
    const onUnhandled = (reason: unknown) => unhandled.push(reason)
    process.on('unhandledRejection', onUnhandled)
    try {
      const single = { chunkSize: 9, partCount: 1, expectedSize: 9 }
      const transport = new ScriptedTransport([{ part: 0, status: 503, code: 'HTTP_ERROR' }], true)
      const api = {
        create: vi.fn().mockResolvedValue(session(single)),
        get: vi.fn().mockResolvedValue(session(single)),
        complete: vi.fn().mockResolvedValue(session({ ...single, status: UploadStatus.COMPLETED, completedParts: [0] })),
      }
      const manager = new UploadManager(api, transport)
      await manager.addFiles([file9()])
      await flush()
      expect(transport.requests).toHaveLength(1) // the 503 arrived; the chunk is in backoff
      const clientId = manager.items[0].clientId
      manager.pause(clientId)
      await flush()
      await vi.advanceTimersByTimeAsync(9_000)
      expect(unhandled).toEqual([])
      expect(manager.items[0].status).toBe('PAUSED')
      expect(manager.scheduler.active).toBe(0)

      await manager.resume(clientId)
      expect(api.get).toHaveBeenCalledWith('upload-a')
      for (let guard = 0; guard < 12 && manager.items[0]?.status !== 'COMPLETED'; guard++) await vi.advanceTimersByTimeAsync(9_000)
      expect(manager.items[0].status).toBe('COMPLETED')
      expect(unhandled).toEqual([])
    } finally {
      process.off('unhandledRejection', onUnhandled)
      vi.useRealTimers()
    }
  })

  it('does not blindly retry a 4xx chunk rejection', async () => {
    const transport = new ScriptedTransport([{ part: 0, status: 409, code: 'UPLOAD_INVALID_STATE' }])
    const api = { create: vi.fn().mockResolvedValue(session()), get: vi.fn(), complete: vi.fn() }
    const manager = new UploadManager(api, transport)
    await manager.addFiles([file9()])
    await flush()
    expect(transport.requests.filter((request) => request.partNumber === 0)).toHaveLength(1)
    expect(manager.items[0].status).toBe('FAILED')
    expect(asFailed(manager.items[0]).errorCode).toBe('UPLOAD_INVALID_STATE')
  })

  it('follows the auth-expired flow on 401', async () => {
    const expired = vi.fn()
    setAuthExpiredHandler(expired)
    const transport = new ScriptedTransport([{ part: 0, status: 401, code: 'AUTH_REQUIRED' }])
    const api = { create: vi.fn().mockResolvedValue(session()), get: vi.fn(), complete: vi.fn() }
    const manager = new UploadManager(api, transport)
    await manager.addFiles([file9()])
    await flush()
    expect(expired).toHaveBeenCalled()
    expect(manager.items[0].status).toBe('FAILED')
  })

  it('recovers from CSRF_INVALID with one token refresh and one automatic resume', async () => {
    setCsrfToken('stale')
    const refresh = vi.fn(() => { setCsrfToken('fresh'); return Promise.resolve(undefined) })
    const transport = new ScriptedTransport([{ part: 0, status: 403, code: 'CSRF_INVALID' }])
    const api = { create: vi.fn().mockResolvedValue(session()), get: vi.fn().mockResolvedValue(session()), complete: vi.fn().mockResolvedValue(session({ status: UploadStatus.COMPLETED, completedParts: [0, 1, 2] })) }
    const manager = new UploadManager(api, transport)
    manager.setCsrfRefresh(refresh)
    await manager.addFiles([file9()])
    await flush()
    expect(refresh).toHaveBeenCalledTimes(1)
    expect(transport.requests.filter((request) => request.partNumber === 0).map((request) => request.csrfToken)).toEqual(['stale', 'fresh'])
    expect(manager.items[0].status).toBe('UPLOADING')
  })

  it('does not loop when the CSRF refresh cannot produce a new token', async () => {
    setCsrfToken('stale')
    const refresh = vi.fn(() => Promise.resolve(undefined))
    const transport = new ScriptedTransport([
      { part: 0, status: 403, code: 'CSRF_INVALID' },
      { part: 1, status: 403, code: 'CSRF_INVALID' },
    ])
    const api = { create: vi.fn().mockResolvedValue(session()), get: vi.fn().mockResolvedValue(session()), complete: vi.fn() }
    const manager = new UploadManager(api, transport)
    manager.setCsrfRefresh(refresh)
    await manager.addFiles([file9()])
    await flush()
    expect(manager.items[0].status).toBe('FAILED')
    expect(asFailed(manager.items[0]).errorCode).toBe('CSRF_INVALID')
    expect(asFailed(manager.items[0]).retryable).toBe(true)
    expect(refresh.mock.calls.length).toBeLessThanOrEqual(2)
    expect(transport.requests).toHaveLength(2)
  })

  it('starts a new bounded CSRF recovery episode on an explicit user retry', async () => {
    setCsrfToken('stale')
    const refresh = vi.fn()
      .mockImplementationOnce(() => { setCsrfToken('fresh1'); return Promise.resolve(undefined) })
      .mockImplementationOnce(() => { setCsrfToken('fresh2'); return Promise.resolve(undefined) })
    const single = { chunkSize: 9, partCount: 1, expectedSize: 9 }
    const transport = new ScriptedTransport([
      { part: 0, status: 403, code: 'CSRF_INVALID' }, // stale token
      { part: 0, status: 403, code: 'CSRF_INVALID' }, // fresh1 is also invalid
    ], true)
    const api = {
      create: vi.fn().mockResolvedValue(session(single)),
      get: vi.fn()
        .mockResolvedValueOnce(session(single)) // auto-resume after the first refresh
        .mockResolvedValueOnce(session(single)), // user-retry reconciliation
      complete: vi.fn().mockResolvedValue(session({ ...single, status: UploadStatus.COMPLETED, completedParts: [0] })),
    }
    const manager = new UploadManager(api, transport)
    manager.setCsrfRefresh(refresh)
    await manager.addFiles([file9()])
    await flush()
    // The auto-refreshed token also fails; the spent episode must NOT refresh
    // again on its own, so the item lands in FAILED awaiting a user retry.
    expect(manager.items[0].status).toBe('FAILED')
    expect(asFailed(manager.items[0]).errorCode).toBe('CSRF_INVALID')

    manager.retry(manager.items[0].clientId)
    await flush()
    expect(refresh).toHaveBeenCalledTimes(2)
    expect(api.get).toHaveBeenCalledTimes(2)
    expect(transport.requests.filter((request) => request.partNumber === 0).map((request) => request.csrfToken)).toEqual(['stale', 'fresh1', 'fresh2'])
    expect(manager.items[0].status).toBe('COMPLETED')
  })

  it('reconciles a lost Complete response through GET instead of re-uploading parts', async () => {
    const transport = new ControlledTransport()
    const api = { create: vi.fn().mockResolvedValue(session({ chunkSize: 9, partCount: 1, expectedSize: 9 })), get: vi.fn().mockResolvedValue(session({ chunkSize: 9, partCount: 1, expectedSize: 9, status: UploadStatus.COMPLETED, completedParts: [0] })), complete: vi.fn().mockRejectedValue(new TypeError('network error')) }
    const manager = new UploadManager(api, transport)
    await manager.addFiles([file9()])
    await flush()
    transport.resolvers[0]()
    await flush()
    expect(api.complete).toHaveBeenCalledTimes(1)
    expect(api.get).toHaveBeenCalledWith('upload-a')
    expect(transport.requests).toHaveLength(1)
    expect(manager.items[0].status).toBe('COMPLETED')
  })

  it('keeps a COMPLETING finalize retryable instead of rescheduling parts', async () => {
    const transport = new ControlledTransport()
    const api = { create: vi.fn().mockResolvedValue(session({ chunkSize: 9, partCount: 1, expectedSize: 9 })), get: vi.fn().mockResolvedValue(session({ chunkSize: 9, partCount: 1, expectedSize: 9, status: UploadStatus.COMPLETING, completedParts: [0] })), complete: vi.fn().mockRejectedValue(new TypeError('network error')) }
    const manager = new UploadManager(api, transport)
    await manager.addFiles([file9()])
    await flush()
    transport.resolvers[0]()
    await flush()
    expect(manager.items[0].status).toBe('FAILED')
    expect(asFailed(manager.items[0]).errorCode).toBe('UPLOAD_FINALIZE_RETRYABLE')
    expect(asFailed(manager.items[0]).retryable).toBe(true)
    expect(transport.requests).toHaveLength(1)
  })

  it('treats a server FAILED session as terminal on resume: no parts, no Complete loop, an explicit re-upload action', async () => {
    const transport = new ControlledTransport()
    const api = { create: vi.fn().mockResolvedValueOnce(session()).mockResolvedValueOnce(session({ id: 'upload-b' })), get: vi.fn().mockResolvedValue(session({ status: UploadStatus.FAILED })), complete: vi.fn() }
    const manager = new UploadManager(api, transport)
    await manager.addFiles([file9()])
    await flush()
    const clientId = manager.items[0].clientId
    manager.pause(clientId)
    const requestsBeforeResume = transport.requests.length
    await manager.resume(clientId)
    expect(manager.items[0].status).toBe('FAILED')
    expect(asFailed(manager.items[0]).errorCode).toBe('UPLOAD_SERVER_FAILED')
    expect(asFailed(manager.items[0]).retryable).toBe(false)
    expect(transport.requests).toHaveLength(requestsBeforeResume)
    expect(api.complete).not.toHaveBeenCalled()
    manager.reupload(clientId)
    await flush()
    expect(api.create).toHaveBeenCalledTimes(2)
    expect(manager.items[0].serverUploadId).toBe('upload-b')
    expect(manager.items[0].status).toBe('UPLOADING')
  })

  it('restores ledger sessions after reload: COMPLETED without a File, FAILED as terminal, expired entries dropped', async () => {
    const ledger = new ResumeLedger()
    ledger.upsert('user-1', { uploadId: 'upload-done', lastModified: 1, createdAt: 't' })
    ledger.upsert('user-1', { uploadId: 'upload-failed', lastModified: 1, createdAt: 't' })
    ledger.upsert('user-1', { uploadId: 'upload-expired', lastModified: 1, createdAt: 't' })
    const api = {
      create: vi.fn(),
      get: vi.fn()
        .mockResolvedValueOnce(session({ id: 'upload-done', status: UploadStatus.COMPLETED, completedParts: [0, 1, 2] }))
        .mockResolvedValueOnce(session({ id: 'upload-failed', status: UploadStatus.FAILED }))
        .mockResolvedValueOnce(session({ id: 'upload-expired', status: UploadStatus.EXPIRED })),
      complete: vi.fn(),
    }
    const manager = new UploadManager(api, new ControlledTransport())
    await manager.reconcile('user-1')
    expect(manager.items).toHaveLength(2)
    const completed = manager.items.find((item) => item.serverUploadId === 'upload-done')!
    expect(completed.status).toBe('COMPLETED')
    expect(completed.file).toBeUndefined()
    const failed = manager.items.find((item) => item.serverUploadId === 'upload-failed') as Extract<UploadItem, { status: 'FAILED' }>
    expect(failed.status).toBe('FAILED')
    expect(failed.errorCode).toBe('UPLOAD_SERVER_FAILED')
    expect(ledger.read('user-1').map((entry) => entry.uploadId)).toEqual(['upload-done', 'upload-failed'])
  })

  it('never restores another user ledger', async () => {
    const ledger = new ResumeLedger()
    ledger.upsert('alice', { uploadId: 'upload-a', lastModified: 1, createdAt: 't' })
    const api = { create: vi.fn(), get: vi.fn(), complete: vi.fn() }
    const manager = new UploadManager(api, new ControlledTransport(), ledger)
    await manager.reconcile('bob')
    expect(manager.items).toHaveLength(0)
    expect(api.get).not.toHaveBeenCalled()
  })

  it('keeps the network upload authoritative when localStorage throws', async () => {
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => { throw new DOMException('quota', 'QuotaExceededError') })
    const transport = new ControlledTransport()
    const api = { create: vi.fn().mockResolvedValue(session({ chunkSize: 9, partCount: 1, expectedSize: 9 })), get: vi.fn(), complete: vi.fn().mockResolvedValue(session({ chunkSize: 9, partCount: 1, expectedSize: 9, status: UploadStatus.COMPLETED, completedParts: [0] })) }
    const manager = new UploadManager(api, transport)
    await manager.reconcile('user-1')
    await manager.addFiles([file9()])
    await flush()
    expect(manager.items[0].status).toBe('UPLOADING')
    expect(uploadState.ledgerWarning).toBe(true)
    transport.resolvers[0]()
    await flush()
    expect(manager.items[0].status).toBe('COMPLETED')
    expect(() => manager.remove(manager.items[0].clientId)).not.toThrow()
    expect(manager.items).toHaveLength(0)
  })

  describe('reselection validation', () => {
    // Simulates a reload: the ledger entry is the only client-side state and
    // the server session restores as PAUSED without any File object.
    async function restoredManager(entry: { uploadId: string; lastModified: number; fingerprint?: string }) {
      new ResumeLedger().upsert('user-1', { ...entry, createdAt: 't' })
      const api = { create: vi.fn(), get: vi.fn().mockResolvedValue(session({ id: entry.uploadId, completedParts: [0] })), complete: vi.fn() }
      const manager = new UploadManager(api, new ControlledTransport())
      await manager.reconcile('user-1')
      return { manager, api }
    }
    it('accepts the original file and schedules only missing parts', async () => {
      const { manager } = await restoredManager({ uploadId: 'upload-a', lastModified: 7 })
      expect(manager.items[0].status).toBe('PAUSED')
      await manager.resume(manager.items[0].clientId, file9('file.bin', new Uint8Array(9), 7))
      expect(manager.items[0].status).toBe('UPLOADING')
      expect(manager.items[0].file).toBeDefined()
    })
    it.each([
      ['filename', () => file9('other.bin', new Uint8Array(9), 7)],
      ['size', () => file9('file.bin', new Uint8Array(8), 7)],
      ['lastModified', () => file9('file.bin', new Uint8Array(9), 8)],
    ])('rejects a reselected file with a different %s', async (_name, build) => {
      const { manager, api } = await restoredManager({ uploadId: 'upload-a', lastModified: 7 })
      await manager.resume(manager.items[0].clientId, build())
      const rejected = manager.items[0] as Extract<UploadItem, { status: 'FAILED' }>
      expect(rejected.status).toBe('FAILED')
      expect(rejected.errorCode).toBe('UPLOAD_FILE_MISMATCH')
      expect(rejected.retryable).toBe(true)
      expect(api.get).toHaveBeenCalledTimes(1)
    })
    it('rejects a reselected file whose lightweight fingerprint differs', async () => {
      const original = file9('file.bin', new Uint8Array([1, 2, 3, 4, 5, 6, 7, 8, 9]), 7)
      const { manager } = await restoredManager({ uploadId: 'upload-a', lastModified: 7, fingerprint: await fingerprintFile(original) })
      await manager.resume(manager.items[0].clientId, file9('file.bin', new Uint8Array([9, 8, 7, 6, 5, 4, 3, 2, 1]), 7))
      const rejected = manager.items[0] as Extract<UploadItem, { status: 'FAILED' }>
      expect(rejected.status).toBe('FAILED')
      expect(rejected.errorCode).toBe('UPLOAD_FILE_MISMATCH')
    })
  })
})
