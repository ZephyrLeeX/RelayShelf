import { expect, test } from '@playwright/test'
import { alice, composeAndSend, login, marker } from './helpers'

test.describe('desktop UI integration', () => {
  test.use({ viewport: { width: 1440, height: 900 } })

  test('shows full content and edits inline while keeping the composer visible', async ({ page }) => {
    const body = marker('inline-layout') + '\n' + '完整正文 '.repeat(600)
    await login(page, alice)
    await composeAndSend(page, body)
    const initialCard = page.locator('.message-card', { hasText: body.slice(0, 30) })
    await expect(initialCard).toBeVisible()
    const id = await initialCard.getAttribute('data-message-id')
    const card = page.locator(`[data-message-id="${id}"]`)
    await expect(card.locator('.body-section')).toContainText(body)
    await expect(page.locator('.detail-surface, .column-resizer')).toHaveCount(0)
    await expect(page.getByRole('dialog', { name: '内容详情' })).toHaveCount(0)
    await card.locator('.body-section').click()
    await expect(page).not.toHaveURL(/(?:\?|&)detail=/)
    await card.getByRole('button', { name: '编辑', exact: true }).click()
    await expect(card.locator('textarea')).toHaveValue(body)
    await card.locator('textarea').fill(body + ' edited')
    await card.getByRole('button', { name: '保存', exact: true }).click()
    await expect(card.locator('.body-section')).toContainText(body + ' edited')
    await expect(page.locator('.composer-shell')).toBeVisible()
    await page.reload()
    await expect(card.locator('.body-section')).toContainText(body + ' edited')
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

test.describe('desktop topbar constraints', () => {
  test('keeps search centered with one account entry and unclipped theme focus at 1180px and 1440px', async ({ page }) => {
    await login(page, alice)
    for (const width of [1180, 1440]) {
      await page.setViewportSize({ width, height: 900 })
      await expect(page.locator('.app-topbar .account-button')).toHaveCount(0)
      await expect(page.locator('.app-sidebar .account-button')).toHaveCount(1)
      const topbar = (await page.locator('.app-topbar').boundingBox())!
      const search = (await page.locator('.global-search').boundingBox())!
      expect(Math.abs(search.x + search.width / 2 - topbar.x - topbar.width / 2)).toBeLessThanOrEqual(1)
      await page.locator('#global-search').focus()
      for (const button of await page.locator('.topbar-actions button').all()) {
        await page.keyboard.press('Tab')
        await expect(button).toBeFocused()
        const box = (await button.boundingBox())!
        expect(box.x - 5).toBeGreaterThan(search.x + search.width)
        expect(box.x + box.width + 5).toBeLessThanOrEqual(topbar.x + topbar.width)
        expect(await button.evaluate((element) => {
          if (getComputedStyle(element).outlineStyle === 'none') return false
          for (let parent = element.parentElement; parent; parent = parent.parentElement) {
            const style = getComputedStyle(parent)
            if (['hidden', 'clip'].includes(style.overflowX)) {
              const rect = parent.getBoundingClientRect()
              const focus = element.getBoundingClientRect()
              if (focus.left - 5 < rect.left || focus.right + 5 > rect.right) return false
            }
          }
          return true
        })).toBe(true)
      }
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
    }
  })
})

test.describe('wide desktop workspace', () => {
  test.use({ viewport: { width: 2048, height: 1152 } })

  test('bounds the two-column content frame and composer without horizontal overflow', async ({ page }) => {
    await login(page, alice)

    const frame = await page.locator('.content-frame.constrained').boundingBox()
    const composer = await page.locator('.composer-shell').boundingBox()
    await expect(page.locator('.detail-surface')).toHaveCount(0)
    await expect(page.locator('.column-resizer')).toHaveCount(0)
    expect(frame).not.toBeNull()
    expect(composer).not.toBeNull()
    expect(frame!.width).toBeLessThanOrEqual(1041)
    expect(composer!.width).toBeLessThanOrEqual(761)
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

    test('uses flow composer, bottom navigation, quick copy, and inline message actions', async ({ page }) => {
      const body = marker(`mobile-${viewport.width}`)
      await login(page, alice)
      await composeAndSend(page, body)

      await expect(page.locator('.app-sidebar')).toBeHidden()
      await expect(page.locator('.app-topbar')).toBeHidden()
      await expect(page.locator('.mobile-bottom-nav')).toBeVisible()
      await expect(page.locator('.detail-surface')).toHaveCount(0)
      expect(await page.locator('.composer').evaluate((element) => getComputedStyle(element).position)).not.toBe('fixed')
      for (const item of await page.locator('.mobile-bottom-nav a, .mobile-bottom-nav button').all()) {
        expect((await item.boundingBox())!.height).toBeGreaterThanOrEqual(44)
      }

      const card = page.locator('.message-card', { hasText: body })
      await card.getByRole('button', { name: '复制正文' }).click()
      await expect(page).not.toHaveURL(/(?:\?|&)detail=/)
      await card.getByRole('button', { name: '更多操作', exact: true }).click()
      await expect(card.getByRole('button', { name: '设为敏感', exact: true })).toBeVisible()
      await page.keyboard.press('Escape')
      await expect(card.locator('.more-menu')).toHaveCount(0)
      await expect(page.locator('.composer-shell')).toBeVisible()
      await expect(page.getByRole('dialog', { name: '内容详情' })).toHaveCount(0)

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
        const body = marker(`return-${viewport.width}-${index}`)
        await composeAndSend(page, body)
        await expect(page.locator('.message-card', { hasText: body })).toBeVisible()
      }

      const button = page.getByRole('button', { name: '回到发送框' })
      await expect(button).toBeHidden()
      await page.locator('.message-card').last().scrollIntoViewIfNeeded()
      await expect(button).toBeVisible()

      if (viewport.width >= 1180) {
        const expectButtonInContent = async () => {
          const content = await page.locator('main.content').boundingBox()
          const action = await button.boundingBox()
          expect(content).not.toBeNull()
          expect(action).not.toBeNull()
          expect(action!.x).toBeGreaterThanOrEqual(content!.x)
          expect(content!.x + content!.width - (action!.x + action!.width)).toBeCloseTo(20, 0)
        }
        await expectButtonInContent()
      }

      await button.click()
      await expect(page.locator('.composer')).toBeVisible()
      await expect(page.locator('#composer-body')).toBeFocused()
    })
  })
}
