import type { UploadItem } from './types'

export function uploadStatusLabel(status: UploadItem['status']) {
  return ({
    QUEUED: '等待中', CREATING: '准备中', UPLOADING: '上传中', COMPLETING: '完成中', PAUSED: '已暂停',
    COMPLETED: '已完成', FAILED: '失败', EXPIRED: '已过期', CANCELED: '已取消',
  } satisfies Record<UploadItem['status'], string>)[status]
}
