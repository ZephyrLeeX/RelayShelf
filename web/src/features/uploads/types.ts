import type { UploadSession } from '@/api/generated'

export type UploadStatus = 'QUEUED' | 'CREATING' | 'UPLOADING' | 'PAUSED' | 'COMPLETING' | 'COMPLETED' | 'FAILED' | 'EXPIRED' | 'CANCELED'

interface UploadBase {
  clientId: string
  file?: File
  serverUploadId?: string
  session?: UploadSession
  filename: string
  size: number
  lastModified: number
  fingerprint?: string
  completedParts: number[]
  activeParts: number[]
  transferredByPart: Record<number, number>
  sentBytes: number
  progress: number
  createdAt: string
  selected: boolean
}

export type UploadItem =
  | (UploadBase & { status: 'QUEUED' | 'CREATING' | 'UPLOADING' | 'PAUSED' | 'COMPLETING' })
  | (UploadBase & { status: 'COMPLETED'; serverUploadId: string; session: UploadSession })
  | (UploadBase & { status: 'FAILED'; error: string; errorCode: string; retryable: boolean })
  | (UploadBase & { status: 'EXPIRED'; error: string; errorCode: 'UPLOAD_EXPIRED'; retryable: false })
  | (UploadBase & { status: 'CANCELED' })

export interface ResumeEntry {
  uploadId: string
  lastModified: number
  fingerprint?: string
  createdAt: string
}

export interface PartRange { partNumber: number; start: number; end: number; length: number }

export interface ChunkRequest {
  uploadId: string
  partNumber: number
  chunk: Blob
  csrfToken?: string
  signal: AbortSignal
  onProgress: (loaded: number) => void
}

export interface ChunkTransport { upload(request: ChunkRequest): Promise<void> }
