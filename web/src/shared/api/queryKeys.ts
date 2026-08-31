import type { Lifecycle } from '@/api/generated'

export interface MessageFilters {
  lifecycle?: Lifecycle
  favorite?: boolean
  tagIds?: string[]
  trash?: boolean
  search?: {
    q?: string
    lifecycle?: Lifecycle
    favorite?: boolean
    tagIds?: string[]
    type?: string
    createdAfter?: string
    createdBefore?: string
  }
}

export const queryKeys = {
  auth: { session: () => ['auth', 'session'] as const },
  messages: {
    root: () => ['messages'] as const,
    lists: () => ['messages', 'list'] as const,
    list: (filters: MessageFilters) => ['messages', 'list', filters] as const,
    details: () => ['messages', 'detail'] as const,
    detail: (id: string) => ['messages', 'detail', id] as const,
  },
  tags: { all: () => ['tags'] as const },
  search: {
    root: () => ['search'] as const,
    results: (filters: NonNullable<MessageFilters['search']>) => ['search', filters] as const,
  },
  trash: { list: () => ['trash'] as const },
  sessions: { all: () => ['sessions'] as const, devices: () => ['devices'] as const },
  recipients: { list: (query: string) => ['recipients', query] as const },
  admin: {
    status: () => ['admin', 'status'] as const,
    storage: () => ['admin', 'storage'] as const,
    settings: () => ['admin', 'settings'] as const,
    users: () => ['admin', 'users'] as const,
  },
}
