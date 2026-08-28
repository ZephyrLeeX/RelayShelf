import { DefaultService, type UploadSession } from '@/api/generated'
import { notifyAuthExpired } from '@/shared/api/authExpiry'
import { getCsrfToken } from '@/shared/api/configure'
import { fingerprintFile } from './fingerprint'
import { uploadError } from './errors'
import { MAX_CHUNK_ATTEMPTS, retryDelay, waitForRetry } from './retry'
import { ResumeLedger } from './resumeLedger'
import { UploadScheduler } from './scheduler'
import { uploadState } from './store'
import { XHRChunkTransport } from './transport'
import type { ChunkTransport, PartRange, ResumeEntry, UploadItem } from './types'

interface UploadAPI {
  create(file: File): PromiseLike<UploadSession>
  get(id: string): PromiseLike<UploadSession>
  complete(id: string): PromiseLike<UploadSession>
}

const defaultAPI: UploadAPI = {
  create: (file) => DefaultService.createUpload({ originalFilename: file.name, expectedSize: file.size, clientMime: file.type || null }),
  get: (id) => DefaultService.getUpload(id),
  complete: (id) => DefaultService.completeUpload(id),
}

export const SERVER_FAILED_MESSAGE = '服务器端上传已失败（暂存数据损坏）。请移除后重新上传原文件。'

export function partRange(partNumber: number, chunkSize: number, fileSize: number): PartRange {
  const start = partNumber * chunkSize
  const end = Math.min(start + chunkSize, fileSize)
  return { partNumber, start, end, length: Math.max(0, end - start) }
}

function baseItem(file: File): UploadItem {
  return {
    clientId: crypto.randomUUID(), file, filename: file.name, size: file.size, lastModified: file.lastModified,
    completedParts: [], activeParts: [], transferredByPart: {}, sentBytes: 0, progress: 0,
    createdAt: new Date().toISOString(), selected: true, status: 'QUEUED',
  }
}

export class UploadManager {
  readonly scheduler: UploadScheduler
  private readonly controllers = new Map<string, Map<number, AbortController>>()
  private readonly lastProgressUpdate = new Map<string, number>()
  private readonly autoRecoveredCsrf = new Set<string>()
  private csrfRefresh: () => Promise<unknown> = async () => undefined
  private csrfRefreshInFlight: Promise<string | undefined> | undefined
  private userId = ''

  constructor(
    private readonly api: UploadAPI = defaultAPI,
    private readonly transport: ChunkTransport = new XHRChunkTransport(),
    private readonly ledger = new ResumeLedger(),
    scheduler = new UploadScheduler(4, 2),
  ) { this.scheduler = scheduler }

  get items() { return uploadState.items }
  getItem(clientId: string) { return uploadState.items.find((item) => item.clientId === clientId) }

  // Injected at app bootstrap to avoid a Pinia circular import; the default
  // refresh keeps tests hermetic while production wires the auth bootstrap.
  setCsrfRefresh(refresh: () => Promise<unknown>) { this.csrfRefresh = refresh }

  async addFiles(files: Iterable<File>) {
    const added: string[] = []
    for (const file of files) {
      const item = baseItem(file)
      uploadState.items.push(item)
      added.push(item.clientId)
      void this.prepareFingerprint(item.clientId, file)
      void this.create(item.clientId, file)
    }
    return added
  }

  private async prepareFingerprint(clientId: string, file: File) {
    try {
      const fingerprint = await fingerprintFile(file)
      const item = this.getItem(clientId)
      if (!item || item.status === 'CANCELED') return
      this.replace(clientId, { ...item, fingerprint })
      if (item.serverUploadId && this.userId) this.persist(this.getItem(clientId)!)
    } catch { /* metadata strengthening is best effort */ }
  }

