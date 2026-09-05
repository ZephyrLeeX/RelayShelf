import { expect, test, type Page } from '@playwright/test'
import { alice, login, marker } from './helpers'

async function drag(page: Page, selector: string, type: string, filename?: string, contentType = 'text/plain') {
  return page.locator(selector).evaluate((element, args) => {
    const dataTransfer = new DataTransfer()
    if (args.filename) dataTransfer.items.add(new File(['drop fixture'], args.filename, { type: 'text/plain' }))
    else dataTransfer.setData(args.contentType, 'https://example.com/')
    const event = new DragEvent(args.type, { dataTransfer, bubbles: true, cancelable: true })
    element.dispatchEvent(event)
    return event.defaultPrevented
  }, { type, filename, contentType })
}

for (const width of [320, 375, 1440]) {
  test(`composer file drop and overlay at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await login(page, alice)
    const prompt = page.locator('.composer .drop-prompt')
    const name = `${marker('drop')}.txt`
    await drag(page, 'body', 'dragenter', name)
    await expect(prompt).toBeVisible()
    await drag(page, '.composer', 'dragenter', name)
    await drag(page, 'body', 'dragleave', name)
    await drag(page, '#composer-body', 'dragenter', name)
    await drag(page, '.composer', 'dragleave', name)
    await expect(prompt).toBeVisible()
    expect(await prompt.evaluate((element) => getComputedStyle(element).pointerEvents)).toBe('none')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    const box = (await prompt.boundingBox())!
    expect(box.x).toBeGreaterThanOrEqual(0)
    expect(box.x + box.width).toBeLessThanOrEqual(width)
    expect(await drag(page, 'body', 'dragover', name)).toBe(true)
    expect(await drag(page, 'body', 'drop', name)).toBe(true)
    await expect(prompt).toHaveCount(0)
    await expect(page.locator('.composer .selected-files li')).toHaveCount(0)
    for (const contentType of ['text/plain', 'text/uri-list']) {
      for (const selector of ['body', '#composer-body']) {
        expect(await drag(page, selector, 'dragover', undefined, contentType)).toBe(false)
        expect(await drag(page, selector, 'drop', undefined, contentType)).toBe(false)
      }
    }
    let creates = 0
    page.on('request', (request) => {
      if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/v1/uploads') creates++
    })
    const targets = ['#composer-body', '.composer-top', '.composer-toolbar', '.composer .selected-files']
    for (const [index, selector] of targets.entries()) {
      const filename = `${index}-${name}`
      expect(await drag(page, selector, 'drop', filename)).toBe(true)
      await expect(page.locator('.composer .selected-files li', { hasText: filename })).toContainText('已完成')
      await expect(page.locator('.composer .selected-files li')).toHaveCount(index + 1)
    }
    expect(creates).toBe(targets.length)
    await expect(prompt).toHaveCount(0)
    await expect(page).toHaveURL(/\/temporary$/)
  })
}

test('degraded storage still prevents file navigation without upload', async ({ page }) => {
  await page.route('**/api/v1/storage/status', (route) => route.fulfill({ json: {
    healthy: false, reason: 'NAS_UNAVAILABLE', lastCheckedAt: null, changedAt: new Date().toISOString(),
  } }))
  let creates = 0
  page.on('request', (request) => {
    if (request.method() === 'POST' && new URL(request.url()).pathname === '/api/v1/uploads') creates++
  })
  await login(page, alice)
  await expect(page.getByRole('button', { name: '附件', exact: true })).toBeDisabled()
  await drag(page, 'body', 'dragenter', 'offline.txt')
  expect(await drag(page, '.composer-top', 'drop', 'offline.txt')).toBe(true)
  await expect(page.locator('.composer [role="alert"]')).toContainText('存储服务暂时不可用')
  expect(await drag(page, 'body', 'drop', 'offline.txt')).toBe(true)
  await expect(page.locator('.composer .drop-prompt')).toHaveCount(0)
  await expect(page.locator('.composer .selected-files li')).toHaveCount(0)
  expect(creates).toBe(0)
  await expect(page).toHaveURL(/\/temporary$/)
})
