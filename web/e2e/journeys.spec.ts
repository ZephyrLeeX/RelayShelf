import { expect, test, type Page } from '@playwright/test'
import { alice, bob, composeAndSend, currentUserId, login, logout, marker } from './helpers'

test.describe('login journey', () => {
  test('login, survive reload, and logout', async ({ page }) => {
    await login(page, alice)
    await expect(page.getByRole('heading', { name: 'Temporary' })).toBeVisible()

    // The session survives a full reload.
    await page.reload()
    await expect(page.getByRole('heading', { name: 'Temporary' })).toBeVisible()
    await expect(page.getByRole('button', { name: '退出登录' })).toBeVisible()

    await logout(page)
    await page.goto('/temporary')
    await expect(page).toHaveURL(/\/login/)
  })

  test('wrong password is rejected without logging in', async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel('用户名').fill(alice.username)
    await page.getByLabel('密码').fill('not-the-right-password')
    await page.getByRole('button', { name: '登录' }).click()
    await expect(page.getByText('用户名或密码错误。')).toBeVisible()
    await expect(page).toHaveURL(/\/login/)
  })
})

test.describe('send text journey', () => {
  test('created text is visible, survives reload, and opens with correct detail', async ({ page }) => {
    const body = marker('send-text')
    await login(page, alice)
    await composeAndSend(page, body)

    const card = page.locator('.message-card', { hasText: body })
    await expect(card).toBeVisible()

    await page.reload()
    await expect(page.locator('.message-card', { hasText: body })).toBeVisible()

    await card.getByRole('button', { name: new RegExp(`打开内容`) }).click()
    const detail = page.locator('.detail')
    await expect(detail).toBeVisible()
    await expect(detail.locator('pre')).toContainText(body)
  })
})

test.describe('second browser SSE journey', () => {
  test('browser B receives the mutation without reloading', async ({ browser }) => {
    const body = marker('sse')
    const contextA = await browser.newContext()
    const contextB = await browser.newContext()
    const pageA = await contextA.newPage()
    const pageB = await contextB.newPage()
    try {
      await login(pageA, alice)
      // Register the stream waiter before B logs in so the moment the
      // EventSource response header flushes is observable, then wait for it:
      // an event published before B's stream opens would be missed by design.
      const bStreamOpen = pageB.waitForResponse((response) =>
        response.url().includes('/api/v1/events') && response.status() === 200,
      )
      await login(pageB, alice)
      await expect(pageB.getByRole('heading', { name: 'Temporary' })).toBeVisible()
      await bStreamOpen

      await composeAndSend(pageA, body)

      // Browser B must show the new card without any navigation.
      await expect(pageB.locator('.message-card', { hasText: body })).toBeVisible({ timeout: 20_000 })
      const navigationCount = await pageB.evaluate(() => performance.getEntriesByType('navigation').length)
      expect(navigationCount).toBe(1)
    } finally {
      await contextA.close()
      await contextB.close()
    }
  })
})

test.describe('temporary to permanent journey', () => {
  test('lifecycle change moves the card and unlocks favorites', async ({ page }) => {
    const body = marker('permanent')
    await login(page, alice)
    await composeAndSend(page, body)

    const card = page.locator('.message-card', { hasText: body })
    await expect(card).toBeVisible()
    await expect(card.getByText(/今天过期|明天过期|剩余/)).toBeVisible()

    await card.getByRole('button', { name: '转为长期' }).click()

    // The card left the temporary feed and now lives in the permanent feed.
    await expect(page.locator('.message-card', { hasText: body })).toHaveCount(0)
    await page.goto('/permanent')
    await expect(page.locator('.message-card', { hasText: body })).toBeVisible()

    // Favorites only work for permanent content.
    await page.goto('/permanent')
    const permanentCard = page.locator('.message-card', { hasText: body })
    await permanentCard.getByRole('button', { name: '收藏' }).click()
    await page.goto('/favorites')
    await expect(page.locator('.message-card', { hasText: body })).toBeVisible()
  })
})

test.describe('search journey', () => {
  test('search finds own content and respects the owner boundary', async ({ page }) => {
    const needle = marker('search-needle')
    await login(page, alice)
    await composeAndSend(page, `prefix ${needle} suffix`)

    await page.getByLabel('搜索内容').fill(needle)
    await page.getByRole('button', { name: '搜索', exact: true }).click()
    await expect(page.locator('.message-card', { hasText: needle })).toBeVisible()
    await expect(page.locator('.message-card', { hasText: needle }).first()).toBeVisible()

    // A different user must not find alice's content.
    const bobContext = await page.context().browser()!.newContext()
    const bobPage = await bobContext.newPage()
    try {
      await login(bobPage, bob)
      await bobPage.getByLabel('搜索内容').fill(needle)
      await bobPage.getByRole('button', { name: '搜索', exact: true }).click()
      await expect(bobPage.locator('.message-card', { hasText: needle })).toHaveCount(0)
      await expect(bobPage.getByText('暂无', { exact: false })).toBeVisible()
    } finally {
      await bobContext.close()
    }
  })
})

