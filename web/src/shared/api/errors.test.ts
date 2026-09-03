import { describe, expect, it } from 'vitest'
import { getApiErrorCode, getTraceId, isStorageUnavailable, toApiError } from './errors'

describe('API error normalization', () => {
  it('purely recognizes the backend storage error body', () => {
    const body = { code: 'STORAGE_UNAVAILABLE', message: 'storage is unavailable', traceId: 'abc' }
    expect(isStorageUnavailable(body)).toBe(true)
    expect(getApiErrorCode(body)).toBe('STORAGE_UNAVAILABLE')
    expect(getTraceId(body)).toBe('abc')
    expect(toApiError(body).message).toBe('storage is unavailable')
  })
})
