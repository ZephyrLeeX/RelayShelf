import { expect, test } from '@playwright/test'
import { alice, login, marker } from './helpers'
import { readFile } from 'node:fs/promises'

for (const viewport of [{ width: 1440, height: 900 }, { width: 390, height: 844 }]) {
  test.describe(`message ZIP download at ${viewport.width}px`, () => {
    test.use({ viewport })
    test('downloads current message attachments from detail', async ({ page }) => {
      await login(page, alice)
      const title = marker('attachment-zip')
      const composer = page.locator('.composer')
      await composer.getByLabel('标题（可选）').fill(title)
      await composer.locator('input[type=file]').setInputFiles([
        { name: 'first.txt', mimeType: 'text/plain', buffer: Buffer.from('first attachment') },
        { name: 'second.txt', mimeType: 'text/plain', buffer: Buffer.from('second attachment') },
      ])
      await expect(composer.getByRole('button', { name: '发送', exact: true })).toBeEnabled()
      await composer.getByRole('button', { name: '发送', exact: true }).click()
      const card = page.locator('.message-card', { hasText: title })
      await card.getByRole('button', { name: /打开内容/ }).click()
      const detail = page.locator('.message-inspector')
      const link = detail.getByRole('link', { name: '下载全部附件（ZIP）', exact: true })
      await expect(link).toBeVisible()
      const downloadPromise = page.waitForEvent('download')
      await link.click()
      const download = await downloadPromise
      const response = await page.request.get((await link.getAttribute('href'))!)
      expect(response.status()).toBe(200)
      expect(response.headers()['content-type']).toBe('application/zip')
      expect(await download.failure()).toBeNull()
      expect(download.suggestedFilename()).toMatch(/^attachments-.*\.zip$/)
      const file = await download.path()
      expect(file).toBeTruthy()
      const bytes = await readFile(file!)
      expect(bytes.subarray(0, 4).toString('hex')).toBe('504b0304')
      for (const content of ['first.txt', 'second.txt', 'first attachment', 'second attachment']) {
        expect(bytes.includes(Buffer.from(content))).toBeTruthy()
      }
      await expect(detail.getByRole('link', { name: '下载 first.txt', exact: true })).toBeVisible()
      await expect(detail).not.toContainText('下载完成')
    })
  })
}
