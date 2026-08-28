export function formatBytes(value: number) {
  if (!Number.isFinite(value) || value < 0) return '0 Bytes'
  const units = ['Bytes', 'KiB', 'MiB', 'GiB']
  let size = value
  let unit = 0
  while (size >= 1024 && unit < units.length - 1) { size /= 1024; unit++ }
  return `${size < 10 && unit > 0 ? size.toFixed(1) : Math.round(size)} ${units[unit]}`
}
