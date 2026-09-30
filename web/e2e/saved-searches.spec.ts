import { expect, test } from '@playwright/test'
import { alice, bob, composeAndSend, login, marker } from './helpers'

test('saved search desktop/mobile management, cross-device persistence and owner isolation', async ({ page, browser }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await login(page, alice)
  await page.goto('/search')
  const name = marker('saved-view'), renamed = `${name}-renamed`
  await page.getByRole('textbox', { name: '搜索词' }).fill('saved keyword')
  await page.getByLabel('时间', { exact: true }).selectOption('7d')
  await page.locator('summary').filter({ hasText: '更多筛选' }).click()
  await page.getByLabel('内容类型', { exact: true }).selectOption('CODE')
  await page.getByLabel('视图名称').fill(name)
  await page.getByRole('button', { name: '另存为新视图' }).click()
  await expect(page.getByRole('heading', { name: `当前视图：${name}` })).toBeVisible()
  const savedId = new URL(page.url()).searchParams.get('saved')!
  await expect(page.locator('.app-sidebar').getByRole('link', { name, exact: true })).toBeVisible()
  await page.getByRole('textbox', { name: '搜索词' }).fill('unsaved keyword')
  await page.getByLabel('视图名称').fill(renamed)
  await page.getByRole('button', { name: '重命名', exact: true }).click()
  await expect(page.getByRole('heading', { name: `当前视图：${renamed}` })).toBeVisible()
  expect((await (await page.request.get('/api/v1/saved-searches')).json()).find((v: { id: string }) => v.id === savedId).conditions.q).toBe('saved keyword')
  await page.getByRole('textbox', { name: '搜索词' }).fill('updated keyword')
  await page.getByRole('button', { name: '搜索', exact: true }).click()
  let stored = await (await page.request.get('/api/v1/saved-searches')).json()
  expect(stored.find((v: { id: string }) => v.id === savedId).conditions.q).toBe('saved keyword')
  await page.getByRole('button', { name: '更新当前视图条件' }).click()
  await expect(page.getByRole('status').filter({ hasText: '当前视图条件已更新' })).toBeVisible()
  // A separate authenticated browser context simulates a second device.
  const otherContext = await browser.newContext({ viewport: { width: 390, height: 844 } })
  const other = await otherContext.newPage()
  await login(other, alice)
  await other.getByRole('button', { name: '我的', exact: true }).click()
  await other.getByRole('dialog').getByRole('link', { name: renamed, exact: true }).click()
  await expect(other.getByRole('textbox', { name: '搜索词' })).toHaveValue('updated keyword')
  expect(new URL(other.url()).searchParams.get('time')).toBe('7d')
  // Custom range sends exact inclusive/exclusive UTC instants to the existing API.
  await other.getByLabel('时间', { exact: true }).selectOption('custom')
  await other.getByLabel('起始时间（包含）').fill('2026-09-01T00:00')
  await other.getByLabel('结束时间（不包含）').fill('2026-10-01T00:00')
  await other.getByLabel('时区', { exact: true }).selectOption('Asia/Shanghai')
  const searched = other.waitForRequest(r => r.url().includes('/api/v1/search?') && r.url().includes('createdBefore'))
  await other.getByRole('button', { name: '搜索', exact: true }).click()
  const searchUrl = new URL((await searched).url())
  expect(searchUrl.searchParams.get('createdAfter')).toBe('2026-08-31T16:00:00.000Z')
  expect(searchUrl.searchParams.get('createdBefore')).toBe('2026-09-30T16:00:00.000Z')
  expect(await other.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await otherContext.close()
  const bobContext = await browser.newContext()
  const bobPage = await bobContext.newPage()
  await login(bobPage, bob)
  stored = await (await bobPage.request.get('/api/v1/saved-searches')).json()
  expect(stored.some((v: { id: string }) => v.id === savedId)).toBe(false)
  await bobPage.goto(`/search?saved=${savedId}`)
  await expect(bobPage.getByRole('alert').filter({ hasText: '保存视图已删除或不可用' })).toBeVisible()
  await bobContext.close()
  await page.getByRole('button', { name: '删除视图', exact: true }).click()
  await expect(page.getByRole('heading', { name: '保存搜索', exact: true })).toBeVisible()
  expect((await (await page.request.get('/api/v1/saved-searches')).json()).some((v: { id: string }) => v.id === savedId)).toBe(false)
})


test('editor code survives serialization and is exclusive from ordinary Markdown in search', async ({ page }) => {
  await login(page, alice)
  const keyword = marker('code-search')
  const sent = page.waitForResponse(r => r.url().endsWith('/api/v1/messages') && r.request().method() === 'POST')
  await composeAndSend(page, keyword + '\nprint("``` embedded")' , { contentType: 'Python', lifecycle: 'PERMANENT' })
  const message = await (await sent).json()
  expect(message.bodyFormat).toBe('MARKDOWN')
  expect(message.body).toContain('````python')
  await expect(page.locator('#composer-body')).toHaveValue('')
  await composeAndSend(page, '# ' + keyword + ' prose\n```bash\necho hi\n```', { contentType: 'Markdown', lifecycle: 'PERMANENT' })
  await expect(page.locator('#composer-body')).toHaveValue('')
  await page.goto(`/search?q=${keyword}&type=CODE`)
  await expect(page.locator('.message-card')).toHaveCount(1)
  await expect(page.locator('.message-card')).toContainText('embedded')
  await page.goto(`/search?q=${keyword}&type=MARKDOWN`)
  await expect(page.locator('.message-card')).toHaveCount(1)
  await expect(page.locator('.message-card')).toContainText('prose')
})

for (const missing of [false, true]) {
  test(`malformed saved tags (${missing ? 'missing' : 'null'}) pause search and allow repair/delete`, async ({ page }) => {
    await login(page, alice)
    const id = 'aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa'
    const conditions: Record<string, unknown> = { q: 'restricted', lifecycle: '', favorite: false, type: 'CODE', time: 'all', from: '', to: '', timezone: 'UTC' }
    if (!missing) conditions.tagIds = null
    let views = [{ id, name: '异常视图', conditions, invalidReason: '保存的条件无法读取，请删除或更新视图。', updatedAt: '2026-09-30T00:00:00Z' }]
    await page.route(/\/api\/v1\/saved-searches(?:\/[^?]+)?(?:\?.*)?$/, async route => {
      const request = route.request()
      if (request.method() === 'GET') await route.fulfill({ json: views })
      else if (request.method() === 'PUT') {
        views = [{ ...views[0]!, ...request.postDataJSON(), invalidReason: '' }]
        await route.fulfill({ json: views[0] })
      } else if (request.method() === 'DELETE') { views = []; await route.fulfill({ status: 204 }) }
      else await route.continue()
    })
    let searchRequests = 0
    page.on('request', r => { if (new URL(r.url()).pathname === '/api/v1/search') searchRequests++ })
    await page.goto(`/search?saved=${id}`)
    await expect(page.getByRole('alert').filter({ hasText: '保存的条件无法读取' })).toBeVisible()
    expect(searchRequests).toBe(0)
    await page.getByRole('textbox', { name: '搜索词' }).fill('repaired keyword')
    await page.getByRole('button', { name: '搜索', exact: true }).click()
    await expect(page.getByRole('alert').filter({ hasText: '保存的条件无法读取' })).toBeVisible()
    expect(searchRequests).toBe(0)
    const searched = page.waitForRequest(r => new URL(r.url()).pathname === '/api/v1/search')
    await page.getByRole('button', { name: '更新当前视图条件' }).click()
    expect(new URL((await searched).url()).searchParams.get('q')).toBe('repaired keyword')
    expect(views[0]!.conditions.tagIds).toEqual([])
    await page.getByRole('button', { name: '删除视图', exact: true }).click()
    await expect(page.getByRole('heading', { name: '保存搜索', exact: true })).toBeVisible()
    expect(views).toHaveLength(0)
  })
}