  private async create(clientId: string, file: File) {
    const current = this.getItem(clientId)
    if (!current || current.status !== 'QUEUED') return
    this.replace(clientId, { ...current, status: 'CREATING' })
    try {
      const session = await this.api.create(file)
      const item = this.getItem(clientId)
      if (!item || item.status === 'CANCELED') return
      // A Pause issued while CREATING cannot abort the Create request, so it
      // is remembered instead: persist the session, keep PAUSED, schedule
      // nothing, and let an explicit resume reconcile against server truth.
      if (item.status === 'PAUSED') {
        this.replace(clientId, { ...item, serverUploadId: session.id, session, completedParts: [...session.completedParts] })
        this.persist(this.getItem(clientId)!)
        return
      }
      this.replace(clientId, { ...item, serverUploadId: session.id, session, completedParts: [...session.completedParts], status: 'UPLOADING' })
      this.persist(this.getItem(clientId)!)
      if (session.partCount === 0) void this.complete(clientId); else this.scheduleMissing(clientId)
    } catch (cause) { this.fail(clientId, cause) }
  }

  pause(clientId: string) {
    const item = this.getItem(clientId)
    if (!item || !['QUEUED', 'CREATING', 'UPLOADING'].includes(item.status)) return
    this.scheduler.remove(clientId)
    this.abort(clientId)
    this.replace(clientId, { ...item, activeParts: [], transferredByPart: {}, status: 'PAUSED' })
  }

  async resume(clientId: string, file?: File) {
    const item = this.getItem(clientId)
    if (!item || !item.serverUploadId) return
    if (file) {
      const mismatch = await this.validateReselection(item, file)
      if (mismatch) {
        const latest = this.getItem(clientId)
        if (latest) this.replace(clientId, { ...latest, activeParts: [], transferredByPart: {}, status: 'FAILED', error: mismatch, errorCode: 'UPLOAD_FILE_MISMATCH', retryable: true })
        return
      }
    }
    try {
      const session = await this.api.get(item.serverUploadId)
      if (session.status === 'EXPIRED') { this.expire(clientId); return }
      if (session.status === 'COMPLETED') { this.markCompleted(clientId, session); return }
      if (session.status === 'FAILED') { this.markServerFailed(clientId, session); return }
      const next = this.getItem(clientId)
      if (!next) return
      if (!file && !next.file) {
        // Without bytes there is nothing to schedule; keep waiting for the
        // user to reselect the original file instead of a stuck UPLOADING.
        this.replace(clientId, { ...next, session, completedParts: [...session.completedParts], status: 'PAUSED' })
        return
      }
      this.replace(clientId, { ...next, file: file ?? next.file, filename: session.originalFilename, size: session.expectedSize, session, completedParts: [...session.completedParts], activeParts: [], transferredByPart: {}, status: session.status === 'COMPLETING' ? 'COMPLETING' : 'UPLOADING' })
      if (session.status === 'COMPLETING' || session.partCount === 0) void this.complete(clientId); else this.scheduleMissing(clientId)
    } catch (cause) { this.fail(clientId, cause) }
  }

  retry(clientId: string) {
    const item = this.getItem(clientId)
    if (!item || item.status !== 'FAILED') return
    if (!item.serverUploadId) {
      if (item.file) {
        this.replace(clientId, { ...item, status: 'QUEUED' })
        void this.create(clientId, item.file)
      }
      return
    }
    // An explicit Retry of a CSRF failure starts a NEW bounded recovery
    // episode: drop the spent auto-recovery marker, refresh the token once,
    // and only then reconcile through the resume GET — never replay the old
    // token straight into another chunk.
    if (item.errorCode === 'CSRF_INVALID') void this.retryAfterCsrfRefresh(clientId)
    else void this.resume(clientId)
  }

  // Safe terminal action for server-FAILED or corrupt staging sessions: the
  // dead server session is abandoned and the same File starts a fresh upload.
  reupload(clientId: string) {
    const item = this.getItem(clientId)
    if (!item?.file || item.status !== 'FAILED') return
    const file = item.file
    this.remove(clientId)
    void this.addFiles([file])
  }

  remove(clientId: string) {
    const item = this.getItem(clientId)
    if (!item) return
    this.scheduler.remove(clientId)
    this.abort(clientId)
    if (item.serverUploadId && this.userId) this.ledger.remove(this.userId, item.serverUploadId)
    uploadState.items = uploadState.items.filter((candidate) => candidate.clientId !== clientId)
  }

