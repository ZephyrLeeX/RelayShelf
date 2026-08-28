import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

// The committed production artifacts lock the Phase 9 PWA contract: static
// precache plus the app-shell navigation route only. Vitest runs with the
// package root (web/) as cwd, so the committed dist resolves relative to it.
const dist = resolve(process.cwd(), '..', 'internal', 'webui', 'dist')
const read = (name: string) => readFileSync(resolve(dist, name), 'utf8')

describe('generated PWA artifacts', () => {
  it('precaches static assets and registers only the app-shell navigation route', () => {
    const worker = read('sw.js')
    expect(worker).toContain('precacheAndRoute')
    expect(worker).toContain('NavigationRoute')
    expect(worker.match(/registerRoute\(/g)).toHaveLength(1)
  })

  it('contains no runtime private-API caching, message/search/sensitive/attachment caching, or Background Sync', () => {
    const worker = read('sw.js')
    expect(worker).not.toContain('/api/v1')
    expect(worker).not.toMatch(/backgroundSync|periodicSync|sync\./i)
    expect(worker).not.toMatch(/registerRoute\((?!.*NavigationRoute)/)
  })

  it('declares a standalone app-shell manifest with icons', () => {
    const manifest = JSON.parse(read('manifest.webmanifest')) as { display: string; start_url: string; scope: string; icons: unknown[] }
    expect(manifest.display).toBe('standalone')
    expect(manifest.start_url).toBe('/')
    expect(manifest.scope).toBe('/')
    expect(manifest.icons.length).toBeGreaterThanOrEqual(2)
  })
})
