import { notifyAuthExpired } from '@/shared/api/authExpiry'
import type { ChunkRequest, ChunkTransport } from './types'

export class ChunkHTTPError extends Error {
  constructor(readonly status: number, readonly code: string) { super(code) }
}

export class XHRChunkTransport implements ChunkTransport {
  upload(request: ChunkRequest) {
    return new Promise<void>((resolve, reject) => {
      const xhr = new XMLHttpRequest()
      xhr.open('PUT', `/api/v1/uploads/${encodeURIComponent(request.uploadId)}/parts/${request.partNumber}`)
      xhr.withCredentials = true
      xhr.setRequestHeader('Content-Type', 'application/octet-stream')
      if (request.csrfToken) xhr.setRequestHeader('X-CSRF-Token', request.csrfToken)
      xhr.upload.onprogress = (event) => request.onProgress(event.loaded)
      xhr.onerror = () => reject(new TypeError('network error'))
      xhr.onabort = () => reject(new DOMException('Aborted', 'AbortError'))
      xhr.onload = () => {
        if (xhr.status === 204) { resolve(); return }
        let code = 'HTTP_ERROR'
        try { code = (JSON.parse(xhr.responseText) as { code?: string }).code ?? code } catch { /* safe fallback */ }
        if (xhr.status === 401) notifyAuthExpired()
        reject(new ChunkHTTPError(xhr.status, code))
      }
      request.signal.addEventListener('abort', () => xhr.abort(), { once: true })
      xhr.send(request.chunk)
    })
  }
}
