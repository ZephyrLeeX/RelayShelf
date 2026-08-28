import type { AttachmentSummary } from '@/api/generated'

export const safeRasterMIMEs = new Set(['image/jpeg', 'image/png', 'image/gif', 'image/webp'])
export const unsafeActiveMIMEs = new Set(['text/html', 'application/xhtml+xml', 'image/svg+xml', 'application/xml', 'text/xml'])
export const mediaMIMEs = new Set(['audio/mpeg', 'audio/mp4', 'audio/ogg', 'audio/wav', 'audio/webm', 'video/mp4', 'video/webm', 'video/ogg', 'video/quicktime'])

export function previewKind(file: AttachmentSummary) {
  const mime = file.detectedMime.toLowerCase()
  if (safeRasterMIMEs.has(mime)) return 'image' as const
  if (mime === 'application/pdf') return 'pdf' as const
  if (mediaMIMEs.has(mime)) return mime.startsWith('audio/') ? 'audio' as const : 'video' as const
  if (!unsafeActiveMIMEs.has(mime) && (mime.startsWith('text/') || ['application/json', 'application/yaml', 'application/x-yaml', 'application/sql', 'application/javascript'].includes(mime))) return 'text' as const
  return 'download' as const
}

export const downloadURL = (id: string) => `/api/v1/attachments/${encodeURIComponent(id)}/download`
export const previewURL = (id: string) => `/api/v1/attachments/${encodeURIComponent(id)}/preview`