test.describe('direct send and forward journey', () => {
  test('direct send delivers an independent copy to the receiver only', async ({ browser }) => {
    const body = marker('direct')
    const aliceContext = await browser.newContext()
    const bobContext = await browser.newContext()
    const alicePage = await aliceContext.newPage()
    const bobPage = await bobContext.newPage()
    try {
      await login(alicePage, alice)
      await login(bobPage, bob)
      const bobId = await currentUserId(bobPage)

      await alicePage.locator('#composer-body').fill(body)
      await alicePage.getByPlaceholder('接收者用户 ID（UUID），留空则保存到自己的内容流').fill(bobId)
      await expect(alicePage.getByText('直发内容会作为独立临时副本发送给接收者')).toBeVisible()
      await alicePage.getByRole('button', { name: '直接发送' }).click()
      await expect(alicePage.locator('#composer-body')).toHaveValue('')

      // Receiver sees the copy without reloading.
      await expect(bobPage.locator('.message-card', { hasText: body })).toBeVisible({ timeout: 20_000 })
      // Sender keeps no copy.
      await alicePage.reload()
      await expect(alicePage.locator('.message-card', { hasText: body })).toHaveCount(0)
    } finally {
      await aliceContext.close()
      await bobContext.close()
    }
  })

  test('forward creates an independent receiver copy and keeps the original', async ({ browser }) => {
    const body = marker('forward')
    const aliceContext = await browser.newContext()
    const bobContext = await browser.newContext()
    const alicePage = await aliceContext.newPage()
    const bobPage = await bobContext.newPage()
    try {
      await login(alicePage, alice)
      await login(bobPage, bob)
      const bobId = await currentUserId(bobPage)

      await composeAndSend(alicePage, body)
      const card = alicePage.locator('.message-card', { hasText: body })
      await card.getByRole('button', { name: /打开内容/ }).click()
      const detail = alicePage.locator('.detail')
      await detail.getByPlaceholder('接收者用户 ID（UUID）').fill(bobId)
      await detail.getByRole('button', { name: '转发副本' }).click()
      await expect(detail.getByText('已转发；接收者会看到一条独立副本。')).toBeVisible()

      await expect(bobPage.locator('.message-card', { hasText: body })).toBeVisible({ timeout: 20_000 })
      // Sender keeps the original: close the detail route and return to the feed.
      await alicePage.getByRole('button', { name: '关闭详情' }).click()
      await expect(alicePage).toHaveURL(/\/temporary$/)
      await expect(alicePage.locator('.message-card', { hasText: body }).first()).toBeVisible()
    } finally {
      await aliceContext.close()
      await bobContext.close()
    }
  })
})

test.describe('trash and restore journey', () => {
  test('trash moves the card and restore brings it back', async ({ page }) => {
    const body = marker('trash')
    await login(page, alice)
    await composeAndSend(page, body)

    const card = page.locator('.message-card', { hasText: body })
    await card.getByRole('button', { name: '删除', exact: true }).click()
    await expect(page.locator('.message-card', { hasText: body })).toHaveCount(0)

    await page.goto('/trash')
    const trashed = page.locator('.message-card', { hasText: body })
    await expect(trashed).toBeVisible()
    await trashed.getByRole('button', { name: '恢复' }).click()
    await expect(page.locator('.message-card', { hasText: body })).toHaveCount(0)

    await page.goto('/temporary')
    await expect(page.locator('.message-card', { hasText: body })).toBeVisible()
  })
})

test.describe('sensitive reveal and copy journey', () => {
  test('plaintext stays hidden until explicitly revealed', async ({ page }) => {
    const secret = marker('sensitive')
    await login(page, alice)
    await composeAndSend(page, secret, { sensitive: true })

    const card = page.locator('.message-card', { hasText: '🔒 Sensitive' }).first()
    await expect(card).toBeVisible()
    await expect(page.locator('.message-card pre', { hasText: secret })).toHaveCount(0)

    await card.getByRole('button', { name: /打开内容/ }).click()
    const detail = page.locator('.detail')
    await expect(detail).toBeVisible()
    // Not revealed yet.
    await expect(detail.locator('pre')).toHaveCount(0)

    await detail.getByRole('button', { name: '显示正文' }).click()
    await expect(detail.locator('pre')).toContainText(secret)

    // The list preview never contains the plaintext even after reveal.
    await page.goto('/temporary')
    await expect(page.locator('.message-card pre', { hasText: secret })).toHaveCount(0)
  })
})

test.describe('security headers in the real browser', () => {
  test('document responses carry the CSP baseline', async ({ page }) => {
    const response = await page.goto('/login')
    expect(response?.status()).toBe(200)
    const csp = response?.headers()['content-security-policy'] ?? ''
    expect(csp).toContain("default-src 'self'")
    expect(csp).toContain("object-src 'none'")
    expect(csp).not.toContain('unsafe-inline')
    expect(csp).not.toContain('unsafe-eval')
    expect(response?.headers()['x-content-type-options']).toBe('nosniff')
  })

  test('no CSP violations while using the app shell', async ({ page }) => {
    const violations: string[] = []
    page.on('console', (message) => {
      if (message.text().includes('Content Security Policy')) violations.push(message.text())
    })
    await login(page, alice)
    await page.goto('/permanent')
    await page.goto('/search')
    await expect(page.getByRole('heading', { name: '搜索' })).toBeVisible()
    expect(violations).toEqual([])
  })
})
