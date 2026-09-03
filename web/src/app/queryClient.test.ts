import { afterEach, describe, expect, it, vi } from 'vitest'
import { queryClient, refreshStorageStatusOnError } from './queryClient'

describe('global API error handling', () => {
  afterEach(() => vi.restoreAllMocks())
  it('turns one storage business error into one logical status refresh', () => {
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const error = { status: 503, code: 'STORAGE_UNAVAILABLE', message: 'storage is unavailable' }
    refreshStorageStatusOnError(error)
    expect(invalidate).toHaveBeenCalledTimes(1)
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['storage', 'status'] })
  })

  it('refreshes to confirm finalize retryability without assuming outage', () => {
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    refreshStorageStatusOnError({ status: 503, code: 'UPLOAD_FINALIZE_RETRYABLE', message: 'retry' })
    expect(invalidate).toHaveBeenCalledTimes(1)
  })
})