  retireUploadIds(uploadIds: string[]) {
    const ids = new Set(uploadIds)
    for (const item of [...uploadState.items]) if (item.serverUploadId && ids.has(item.serverUploadId)) this.remove(item.clientId)
  }

  async reconcile(userId: string) {
    if (this.userId === userId && uploadState.items.some((item) => item.serverUploadId)) return
    this.userId = userId
    for (const entry of this.ledger.read(userId)) await this.reconcileEntry(entry)
    if (!this.ledger.available) uploadState.ledgerWarning = true
  }

  clearForLogout() {
    for (const item of uploadState.items) this.abort(item.clientId)
    uploadState.items = []
    this.userId = ''
  }

  private async reconcileEntry(entry: ResumeEntry) {
    try {
      const session = await this.api.get(entry.uploadId)
      if (session.status === 'EXPIRED') { this.ledger.remove(this.userId, entry.uploadId); return }
      if (session.status === 'FAILED') {
        uploadState.items.push({
          clientId: crypto.randomUUID(), serverUploadId: session.id, session, filename: session.originalFilename,
          size: session.expectedSize, lastModified: entry.lastModified, fingerprint: entry.fingerprint,
          completedParts: [...session.completedParts], activeParts: [], transferredByPart: {}, sentBytes: this.durableBytes(session),
          progress: session.expectedSize ? this.durableBytes(session) / session.expectedSize : 0,
          createdAt: entry.createdAt, selected: true, status: 'FAILED',
          error: SERVER_FAILED_MESSAGE, errorCode: 'UPLOAD_SERVER_FAILED', retryable: false,
        })
        return
      }
      const status = session.status === 'COMPLETED' ? 'COMPLETED' : session.status === 'COMPLETING' ? 'COMPLETING' : 'PAUSED'
      const item = {
        clientId: crypto.randomUUID(), serverUploadId: session.id, session, filename: session.originalFilename,
        size: session.expectedSize, lastModified: entry.lastModified, fingerprint: entry.fingerprint,
        completedParts: [...session.completedParts], activeParts: [], transferredByPart: {}, sentBytes: this.durableBytes(session),
        progress: session.expectedSize ? this.durableBytes(session) / session.expectedSize : session.status === 'COMPLETED' ? 1 : 0,
        createdAt: entry.createdAt, selected: true, status,
      } as UploadItem
      uploadState.items.push(item)
      if (status === 'COMPLETING') void this.complete(item.clientId)
    } catch (cause) {
      const error = uploadError(cause)
      if (error.status === 404) this.ledger.remove(this.userId, entry.uploadId)
    }
  }

  private scheduleMissing(clientId: string) {
    const item = this.getItem(clientId)
    if (!item?.file || !item.session || item.status !== 'UPLOADING') return
    const completed = new Set(item.completedParts)
    for (let part = 0; part < item.session.partCount; part++) {
      if (!completed.has(part)) this.scheduler.enqueue(clientId, () => this.uploadPart(clientId, part))
    }
  }

