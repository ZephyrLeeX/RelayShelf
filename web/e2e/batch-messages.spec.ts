import { randomUUID } from 'node:crypto'
import { expect, test } from '@playwright/test'
import { alice, login, marker } from './helpers'

for (const width of [1440, 390]) {
  test(`batch selection, additive tags, conflict retry and restore at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await login(page, alice)
    const session = await (await page.request.get('/api/v1/auth/session')).json()
    const headers = { 'X-CSRF-Token': session.csrfToken, Origin: 'http://127.0.0.1:8080' }
    const prefix = marker('batch')
    const createTag = async (name: string) => {
      const response = await page.request.post('/api/v1/tags', { headers, data: { name, color: '#456789' } })
      expect(response.ok()).toBeTruthy()
      return response.json()
    }
    const oldTag = await createTag(`${prefix}-old`)
    const addedTag = await createTag(`${prefix}-added`)
    const messages: { id: string; version: number }[] = []
    for (let i = 0; i < 2; i++) {
      const response = await page.request.post('/api/v1/messages', {
        headers: { ...headers, 'Idempotency-Key': randomUUID() },
        data: { title: `${prefix}-${i}`, body: 'batch acceptance', lifecycle: 'TEMPORARY', tagIds: [oldTag.id], sensitive: i === 1 },
      })
      expect(response.ok()).toBeTruthy()
      messages.push(await response.json())
    }
    const toolbar = page.getByRole('region', { name: '批量操作' })
    for (const time of ['24h', '7d', '30d']) {
      await page.goto(`/search?q=${prefix}&time=${time}`)
      await expect(page.locator('.message-card')).toHaveCount(2)
      await page.getByRole('button', { name: '多选', exact: true }).click()
      await toolbar.getByRole('button', { name: '全选已加载内容' }).click()
      await expect(toolbar).toContainText('已选 2 项')
      await page.getByRole('textbox', { name: '搜索词' }).fill(`${prefix} draft`)
      await page.locator('.message-card').first().getByRole('button', { name: /打开内容/ }).click()
      await page.getByRole('button', { name: '关闭详情' }).click()
      await expect(toolbar).toContainText('已选 2 项')
      await expect(page.getByRole('textbox', { name: '搜索词' })).toHaveValue(`${prefix} draft`)
      await page.getByRole('textbox', { name: '搜索词' }).fill(prefix)
    }
    await toolbar.getByRole('button', { name: '取消选择' }).click()
    await expect(toolbar).toContainText('已选 0 项')
    await toolbar.getByRole('button', { name: '全选已加载内容' }).click()
    await toolbar.getByRole('button', { name: '批量添加标签', exact: true }).click()
    await toolbar.getByLabel(addedTag.name, { exact: true }).check()
    await toolbar.getByRole('button', { name: '追加所选标签' }).click()
    await expect(toolbar).toContainText('成功 2 · 失败 0')
    for (const message of messages) {
      const data = await (await page.request.get(`/api/v1/messages/${message.id}`)).json()
      expect(data.tags.map((tag: { id: string }) => tag.id).sort()).toEqual([oldTag.id, addedTag.id].sort())
      if (data.sensitive) expect(data.body).toBeNull()
    }
    // Pause a single outgoing command and apply a real concurrent edit to the same record.
    let conflicted = false
    const writes: string[] = []
    await page.route('**/api/v1/messages/*/make-permanent', async route => {
      const id = route.request().url().split('/').at(-2)!
      writes.push(id)
      if (!conflicted) {
        conflicted = true
        const current = await (await page.request.get(`/api/v1/messages/${id}`)).json()
        const response = await page.request.patch(`/api/v1/messages/${id}`, { headers, data: { expectedVersion: current.version, title: `${prefix}-updated` } })
        expect(response.ok()).toBeTruthy()
      }
      await route.continue()
    })
    await toolbar.getByRole('button', { name: '全选已加载内容' }).click()
    await toolbar.getByRole('button', { name: '批量转长期' }).click()
    await expect(toolbar).toContainText('成功 1 · 失败 1')
    await expect(toolbar).toContainText('已选 1 项')
    await expect(toolbar.getByLabel('逐项失败结果')).toContainText('其他设备修改')
    await toolbar.getByRole('button', { name: '只重试失败项' }).click()
    await expect(toolbar).toContainText('成功 1 · 失败 0')
    expect(writes).toHaveLength(3)
    expect(writes[2]).toBe(writes[0])
    await toolbar.getByRole('button', { name: '全选已加载内容' }).click()
    await toolbar.getByRole('button', { name: '批量移入回收站' }).click()
    await expect(toolbar).toContainText('成功 2 · 失败 0')
    await page.goto('/trash')
    await page.getByRole('button', { name: '多选', exact: true }).click()
    const trashToolbar = page.getByRole('region', { name: '批量操作' })
    await expect(trashToolbar).toContainText('已选 0 项')
    for (const message of messages) {
      const data = await (await page.request.get(`/api/v1/messages/${message.id}`)).json()
      await page.getByLabel(`选择 ${data.title}`, { exact: true }).check()
    }
    await expect(trashToolbar.getByRole('button', { name: /永久删除/ })).toHaveCount(0)
    await trashToolbar.getByRole('button', { name: '批量恢复' }).click()
    await expect(trashToolbar).toContainText('成功 2 · 失败 0')
    for (const message of messages) {
      const data = await (await page.request.get(`/api/v1/messages/${message.id}`)).json()
      expect(data.trashedAt).toBeFalsy()
      expect(data.lifecycle).toBe('PERMANENT')
    }
    await page.goto(`/search?q=${prefix}`)
    await expect(page.locator('.message-card')).toHaveCount(2)
    await page.getByRole('button', { name: '多选', exact: true }).click()
    await page.getByRole('button', { name: '全选已加载内容' }).click()
    await page.getByRole('combobox', { name: '区域', exact: true }).selectOption('TEMPORARY')
    await page.getByRole('button', { name: '搜索', exact: true }).click()
    await expect(page.getByRole('region', { name: '批量操作' })).toHaveCount(0)
    // Hold a second page made from real search records until selection completes.
    let releaseNextPage!: () => void
    const nextPage = new Promise<void>(resolve => { releaseNextPage = resolve })
    let searchItems: unknown[] = []
    await page.route('**/api/v1/search?**', async route => {
      if (new URL(route.request().url()).searchParams.has('cursor')) {
        await nextPage
        await route.fulfill({ json: { items: searchItems.slice(1), nextCursor: null } })
      } else {
        const response = await route.fetch()
        const result = await response.json()
        searchItems = result.items
        await route.fulfill({ json: { ...result, items: searchItems.slice(0, 1), nextCursor: 'held-next-page' } })
      }
    })
    await page.goto(`/search?q=${prefix}&time=24h`, { waitUntil: 'domcontentloaded' })
    await expect(page.locator('.message-card')).toHaveCount(1)
    await page.getByRole('button', { name: '多选', exact: true }).click()
    const loadedToolbar = page.getByRole('region', { name: '批量操作' })
    await loadedToolbar.getByRole('button', { name: '全选已加载内容' }).click()
    await expect(loadedToolbar).toContainText('已选 1 项')
    releaseNextPage()
    await page.locator('.sentinel').scrollIntoViewIfNeeded()
    await expect(page.locator('.message-card')).toHaveCount(2)
    await expect(page.locator('.selection-control input:checked')).toHaveCount(1)
    await loadedToolbar.getByRole('button', { name: '全选已加载内容' }).click()
    await expect(loadedToolbar).toContainText('已选 2 项')
  })
}
