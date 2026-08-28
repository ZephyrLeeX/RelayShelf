import { computed, reactive } from 'vue'
import type { UploadItem } from './types'

export const uploadState = reactive({ items: [] as UploadItem[], queueOpen: false, ledgerWarning: false })
export const hasActiveTransfers = computed(() => uploadState.items.some((item) => ['CREATING', 'UPLOADING', 'COMPLETING'].includes(item.status)))
export const visibleUploads = computed(() => uploadState.items.filter((item) => item.status !== 'CANCELED'))
// A reload destroys this tab's in-memory upload references. When the resume
// ledger is unavailable, items that can only be continued from here (PAUSED,
// retryable FAILED with a server session, or a COMPLETED upload not yet
// consumed by a message) make any reload destructive.
export const reloadUnsafe = computed(() => uploadState.ledgerWarning && uploadState.items.some((item) =>
  item.status === 'PAUSED'
  || item.status === 'COMPLETED'
  || (item.status === 'FAILED' && item.retryable && Boolean(item.serverUploadId))))
