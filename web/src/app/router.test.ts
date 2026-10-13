import { describe, expect, it } from 'vitest'
import { router } from './router'

describe('application route compatibility', () => {
  it('keeps feed routes and redirects legacy detail pages to the single interface', () => {
    const routes = router.getRoutes()
    const paths = routes.map((route) => route.path)
    expect(paths).toEqual(expect.arrayContaining([
      '/temporary', '/permanent', '/favorites', '/tags/:id', '/trash', '/search', '/messages/:id', '/admin/:pathMatch(.*)*',
    ]))
    expect(routes.find((route) => route.name === 'message-detail')?.redirect).toBe('/temporary')
    expect(routes.find((route) => route.name === 'admin')?.meta).toMatchObject({ admin: true, shell: 'full' })
  })

  it.each([
    ['/temporary', {}], ['/permanent', {}], ['/favorites', {}], ['/tags/tag-1', {}], ['/trash', {}], ['/search', { q: 'relay' }],
  ])(
    'preserves detail selection on %s',
    (path, query) => {
      const resolved = router.resolve({ path, query: { ...query, detail: 'message-1' } })
      expect(resolved.query.detail).toBe('message-1')
    },
  )
})
