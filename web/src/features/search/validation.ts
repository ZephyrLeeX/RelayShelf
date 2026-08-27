export function hasShortSearchToken(value: string) {
  return value.trim().split(/\s+/u).filter(Boolean).some((token) => Array.from(token).length < 2)
}
