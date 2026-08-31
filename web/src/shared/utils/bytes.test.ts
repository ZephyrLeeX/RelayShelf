import { describe, expect, it } from 'vitest'
import { bytesToUnit, formatBytes, unitToBytes } from './bytes'

describe('byte formatting', () => {
  it('uses readable units without exposing large byte counts', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(1024)).toBe('1.0 KB')
    expect(formatBytes(5 * 1024 * 1024)).toBe('5.0 MB')
    expect(formatBytes(2 * 1024 ** 3)).toBe('2.0 GB')
    expect(formatBytes(3 * 1024 ** 4)).toBe('3.0 TB')
  })

  it('converts friendly setting values without changing the byte API contract', () => {
    expect(bytesToUnit(2 * 1024 ** 3, 1024 ** 2)).toBe(2048)
    expect(unitToBytes(2048, 1024 ** 2)).toBe(2 * 1024 ** 3)
  })
})
