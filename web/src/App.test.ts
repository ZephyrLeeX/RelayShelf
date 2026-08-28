import { describe, expect, it } from 'vitest'
import { router } from '@/app/router'

describe('route table', () => {
  it.each(['/temporary', '/permanent', '/favorites', '/tags/tag-1', '/trash', '/search', '/messages/message-1', '/admin/status'])('resolves %s', (path) => {
    expect(router.resolve(path).matched.length).toBeGreaterThan(0)
  })
  it('redirects the root route to temporary', () => {
    expect(router.resolve('/').matched.at(-1)?.redirect).toBe('/temporary')
  })
})
