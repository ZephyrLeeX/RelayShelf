import { expect, test, type Locator, type Page } from '@playwright/test'
import { alice, login, marker } from './helpers'

async function percentage(dialog: Locator) {
  return Number.parseFloat(await dialog.getByLabel('缩放比例').innerText())
}
async function assertFit(dialog: Locator) {
  const geometry = await dialog.locator('img').evaluate(image => {
    const img = image as HTMLImageElement
    const box = img.getBoundingClientRect()
    const stage = img.parentElement!.getBoundingClientRect()
    return { actual: box.width / img.naturalWidth * 100, expected: Math.min(1, stage.width / img.naturalWidth, stage.height / img.naturalHeight) * 100, inside: box.left >= stage.left - 1 && box.top >= stage.top - 1 && box.right <= stage.right + 1 && box.bottom <= stage.bottom + 1 }
  })
  expect(geometry.inside).toBe(true)
  expect(geometry.actual).toBeCloseTo(geometry.expected, 1)
  expect(Math.abs(await percentage(dialog) - geometry.actual)).toBeLessThanOrEqual(.051)
}
async function touch(page: Page, stage: Locator, kind: 'swipe' | 'pinch' | 'pan') {
  const box = (await stage.boundingBox())!
  const x = box.x + box.width / 2, y = box.y + box.height / 2
  const session = await page.context().newCDPSession(page)
  const points = (progress: number) => kind === 'pinch'
    ? [{ x: x - 30 - progress * 50, y, id: 1 }, { x: x + 30 + progress * 50, y, id: 2 }]
    : [{ x: x + 70 - progress * 140, y, id: 1 }]
  await session.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: points(0) })
  for (let i = 1; i <= 6; i++) await session.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: points(i / 6) })
  await session.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
  await session.detach()
}

