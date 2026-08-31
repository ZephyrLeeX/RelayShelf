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
