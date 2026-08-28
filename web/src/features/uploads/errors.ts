import { toApiError } from '@/shared/api/errors'

const messages: Record<string, string> = {
  UPLOAD_FILE_TOO_LARGE: '文件超过系统允许的大小。',
  UPLOAD_STAGING_FULL: '服务器临时上传空间不足。',
  UPLOAD_STAGING_UNAVAILABLE: '服务器临时上传空间暂不可用。',
  STORAGE_QUOTA_EXCEEDED: '存储空间已达到限制。',
  UPLOAD_EXPIRED: '上传任务已过期，请重新上传。',
  UPLOAD_NOT_FOUND: '上传任务不存在或已被清理。',
  UPLOAD_INVALID_STATE: '上传任务当前无法继续。',
  UPLOAD_PART_OUT_OF_RANGE: '上传分片超出范围。',
  UPLOAD_PART_SIZE_MISMATCH: '上传分片大小不正确。',
  UPLOAD_INCOMPLETE: '仍有分片未完成。',
  UPLOAD_STAGING_CORRUPT: '服务器临时上传数据损坏，请重新上传。',
  UPLOAD_FINALIZE_RETRYABLE: '文件暂时无法完成入库，可以重试。',
  STORAGE_UNAVAILABLE: '文件存储暂不可用，可以稍后重试。',
  AUTH_REQUIRED: '登录已失效，请重新登录。',
  AUTH_SESSION_EXPIRED: '登录已过期，请重新登录。',
  CSRF_INVALID: '安全令牌已更新，请重新继续上传。',
  NETWORK_ERROR: '网络中断，上传已暂停。',
}

export function uploadError(error: unknown) {
  const chunk = error && typeof error === 'object' && 'status' in error && 'code' in error
    ? error as { status: number; code: string }
    : null
  const adapted = chunk ? { ...chunk, message: '' } : toApiError(error)
  const retryable = adapted.status === 0 || adapted.status >= 500 || ['UPLOAD_FINALIZE_RETRYABLE', 'STORAGE_UNAVAILABLE'].includes(adapted.code)
  return { code: adapted.code, message: messages[adapted.code] ?? '上传失败，请重试。', status: adapted.status, retryable }
}