for (const mobile of [false, true]) {
  test.describe(mobile ? 'mobile image viewer' : 'desktop image viewer', () => {
    test.use({ viewport: mobile ? { width: 390, height: 844 } : { width: 1440, height: 900 }, isMobile: mobile, hasTouch: mobile })
    test('unifies card, detail and Markdown previews; zooms, navigates, retries and restores', async ({ page }) => {
      await login(page, alice)
      const title = marker('image-preview')
      const png = Buffer.from(await page.evaluate(() => {
        const canvas = document.createElement('canvas'); canvas.width = 2000; canvas.height = 1400
        const ctx = canvas.getContext('2d')!; ctx.fillStyle = '#5b56d6'; ctx.fillRect(0, 0, 2000, 1400)
        ctx.fillStyle = 'white'; ctx.font = '100px sans-serif'; ctx.fillText('RelayShelf image preview', 80, 200)
        return canvas.toDataURL('image/png').split(',')[1]
      }), 'base64')
      let remoteRequests = 0, originals = 0
      let failRemote = true
      let releaseRemote!: () => void
      const remoteGate = new Promise<void>(resolve => { releaseRemote = resolve })
      await page.route('https://image.test/preview.png', async route => {
        remoteRequests++
        if (failRemote) await remoteGate
        return failRemote ? route.fulfill({ status: 503, body: 'unavailable' }) : route.fulfill({ contentType: 'image/png', body: png })
      })
      page.on('request', request => { if (/\/attachments\/[^/]+\/preview$/.test(new URL(request.url()).pathname)) originals++ })
      const composer = page.locator('.composer')
      await composer.getByLabel('标题（可选）').fill(title)
      await composer.getByRole('button', { name: '内容类型', exact: true }).click()
      await composer.getByRole('option', { name: 'Markdown', exact: true }).click()
      await composer.locator('textarea').fill('![远程图](https://image.test/preview.png)')
      await composer.locator('input[type=file]').setInputFiles(['first.png', 'second.png'].map(name => ({ name, mimeType: 'image/png', buffer: png })))
      await expect(composer.getByRole('button', { name: '发送', exact: true })).toBeEnabled()
      await composer.getByRole('button', { name: '发送', exact: true }).click()
      const card = page.locator('.message-card', { hasText: title })
      const opener = card.getByRole('button', { name: '预览 first.png', exact: true })
      await expect(opener).toBeVisible()
      expect(remoteRequests).toBe(0); expect(originals).toBe(0)
      await opener.focus()
      // Playwright may scroll the trigger into view before clicking. Capture at the actual opening event.
      await opener.evaluate(element => element.addEventListener('click', () => {
        document.body.dataset.previewScroll = JSON.stringify({ x: window.scrollX, y: window.scrollY, overflow: document.body.style.overflow })
      }, { once: true, capture: true }))
      await opener.click()
      const beforeScroll = await page.evaluate(() => JSON.parse(document.body.dataset.previewScroll!))
      const dialog = page.getByRole('dialog', { name: '图片预览' })
      const stage = dialog.locator('.image-stage')
      await expect(dialog.locator('img')).toBeVisible()
      await expect(dialog.locator('header')).toContainText('2 / 3')
      await assertFit(dialog)
      await expect(dialog.getByRole('link', { name: '下载原图' })).toBeVisible()
      expect(await page.evaluate(() => document.body.style.position)).toBe('fixed')
      if (mobile) {
        const fit = await percentage(dialog)
        await touch(page, stage, 'pinch')
        expect(await percentage(dialog)).toBeGreaterThan(fit)
        const before = await dialog.locator('img').getAttribute('style')
        await touch(page, stage, 'pan')
        expect(await dialog.locator('img').getAttribute('style')).not.toBe(before)
        await expect(dialog.locator('header')).toContainText('2 / 3')
        await dialog.getByRole('button', { name: '适应窗口', exact: true }).click()
        await touch(page, stage, 'swipe')
        await expect(dialog.locator('header')).toContainText('3 / 3')
        await expect(dialog.locator('img')).toBeVisible()
        await page.setViewportSize({ width: 844, height: 390 })
        await assertFit(dialog)
        await expect(dialog.getByRole('link', { name: '下载原图' })).toBeInViewport()
        await page.setViewportSize({ width: 390, height: 844 })
      } else {
        await stage.dblclick()
        await expect(dialog.getByLabel('缩放比例')).toHaveText('100%')
        const box = (await stage.boundingBox())!
        await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
        await page.mouse.down(); await page.mouse.move(box.x + box.width / 2 + 90, box.y + box.height / 2 + 50); await page.mouse.up()
        await expect(dialog.locator('img')).toHaveAttribute('style', /translate\(90px, 50px\)/)
        await page.mouse.wheel(0, -100)
        await expect.poll(() => percentage(dialog)).toBeGreaterThan(100)
        await stage.dblclick(); await assertFit(dialog)
        await dialog.getByRole('button', { name: '放大', exact: true }).click()
        await dialog.getByRole('button', { name: '原始尺寸', exact: true }).click()
        await expect(dialog.getByLabel('缩放比例')).toHaveText('100%')
        await page.keyboard.press('ArrowRight')
        await expect(dialog.locator('header')).toContainText('3 / 3')
        await expect(dialog.locator('img')).toBeVisible(); await assertFit(dialog)
        await page.keyboard.press('ArrowLeft')
        await expect(dialog.locator('header')).toContainText('2 / 3')
        await dialog.getByRole('button', { name: '关闭图片预览' }).focus()
        await page.keyboard.press('Shift+Tab')
        await expect(dialog.getByRole('button', { name: '下一张' })).toBeFocused()
      }
      await dialog.getByRole('button', { name: '适应窗口', exact: true }).click()
      await assertFit(dialog)
      // The image ignores pointer events, so even its center click targets the stage.
      const imageBox = (await dialog.locator('img').boundingBox())!
      if (mobile) await page.touchscreen.tap(imageBox.x + imageBox.width / 2, imageBox.y + imageBox.height / 2)
      else await page.mouse.click(imageBox.x + imageBox.width / 2, imageBox.y + imageBox.height / 2)
      await expect(dialog).toBeVisible()
      const stageBox = (await stage.boundingBox())!
      const blank = imageBox.x - stageBox.x > 2
        ? { x: (stageBox.x + imageBox.x) / 2, y: stageBox.y + stageBox.height / 2 }
        : { x: stageBox.x + stageBox.width / 2, y: (stageBox.y + imageBox.y) / 2 }
      expect(await stage.evaluate((element, point) => document.elementFromPoint(point.x, point.y) === element, blank)).toBe(true)
      if (mobile) await page.touchscreen.tap(blank.x, blank.y)
      else await page.mouse.click(blank.x, blank.y)
      await expect(dialog).toHaveCount(0)
      await expect(opener).toBeFocused()
      expect(await page.evaluate(() => ({ x: window.scrollX, y: window.scrollY, overflow: document.body.style.overflow }))).toEqual(beforeScroll)
      await card.locator('a.markdown-image').click()
      await expect(dialog.getByRole('status').filter({ hasText: '正在加载图片' })).toContainText('正在加载图片')
      await expect(dialog).not.toContainText('此文件仅支持下载')
      releaseRemote()
      await expect(dialog.getByRole('alert')).toContainText('图片加载失败')
      await expect(dialog).not.toContainText('此文件仅支持下载')
      failRemote = false
      await dialog.getByRole('button', { name: '重试' }).click()
      await expect(dialog.locator('img')).toBeVisible(); await assertFit(dialog)
      expect(remoteRequests).toBe(2)
      await dialog.getByRole('button', { name: '关闭图片预览' }).click()
      await card.getByRole('button', { name: /打开内容/ }).click()
      const detail = page.locator('.message-inspector')
      await detail.locator('button.attachment', { hasText: 'first.png' }).click()
      await expect(dialog.locator('header')).toContainText('2 / 3')
      await expect(dialog.locator('img')).toBeVisible()
      const downloaded = page.waitForEvent('download')
      await dialog.getByRole('link', { name: '下载原图' }).click()
      expect((await downloaded).suggestedFilename()).toBe('first.png')
      await page.keyboard.press('Escape')
      await expect(detail).toBeVisible()
    })
  })
}
