import type { LocationQuery } from 'vue-router'
import { describe, expect, it } from 'vitest'
import { conditionError, conditionsQuery, localInstant, readConditions, searchFilters } from './conditions'
import { SearchConditions } from '@/api/generated'

describe('saved search conditions', () => {
  it('round trips URL filters without a result snapshot', () => {
    const c = readConditions({ q: 'hello 世界', lifecycle: 'PERMANENT', favorite: 'true', tagId: ['aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'], type: 'CODE', time: '7d' })
    expect(readConditions(conditionsQuery(c) as never)).toEqual(c)
    const first = searchFilters(c, Date.parse('2026-09-30T00:00:00Z'))
    const later = searchFilters(c, Date.parse('2026-10-01T00:00:00Z'))
    expect(first.search.createdAfter).toBe('2026-09-23T00:00:00.000Z')
    expect(later.search.createdAfter).toBe('2026-09-24T00:00:00.000Z')
  })
  it('uses explicit timezone and half-open date boundaries', () => {
    const c = readConditions({ time: 'custom', from: '2026-09-01T00:00', to: '2026-09-02T00:00', timezone: 'Asia/Shanghai' })
    expect(conditionError(c)).toBe('')
    expect(searchFilters(c, 0).search).toMatchObject({ createdAfter: '2026-08-31T16:00:00.000Z', createdBefore: '2026-09-01T16:00:00.000Z' })
    expect(localInstant('2026-02-30T00:00', 'UTC')).toBeUndefined()
    expect(conditionError({ ...c, to: c.from })).toContain('起始时间')
  })
  it('rejects unsupported filters rather than dropping them', () => {
    for (const query of ([{ type: 'BOGUS' }, { lifecycle: 'TRASH' }, { time: 'invalid' }, { timezone: 'invalid' }, { tagId: 'bad-id' }, { time: 'custom' }, { q: 'x' }] as LocationQuery[])) {
      expect(conditionError(readConditions(query))).not.toBe('')
    }
    const c = readConditions({ type: 'MARKDOWN' })
    expect(c.type).toBe(SearchConditions.type.MARKDOWN)
  })
})
