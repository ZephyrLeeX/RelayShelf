import { BodyFormat, Lifecycle, type AuthBootstrap, type MessageSummary } from '@/api/generated'

export const authFixture: AuthBootstrap = {
  user: { id: 'user-1', username: 'alice', displayName: 'Alice', isAdmin: false },
  device: { id: 'device-1', name: 'Laptop', userAgent: 'test', firstSeenAt: '2026-01-01T00:00:00Z', lastSeenAt: '2026-01-01T00:00:00Z' },
  session: { id: 'session-1', deviceId: 'device-1', current: true, createdAt: '2026-01-01T00:00:00Z', lastSeenAt: '2026-01-01T00:00:00Z', expiresAt: '2026-02-01T00:00:00Z', absoluteExpiresAt: '2026-03-01T00:00:00Z' },
  csrfToken: 'csrf-a',
}

export function messageFixture(overrides: Partial<MessageSummary> = {}): MessageSummary {
  return {
    id: 'message-1', body: 'hello', bodyPreview: 'hello', bodyTruncated: false,
    bodyFormat: BodyFormat.TEXT, sensitive: false, lifecycle: Lifecycle.TEMPORARY,
    favorite: false, version: 1, createdAt: '2026-01-01T00:00:00Z', updatedAt: '2026-01-01T00:00:00Z',
    tags: [], attachments: [], attachmentCount: 0, ...overrides,
  }
}
