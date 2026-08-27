import { describe, expect, it } from 'vitest'
import { hasShortSearchToken } from './validation'

describe('search validation', () => {
  it('allows two-code-point Unicode tokens', () => expect(hasShortSearchToken('中文 postgres')).toBe(false))
  it('rejects a one-character token', () => expect(hasShortSearchToken('中')).toBe(true))
  it('rejects any short token in a multi-token query', () => expect(hasShortSearchToken('postgres a')).toBe(true))
})
