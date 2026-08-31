import { expect, test } from '@playwright/test'
import { alice, composeAndSend, login, marker } from './helpers'

test.describe('desktop UI integration', () => {
  test.use({ viewport: { width: 1440, height: 900 } })

  test('keeps the composer in the content column and detail in the inspector', async ({ page }) => {
    const body = marker('desktop-layout')
    await login(page, alice)
    await composeAndSend(page, body)

    await expect(page.locator('.app-sidebar')).toBeVisible()
    await expect(page.locator('.app-topbar')).toBeVisible()
    await expect(page.locator('.mobile-bottom-nav')).toBeHidden()
    const contentBox = await page.locator('main.content').boundingBox()
    const composerBox = await page.locator('.composer-shell').boundingBox()
    const detailBox = await page.locator('.detail-surface').boundingBox()
    expect(contentBox).not.toBeNull()
    expect(composerBox).not.toBeNull()
    expect(detailBox).not.toBeNull()
    expect(composerBox!.x).toBeGreaterThan(contentBox!.x)
    expect(composerBox!.width).toBeLessThan(contentBox!.width)
    expect(detailBox!.x).toBeGreaterThan(contentBox!.x)

    const card = page.locator('.message-card', { hasText: body })
    await card.getByRole('button', { name: '复制正文' }).click()
    await expect(page).not.toHaveURL(/(?:\?|&)detail=/)
    await card.getByRole('button', { name: /打开内容/ }).click()
    await expect(page).toHaveURL(/(?:\?|&)detail=/)
    await expect(page.getByRole('dialog', { name: '内容详情' })).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page).not.toHaveURL(/(?:\?|&)detail=/)
  })

  test('persists explicit themes and follows system preference changes', async ({ page }) => {
    await page.emulateMedia({ colorScheme: 'light' })
    await login(page, alice)
    await expect(page.locator('html')).toHaveAttribute('data-theme-mode', 'system')
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')

    await page.getByRole('button', { name: '深色', exact: true }).click()
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
    await page.reload()
    await expect(page.locator('html')).toHaveAttribute('data-theme-mode', 'dark')
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')

    await page.getByRole('button', { name: '跟随系统', exact: true }).click()
    await page.emulateMedia({ colorScheme: 'dark' })
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
    await page.emulateMedia({ colorScheme: 'light' })
    await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
  })

  test('sends shell content as fenced markdown, highlights it, and copies bare code', async ({ page }) => {
    await page.context().grantPermissions(['clipboard-read', 'clipboard-write'])
    const body = marker('shell-code')
    await login(page, alice)
    await composeAndSend(page, `docker compose ps # ${body}`, { contentType: 'Shell' })

    const card = page.locator('.message-card', { hasText: body })
    await expect(card.locator('.type-badge')).toHaveText('SH')
    await expect(card.locator('.feed-markdown pre code')).toContainText('docker compose ps')

    await card.getByRole('button', { name: '复制正文' }).click()
    await expect.poll(() => page.evaluate(() => navigator.clipboard.readText())).toBe(`docker compose ps # ${body}`)
  })

  test('linkifies plain-text URLs without opening the detail', async ({ page }) => {
    const body = marker('linkify')
    await login(page, alice)
    await composeAndSend(page, `下载 https://example.com/${body} 完成`)

    const card = page.locator('.message-card', { hasText: body })
    const link = card.locator(`a[href="https://example.com/${body}"]`)
    await expect(link).toBeVisible()
    await expect(link).toHaveAttribute('rel', 'noopener noreferrer')
    await link.evaluate((element) => element.addEventListener('click', (event) => event.preventDefault()))
    await link.click()
    await expect(page).not.toHaveURL(/(?:\?|&)detail=/)
  })
})

test.describe('wide desktop workspace', () => {
  test.use({ viewport: { width: 2048, height: 1152 } })

  test('bounds the content frame, composer, and inspector without horizontal overflow', async ({ page }) => {
    await login(page, alice)

    const frame = await page.locator('.content-frame.constrained').boundingBox()
    const composer = await page.locator('.composer-shell').boundingBox()
    const inspector = await page.locator('.detail-surface').boundingBox()
    expect(frame).not.toBeNull()
    expect(composer).not.toBeNull()
    expect(inspector).not.toBeNull()
    expect(frame!.width).toBeLessThanOrEqual(1041)
    expect(composer!.width).toBeLessThanOrEqual(761)
    expect(inspector!.width).toBeGreaterThanOrEqual(360)
    expect(inspector!.width).toBeLessThanOrEqual(401)
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
  })
})

for (const viewport of [
  { width: 390, height: 844 },
  { width: 393, height: 852 },
  { width: 430, height: 932 },
  { width: 768, height: 1024 },
]) {
  test.describe(`mobile UI integration at ${viewport.width}x${viewport.height}`, () => {
    test.use({ viewport })

    test('uses flow composer, bottom navigation, quick copy, and a detail sheet', async ({ page }) => {
      const body = marker(`mobile-${viewport.width}`)
      await login(page, alice)
      await composeAndSend(page, body)

      await expect(page.locator('.app-sidebar')).toBeHidden()
      await expect(page.locator('.app-topbar')).toBeHidden()
      await expect(page.locator('.mobile-bottom-nav')).toBeVisible()
      await expect(page.locator('.detail-surface')).toBeHidden()
      expect(await page.locator('.composer').evaluate((element) => getComputedStyle(element).position)).not.toBe('fixed')
      for (const item of await page.locator('.mobile-bottom-nav a, .mobile-bottom-nav button').all()) {
        expect((await item.boundingBox())!.height).toBeGreaterThanOrEqual(44)
      }

      const card = page.locator('.message-card', { hasText: body })
      await card.getByRole('button', { name: '复制正文' }).click()
      await expect(page.locator('.detail-surface.selected')).toHaveCount(0)
      await card.getByRole('button', { name: /打开内容/ }).click()
      await expect(page.locator('.detail-surface.selected')).toBeVisible()
      await expect(page.getByRole('dialog', { name: '内容详情' })).toBeVisible()
      await page.keyboard.press('Escape')
      await expect(page.locator('.detail-surface.selected')).toHaveCount(0)

      await page.getByRole('button', { name: '我的', exact: true }).click()
      await expect(page.getByRole('dialog', { name: '我的' })).toBeVisible()
      await expect(page.getByRole('button', { name: '设备与会话' })).toBeVisible()
      await page.keyboard.press('Escape')
      await expect(page.getByRole('dialog', { name: '我的' })).toHaveCount(0)
    })
  })
}

for (const viewport of [
  { width: 1440, height: 900 },
  { width: 390, height: 844 },
]) {
  test.describe(`return to composer at ${viewport.width}x${viewport.height}`, () => {
    test.use({ viewport })

    test('reveals the floating button only after the composer scrolls away', async ({ page }) => {
      await login(page, alice)
      for (let index = 0; index < 8; index += 1) {
        await composeAndSend(page, marker(`return-${viewport.width}-${index}`))
      }

      const button = page.getByRole('button', { name: '回到发送框' })
      await expect(button).toBeHidden()
      await page.locator('.message-card').last().scrollIntoViewIfNeeded()
      await expect(button).toBeVisible()

      await button.click()
      await expect(page.locator('.composer')).toBeVisible()
      await expect(page.locator('#composer-body')).toBeFocused()
    })
  })
}
