export function formatBytes(value: number) {
  if (!Number.isFinite(value) || value < 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let size = value
  let unit = 0
  while (size >= 1024 && unit < units.length - 1) { size /= 1024; unit++ }
  return `${size < 10 && unit > 0 ? size.toFixed(1) : Math.round(size)} ${units[unit]}`
}

export function bytesToUnit(value: number, unitBytes: number) {
  return Number((value / unitBytes).toFixed(2))
}

export function unitToBytes(value: number | string, unitBytes: number) {
  return Math.round(Number(value) * unitBytes)
}
