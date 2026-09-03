import { computed, shallowRef } from 'vue'
import type { StorageRuntimeStatus } from '@/api/generated'
import { queryClient } from '@/app/queryClient'
import { queryKeys } from '@/shared/api/queryKeys'

export const storageRuntimeStatus = shallowRef<StorageRuntimeStatus>()
export const storageAvailable = computed(() => storageRuntimeStatus.value?.healthy !== false)

export function setStorageRuntimeStatus(status?: StorageRuntimeStatus) {
  storageRuntimeStatus.value = status
}

export function refreshStorageRuntimeStatus() {
  return queryClient.invalidateQueries({ queryKey: queryKeys.storage.status() })
}

export function notifyStorageUnavailable() {
  void refreshStorageRuntimeStatus()
}
