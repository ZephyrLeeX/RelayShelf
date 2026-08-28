import { describe, expect, it } from 'vitest'
import { UploadScheduler } from './scheduler'

describe('UploadScheduler', () => {
  it('never exceeds four global or two per-file tasks', async () => {
    const scheduler = new UploadScheduler(4, 2)
    let global = 0; let globalMax = 0
    const perFile = new Map<string, number>(); const perFileMax = new Map<string, number>()
    const releases: Array<() => void> = []
    const done: Promise<void>[] = []
    for (const file of ['a', 'b', 'c']) for (let part = 0; part < 4; part++) {
      done.push(new Promise<void>((resolve) => scheduler.enqueue(file, async () => {
        global++; globalMax = Math.max(globalMax, global)
        perFile.set(file, (perFile.get(file) ?? 0) + 1)
        perFileMax.set(file, Math.max(perFileMax.get(file) ?? 0, perFile.get(file)!))
        await new Promise<void>((release) => releases.push(release))
        global--; perFile.set(file, perFile.get(file)! - 1); resolve()
      })))
    }
    while (releases.length < 4) await Promise.resolve()
    while (releases.length) { releases.shift()!(); await Promise.resolve(); await Promise.resolve() }
    while (done.some(() => scheduler.active > 0)) {
      while (releases.length) releases.shift()!()
      await Promise.resolve()
    }
    await Promise.all(done)
    expect(globalMax).toBeLessThanOrEqual(4)
    expect([...perFileMax.values()].every((value) => value <= 2)).toBe(true)
  })
})
