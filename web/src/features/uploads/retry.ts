export const MAX_CHUNK_ATTEMPTS = 5

export function retryDelay(attempt: number, random = Math.random) {
  const base = Math.min(8_000, 500 * 2 ** Math.max(0, attempt - 1))
  return Math.round(base * (0.9 + random() * 0.2))
}

export function waitForRetry(ms: number, signal: AbortSignal) {
  return new Promise<void>((resolve, reject) => {
    const timer = window.setTimeout(resolve, ms)
    signal.addEventListener('abort', () => { clearTimeout(timer); reject(new DOMException('Aborted', 'AbortError')) }, { once: true })
  })
}
