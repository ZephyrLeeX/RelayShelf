import { createHmac } from 'node:crypto'
import { expect, test } from '@playwright/test'
import { alice, login, marker, openSessions } from './helpers'

for (const width of [1440, 375, 320]) {
  test(`sessions cards fit and preserve close behavior at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await login(page, alice)
    const panel = await openSessions(page)
    await expect(panel.getByText('当前会话', { exact: true })).toBeVisible()
    await expect(panel.locator('#password-form')).toHaveCount(0)
    await expect(panel.locator('#totp-form')).toBeHidden()
    await panel.getByRole('button', { name: '重命名', exact: true }).click()
    await panel.getByRole('button', { name: '修改密码', exact: true }).click()
    await panel.getByRole('button', { name: '设置两步验证', exact: true }).click()
    expect(await panel.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true)
    for (const button of await panel.locator('button').all()) {
      const box = (await button.boundingBox())!
      expect(box.x).toBeGreaterThanOrEqual(0)
      expect(box.x + box.width).toBeLessThanOrEqual(width)
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
    await page.keyboard.press('Escape')
    await expect(panel).toHaveCount(0)
    if (width >= 1180) {
      await expect(page.locator('.app-sidebar .account-button')).toBeFocused()
      await openSessions(page)
      await page.locator('.backdrop').click({ position: { x: 10, y: 10 } })
      await expect(panel).toHaveCount(0)
    }
    await openSessions(page)
    await panel.getByRole('button', { name: '关闭', exact: true }).click()
    await expect(panel).toHaveCount(0)
  })
}

function code(secret: string) {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  const bits = [...secret.replace(/=+$/, '')].map((char) => alphabet.indexOf(char).toString(2).padStart(5, '0')).join('')
  const key = Buffer.from(bits.match(/.{8}/g)!.map((byte) => parseInt(byte, 2)))
  const counter = Buffer.alloc(8)
  counter.writeBigUInt64BE(BigInt(Math.floor(Date.now() / 30_000)))
  const digest = createHmac('sha1', key).update(counter).digest()
  const offset = digest[digest.length - 1]! & 15
  return ((digest.readUInt32BE(offset) & 0x7fffffff) % 1_000_000).toString().padStart(6, '0')
}

test('device rename, revoke, password and TOTP remain usable through the cards', async ({ page, browser }) => {
  await page.setViewportSize({ width: 375, height: 900 })
  await login(page, alice)
  const other = await browser.newContext()
  const otherPage = await other.newPage()
  await login(otherPage, alice)
  const otherPanel = await openSessions(otherPage)
  const otherName = marker('other-device')
  await otherPanel.getByRole('button', { name: '重命名', exact: true }).click()
  await otherPanel.getByLabel('当前设备名').fill(otherName)
  await otherPanel.getByRole('button', { name: '保存设备名' }).click()
  await expect(otherPanel.locator('.device-summary')).toContainText(otherName)
  const panel = await openSessions(page)
  const deviceName = marker('device')
  await panel.getByRole('button', { name: '重命名', exact: true }).click()
  await panel.getByLabel('当前设备名').fill(deviceName)
  await panel.getByRole('button', { name: '保存设备名' }).click()
  await expect(page.getByRole('status').filter({ hasText: '设备名已保存' })).toBeVisible()
  await expect(panel.locator('.device-summary')).toContainText(deviceName)
  await panel.locator('.session-card:not(.current)', { hasText: otherName }).getByRole('button', { name: '撤销', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: '会话已撤销' })).toBeVisible()
  await otherPage.reload()
  await expect(otherPage).toHaveURL(/login/)
  await login(otherPage, alice)

  // Reuse the seeded password so the dev account remains available after acceptance.
  await panel.getByRole('button', { name: '修改密码', exact: true }).click()
  await panel.locator('#password-form').getByLabel('当前密码').fill(alice.password)
  await panel.getByLabel('新密码').fill(alice.password)
  await panel.getByRole('button', { name: '修改并撤销其他会话' }).click()
  await expect(page.getByRole('status').filter({ hasText: '密码已修改' })).toBeVisible()
  await otherPage.reload()
  await expect(otherPage).toHaveURL(/login/)
  await other.close()

  await panel.getByRole('button', { name: '设置两步验证' }).click()
  await panel.locator('#totp-form').getByLabel('当前密码').fill(alice.password)
  await panel.getByRole('button', { name: '开始启用', exact: true }).click()
  await expect(panel.getByRole('img', { name: 'TOTP enrollment 二维码' })).toBeVisible()
  const secret = (await panel.locator('.secret').innerText()).trim()
  expect(await panel.evaluate((element) => element.scrollWidth <= element.clientWidth)).toBe(true)
  await panel.getByLabel('验证码').fill(code(secret))
  await panel.getByRole('button', { name: '确认并启用' }).click()
  await expect(panel.locator('.badge.success').filter({ hasText: '已启用' })).toBeVisible()
  try {
    await panel.getByRole('button', { name: '管理两步验证' }).click()
    // A fresh time step avoids replaying the enrollment confirmation code.
    await page.waitForTimeout(30_100 - Date.now() % 30_000)
    await panel.getByLabel('验证码').fill(code(secret))
    await panel.getByRole('button', { name: '关闭两步验证', exact: true }).click()
    await expect(panel.locator('.badge').filter({ hasText: '建议启用' })).toBeVisible()
  } finally {
    if (await panel.locator('.badge.success').filter({ hasText: '已启用' }).count()) {
      await page.waitForTimeout(30_100 - Date.now() % 30_000)
      await panel.getByLabel('验证码').fill(code(secret))
      await panel.getByRole('button', { name: '关闭两步验证', exact: true }).click()
    }
  }
})
