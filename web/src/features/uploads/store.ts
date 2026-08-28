import { computed, reactive } from 'vue'
import type { UploadItem } from './types'

export const uploadState = reactive({ items: [] as UploadItem[], queueOpen: false, ledgerWarning: false })
export const hasActiveTransfers = computed(() => uploadState.items.some((item) => ['CREATING', 'UPLOADING', 'COMPLETING'].includes(item.status)))
export const visibleUploads = computed(() => uploadState.items.filter((item) => item.status !== 'CANCELED'))
