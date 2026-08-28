const SAMPLE = 64 * 1024

export async function fingerprintFile(file: File) {
  const firstEnd = Math.min(file.size, SAMPLE)
  const parts: BlobPart[] = [await file.slice(0, firstEnd).arrayBuffer()]
  if (file.size > SAMPLE) {
    const lastStart = Math.max(firstEnd, file.size - SAMPLE)
    if (lastStart < file.size) parts.push(await file.slice(lastStart).arrayBuffer())
  }
  parts.push(new TextEncoder().encode(`:${file.size}`))
  const bytes = await new Blob(parts).arrayBuffer()
  const hash = await crypto.subtle.digest('SHA-256', bytes)
  return Array.from(new Uint8Array(hash), (value) => value.toString(16).padStart(2, '0')).join('')
}
