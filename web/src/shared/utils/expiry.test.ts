import { describe, expect, it } from 'vitest'
import { relativeExpiry } from './expiry'

describe('relativeExpiry', () => {
  const now = Date.parse('2026-09-05T00:00:00Z')
  it.each([
    [51 * 3600, '剩余 3 天'],
    [24 * 3600, '剩余 1 天'],
    [23 * 3600 + 10 * 60, '剩余 24 小时'],
    [5 * 3600 + 2 * 60, '剩余 6 小时'],
    [3600, '剩余 1 小时'],
    [59 * 60 + 10, '剩余 60 分钟'],
    [12 * 60, '剩余 12 分钟'],
    [60, '剩余 1 分钟'],
    [15, '剩余 1 分钟'],
    [0, '已过期'],
    [-1, '已过期'],
  ])('formats %i seconds as %s', (seconds, expected) => {
    expect(relativeExpiry(new Date(now + seconds * 1000).toISOString(), now)).toBe(expected)
  })
})