  private async uploadPart(clientId: string, partNumber: number) {
    let item = this.getItem(clientId)
    if (!item?.file || !item.session || !item.serverUploadId || item.status !== 'UPLOADING' || item.completedParts.includes(partNumber)) return
    const controller = new AbortController()
    const byPart = this.controllers.get(clientId) ?? new Map<number, AbortController>()
    byPart.set(partNumber, controller); this.controllers.set(clientId, byPart)
    this.replace(clientId, { ...item, activeParts: [...new Set([...item.activeParts, partNumber])] })
    const range = partRange(partNumber, item.session.chunkSize, item.file.size)
    const uploadId = item.serverUploadId
    const file = item.file
    try {
      for (let attempt = 1; attempt <= MAX_CHUNK_ATTEMPTS; attempt++) {
        try {
          await this.transport.upload({
            uploadId, partNumber, chunk: file.slice(range.start, range.end), csrfToken: getCsrfToken(), signal: controller.signal,
            onProgress: (loaded) => this.onProgress(clientId, partNumber, Math.min(range.length, loaded), loaded >= range.length),
          })
          item = this.getItem(clientId)
          if (!item || item.status !== 'UPLOADING') return
          this.autoRecoveredCsrf.delete(clientId)
          const completedParts = [...new Set([...item.completedParts, partNumber])].sort((a, b) => a - b)
          this.replace(clientId, { ...item, completedParts, transferredByPart: { ...item.transferredByPart, [partNumber]: 0 } })
          this.recalculate(clientId)
          if (completedParts.length === item.session!.partCount) void this.complete(clientId)
          return
        } catch (cause) {
          if (controller.signal.aborted) return
          const error = uploadError(cause)
          if (error.status === 401) { notifyAuthExpired(); this.fail(clientId, cause); return }
          // CSRF staleness must not enter the backoff loop replaying the same
          // token; fail immediately so the bounded recovery path can refresh.
          if (error.code === 'CSRF_INVALID') { this.fail(clientId, cause); return }
          if (!error.retryable || attempt === MAX_CHUNK_ATTEMPTS) { this.fail(clientId, cause); return }
          try {
            await waitForRetry(retryDelay(attempt), controller.signal)
          } catch {
            // Pause aborts the backoff wait; that is a normal cancellation,
            // not a chunk failure, so leave quietly (finally still books the
            // part as inactive) instead of rejecting uploadPart.
            return
          }
        }
      }
    } finally {
      byPart.delete(partNumber)
      item = this.getItem(clientId)
      if (item) this.replace(clientId, { ...item, activeParts: item.activeParts.filter((value) => value !== partNumber) })
    }
  }

  private async complete(clientId: string) {
    let item = this.getItem(clientId)
    if (!item?.serverUploadId || !item.session || ['COMPLETED', 'CANCELED', 'EXPIRED'].includes(item.status)) return
    this.scheduler.remove(clientId)
    this.replace(clientId, { ...item, status: 'COMPLETING', sentBytes: item.size, progress: 1 })
    try {
      const session = await this.api.complete(item.serverUploadId)
      this.markCompleted(clientId, session)
    } catch (cause) {
      try {
        item = this.getItem(clientId)
        if (!item?.serverUploadId) return
        const session = await this.api.get(item.serverUploadId)
        if (session.status === 'COMPLETED') { this.markCompleted(clientId, session); return }
        if (session.status === 'FAILED') { this.markServerFailed(clientId, session); return }
        if (session.status === 'COMPLETING') {
          this.replace(clientId, { ...item, session, status: 'FAILED', error: '文件暂时无法完成入库，可以重试。', errorCode: 'UPLOAD_FINALIZE_RETRYABLE', retryable: true })
          return
        }
        this.replace(clientId, { ...item, session, completedParts: [...session.completedParts], status: 'PAUSED' })
      } catch { this.fail(clientId, cause) }
    }
  }

  private markCompleted(clientId: string, session: UploadSession) {
    const item = this.getItem(clientId)
    if (!item) return
    this.replace(clientId, { ...item, serverUploadId: session.id, session, completedParts: [...session.completedParts], activeParts: [], transferredByPart: {}, sentBytes: item.size, progress: 1, status: 'COMPLETED' })
    this.persist(this.getItem(clientId)!)
  }

  private markServerFailed(clientId: string, session: UploadSession) {
    const item = this.getItem(clientId)
    if (!item) return
    this.scheduler.remove(clientId)
    this.abort(clientId)
    this.replace(clientId, { ...item, session, completedParts: [...session.completedParts], activeParts: [], transferredByPart: {}, status: 'FAILED', error: SERVER_FAILED_MESSAGE, errorCode: 'UPLOAD_SERVER_FAILED', retryable: false })
  }

  private async validateReselection(item: UploadItem, file: File) {
    if (!item.session || file.name !== item.session.originalFilename || file.size !== item.session.expectedSize || file.lastModified !== item.lastModified) return '选择的文件与原上传文件不一致。'
    if (item.fingerprint && await fingerprintFile(file) !== item.fingerprint) return '选择的文件与原上传文件不一致。'
    this.replace(item.clientId, { ...item, file })
    return ''
  }

