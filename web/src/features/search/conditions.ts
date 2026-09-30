import type { LocationQuery, LocationQueryRaw } from 'vue-router'
import { SearchConditions, Lifecycle } from '@/api/generated'
import { hasShortSearchToken } from './validation'

export function readConditions(query: LocationQuery): SearchConditions {
  const str = (key: string) => typeof query[key] === 'string' ? query[key] as string : ''
  const tag = query.tagId
  return {
    q: str('q'), lifecycle: str('lifecycle') as SearchConditions.lifecycle,
    favorite: str('favorite') === 'true', tagIds: Array.isArray(tag) ? tag.filter((v): v is string => typeof v === 'string') : typeof tag === 'string' ? [tag] : [],
    type: str('type') as SearchConditions.type, time: (str('time') || 'all') as SearchConditions.time,
    from: str('from'), to: str('to'), timezone: (str('timezone') || 'Asia/Shanghai') as SearchConditions.timezone,
  }
}
export function conditionsQuery(c: SearchConditions): LocationQueryRaw {
  return { q: c.q || undefined, lifecycle: c.lifecycle || undefined, favorite: c.favorite ? 'true' : undefined,
    tagId: c.tagIds.length ? c.tagIds : undefined, type: c.type || undefined, time: c.time,
    from: c.time === 'custom' ? c.from : undefined, to: c.time === 'custom' ? c.to : undefined, timezone: c.timezone }
}
export function localInstant(value: string, timezone: string): string | undefined {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(value)) return
  const date = new Date(`${value}:00${timezone === 'UTC' ? 'Z' : '+08:00'}`)
  if (!Number.isFinite(date.getTime())) return
  // Reject normalized invalid calendar dates (e.g. February 30).
  const local = new Date(date.getTime() + (timezone === 'UTC' ? 0 : 8 * 3600000)).toISOString().slice(0, 16)
  return local === value ? date.toISOString() : undefined
}
export function conditionError(c: SearchConditions): string {
  if (hasShortSearchToken(c.q)) return '每个搜索词至少 2 个字符'
  if (new TextEncoder().encode(c.q).length > 1024 || c.q.trim().split(/\s+/).filter(Boolean).length > 16 || c.q.trim().split(/\s+/).some(v => [...v].length > 128)) return '搜索词过长或数量过多'
  if (!['', 'TEMPORARY', 'PERMANENT'].includes(c.lifecycle) || !['', 'TEXT', 'MARKDOWN', 'CODE'].includes(c.type) || !['all', '24h', '7d', '30d', 'custom'].includes(c.time) || !['UTC', 'Asia/Shanghai'].includes(c.timezone)) return '筛选条件已失效，请重新选择后搜索或更新视图'
  if (c.tagIds.length > 100 || c.tagIds.some(id => !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(id))) return '标签条件已失效，请重新选择标签'
  if (c.time === 'custom') {
    const from = localInstant(c.from, c.timezone), to = localInstant(c.to, c.timezone)
    if (!from || !to || from >= to) return '请填写有效日期范围，起始时间必须早于结束时间'
  } else if (c.from || c.to) return '日期条件与时间选项不一致，请重新选择'
  return ''
}
export function searchFilters(c: SearchConditions, now: number) {
  const days: Record<string, number> = { '24h': 1, '7d': 7, '30d': 30 }
  return { search: { q: c.q.trim() || undefined, lifecycle: c.lifecycle === 'TEMPORARY' ? Lifecycle.TEMPORARY : c.lifecycle === 'PERMANENT' ? Lifecycle.PERMANENT : undefined,
    favorite: c.favorite || undefined, tagIds: c.tagIds.length ? c.tagIds : undefined, type: c.type || undefined,
    createdAfter: c.time === 'custom' ? localInstant(c.from, c.timezone) : days[c.time] ? new Date(now - days[c.time] * 86400000).toISOString() : undefined,
    createdBefore: c.time === 'custom' ? localInstant(c.to, c.timezone) : undefined } }
}

export function queryError(query: LocationQuery): string {
  const scalar = ['q', 'lifecycle', 'favorite', 'type', 'time', 'from', 'to', 'timezone', 'saved']
  if (scalar.some(key => query[key] !== undefined && typeof query[key] !== 'string') || (query.favorite !== undefined && !['true', 'false'].includes(query.favorite as string))) return 'URL 筛选条件无效，请重新选择筛选后搜索'
  if (query.tagId !== undefined && (Array.isArray(query.tagId) ? query.tagId.some(v => typeof v !== 'string') : typeof query.tagId !== 'string')) return 'URL 标签条件无效，请重新选择标签'
  return ''
}
