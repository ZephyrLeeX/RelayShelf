import { beforeEach, describe, expect, it } from 'vitest'
import { OpenAPI } from '@/api/generated'
import { configureApi, setCsrfToken } from './configure'

describe('generated API runtime configuration', () => {
  beforeEach(() => { setCsrfToken(undefined); configureApi() })
  it('includes cookie credentials', () => {
    expect(OpenAPI.WITH_CREDENTIALS).toBe(true)
    expect(OpenAPI.CREDENTIALS).toBe('include')
  })
  it('supplies CSRF only while it exists in memory', async () => {
    const headers = OpenAPI.HEADERS as (options: never) => Promise<Record<string, string>>
    expect(await headers(undefined as never)).toEqual({})
    setCsrfToken('token-a')
    expect(await headers(undefined as never)).toEqual({ 'X-CSRF-Token': 'token-a' })
    setCsrfToken(undefined)
    expect(await headers(undefined as never)).toEqual({})
  })
})
