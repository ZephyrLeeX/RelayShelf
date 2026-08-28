let handler: (() => void) | undefined

export function setAuthExpiredHandler(next?: () => void) {
  handler = next
}

export function notifyAuthExpired() {
  handler?.()
}