  private fail(clientId: string, cause: unknown, override?: string) {
    const item = this.getItem(clientId)
    if (!item || item.status === 'CANCELED') return
    this.scheduler.remove(clientId); this.abort(clientId)
    const error = uploadError(cause)
    if (error.code === 'UPLOAD_EXPIRED' || error.status === 410) { this.expire(clientId); return }
    this.replace(clientId, { ...item, activeParts: [], transferredByPart: {}, status: 'FAILED', error: override ?? error.message, errorCode: error.code, retryable: error.retryable })
    if (error.code === 'CSRF_INVALID') void this.recoverCsrf(clientId, getCsrfToken())
  }

  private expire(clientId: string) {
    const item = this.getItem(clientId)
    if (!item) return
    if (item.serverUploadId && this.userId) this.ledger.remove(this.userId, item.serverUploadId)
    this.replace(clientId, { ...item, activeParts: [], transferredByPart: {}, status: 'EXPIRED', error: '上传任务已过期，请重新上传。', errorCode: 'UPLOAD_EXPIRED', retryable: false })
  }

  // Bounded CSRF recovery: refresh the session token once, then continue at
  // most one automatic resume per failure episode. A repeat failure lands in
  // FAILED with an explicit user retry so the chunk can never loop forever.
  private async recoverCsrf(clientId: string, staleToken: string | undefined) {
    if (this.autoRecoveredCsrf.has(clientId)) return
    const fresh = await this.refreshCsrfToken()
    if (!fresh || fresh === staleToken) return
    this.autoRecoveredCsrf.add(clientId)
    await this.resume(clientId)
  }

  // User-triggered episode boundary for CSRF_INVALID: resets the marker so
  // the retry is allowed exactly one fresh token refresh before resuming.
  private async retryAfterCsrfRefresh(clientId: string) {
    this.autoRecoveredCsrf.delete(clientId)
    await this.refreshCsrfToken()
    await this.resume(clientId)
  }

  private async refreshCsrfToken(): Promise<string | undefined> {
    this.csrfRefreshInFlight ??= Promise.resolve(this.csrfRefresh()).then(() => getCsrfToken(), () => undefined)
    try { return await this.csrfRefreshInFlight } finally { this.csrfRefreshInFlight = undefined }
  }

  private onProgress(clientId: string, partNumber: number, loaded: number, final: boolean) {
    const item = this.getItem(clientId)
    if (!item || item.status !== 'UPLOADING') return
    const now = performance.now()
    if (!final && now - (this.lastProgressUpdate.get(clientId) ?? 0) < 100) return
    this.lastProgressUpdate.set(clientId, now)
    this.replace(clientId, { ...item, transferredByPart: { ...item.transferredByPart, [partNumber]: loaded } })
    this.recalculate(clientId)
  }

  private recalculate(clientId: string) {
    const item = this.getItem(clientId)
    if (!item?.session) return
    const durable = this.durableBytes(item.session, item.completedParts)
    const transferring = Object.values(item.transferredByPart).reduce((sum, value) => sum + value, 0)
    const sentBytes = Math.min(item.size, durable + transferring)
    this.replace(clientId, { ...item, sentBytes, progress: item.size === 0 ? (item.status === 'COMPLETED' ? 1 : 0) : sentBytes / item.size })
  }

  private durableBytes(session: UploadSession, completed = session.completedParts) {
    return completed.reduce((sum, part) => sum + partRange(part, session.chunkSize, session.expectedSize).length, 0)
  }

  private abort(clientId: string) {
    for (const controller of this.controllers.get(clientId)?.values() ?? []) controller.abort()
    this.controllers.delete(clientId)
  }

  private persist(item: UploadItem) {
    if (!this.userId || !item.serverUploadId) return
    try {
      this.ledger.upsert(this.userId, { uploadId: item.serverUploadId, lastModified: item.lastModified, fingerprint: item.fingerprint, createdAt: item.createdAt })
      if (!this.ledger.available) uploadState.ledgerWarning = true
    } catch { /* resume persistence is best-effort and never affects the upload */ }
  }

  private replace(clientId: string, item: UploadItem) {
    const index = uploadState.items.findIndex((candidate) => candidate.clientId === clientId)
    if (index >= 0) uploadState.items[index] = item
  }
}

export const uploadManager = new UploadManager()
