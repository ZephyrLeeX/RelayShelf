/** Select the unit before rounding; keep the existing render refresh cadence. */
export function relativeExpiry(value?: string | null, now = Date.now()): string {
  if (!value) return ''
  const remaining = new Date(value).getTime() - now
  if (remaining <= 0) return '已过期'
  if (remaining >= 86_400_000) return `剩余 ${Math.ceil(remaining / 86_400_000)} 天`
  if (remaining >= 3_600_000) return `剩余 ${Math.ceil(remaining / 3_600_000)} 小时`
  return `剩余 ${Math.ceil(remaining / 60_000)} 分钟`
}
