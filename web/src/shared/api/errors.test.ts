import { describe, expect, it, vi } from 'vitest'
import { queryClient } from '@/app/queryClient'
import { getApiErrorCode, getTraceId, isStorageUnavailable, toApiError } from './errors'

describe('API error normalization', () => {
  it('recognizes the backend storage error body and refreshes global status', () => {
    const invalidate = vi.spyOn(queryClient, 'invalidateQueries').mockResolvedValue()
    const body = { code: 'STORAGE_UNAVAILABLE', message: 'storage is unavailable', traceId: 'abc' }
    expect(isStorageUnavailable(body)).toBe(true)
    expect(getApiErrorCode(body)).toBe('STORAGE_UNAVAILABLE')
    expect(getTraceId(body)).toBe('abc')
    expect(toApiError(body).message).toBe('storage is unavailable')
    expect(invalidate).toHaveBeenCalled()
  })
})
