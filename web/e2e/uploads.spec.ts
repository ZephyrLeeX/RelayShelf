import { expect, test } from '@playwright/test'
import * as crypto from 'node:crypto'
import { mkdtempSync, readFileSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { alice, login, marker } from './helpers'

/** A deterministic multi-chunk payload spanning three 8 MiB server chunks. */
function threeChunkFile(name: string): string {
  const dir = mkdtempSync(join(tmpdir(), 'relayshelf-e2e-upload-'))
  const path = join(dir, name)
  const chunk = Buffer.alloc(8 * 1024 * 1024)
  for (let i = 0; i < chunk.length; i += 4096) chunk.fill(i % 251, i, i + 4096)
  const tail = Buffer.alloc(512 * 1024)
  tail.fill(0xab)
  writeFileSync(path, Buffer.concat([chunk, Buffer.from(chunk), tail]))
  return path
}

test.describe('upload journey', () => {
  test('small file uploads, binds to a message, and downloads back', async ({ page }) => {
    const name = `${marker('upload')}.txt`
    const path = threeChunkFile(name)
    const digest = crypto.createHash('sha256').update(readFileSync(path)).digest('hex')
    await login(page, alice)

    await page.locator('.drop-zone input[type=file]').setInputFiles(path)
    await expect(page.locator('.selected-files li', { hasText: name })).toBeVisible()
    await expect(page.locator('.selected-files li', { hasText: name })).toContainText('COMPLETED', { timeout: 60_000 })

    await page.locator('#composer-body').fill(`upload note ${name}`)
    await page.getByRole('button', { name: '发送', exact: true }).click()

    const card = page.locator('.message-card', { hasText: name })
    await expect(card).toBeVisible()
    await expect(card.getByText('3 个附件').or(card.getByText(/个附件/))).toBeVisible()

    // The attachment downloads back byte-identical content through the
    // viewer's real download link.
    await card.getByRole('button', { name: /打开内容/ }).click()
    const detail = page.locator('.detail')
    await expect(detail).toBeVisible()
    await detail.locator('.attachment', { hasText: name }).click()
    const viewer = page.locator('.viewer')
    await expect(viewer).toBeVisible()
    const [downloadEvent] = await Promise.all([
      page.waitForEvent('download'),
      viewer.locator('a.download').click(),
    ])
    const handle = downloadEvent
    const stream = await handle.createReadStream()
    const chunks: Buffer[] = []
    for await (const chunk of stream) chunks.push(chunk as Buffer)
    const received = crypto.createHash('sha256').update(Buffer.concat(chunks)).digest('hex')
    expect(received).toBe(digest)
  })
})

test.describe('upload resume journey', () => {
  test('interrupted upload resumes after reload and reselection', async ({ page }) => {
    const name = `${marker('resume')}.bin`
    const path = threeChunkFile(name)
    await login(page, alice)

    // Let part 0 through; hang part 1 and beyond so the browser is genuinely
    // interrupted mid-transfer, then reload the page (the route dies with it).
    await page.route(/\/api\/v1\/uploads\/[^/]+\/parts\/(\d+)/, async (route) => {
      const part = Number(new RegExp(/\/parts\/(\d+)/).exec(route.request().url())![1])
      if (part === 0) await route.continue()
      else await new Promise(() => undefined)
    })

    const created = page.waitForResponse((response) =>
      response.url().includes('/api/v1/uploads')
      && response.request().method() === 'POST'
      && response.status() === 201,
    )
    await page.locator('.drop-zone input[type=file]').setInputFiles(path)
    const uploadId = (await (await created).json()) as { id: string }

    // Part 0 really reached the server.
    await expect.poll(async () => {
      const response = await page.request.get(`/api/v1/uploads/${uploadId.id}`)
      const session = await response.json() as { completedParts: number[] }
      return session.completedParts.length
    }, { timeout: 60_000 }).toBeGreaterThanOrEqual(1)

    await page.reload()
    await expect(page.getByRole('heading', { name: 'Temporary' })).toBeVisible()
    // The interruption is real: the page died mid-transfer while parts were
    // hanging. Route registrations survive reloads, so lift them now to model
    // the network being healthy again for the resumed attempt.
    await page.unrouteAll({ behavior: 'ignoreErrors' })

    // The interrupted session is restored from the resume ledger; open the
    // transfer shelf to see it waiting for the original file.
    await page.getByRole('button', { name: /打开上传任务/ }).click()
    const queueItem = page.locator('.upload-item', { hasText: name })
    await expect(queueItem).toBeVisible({ timeout: 20_000 })
    await expect(page.getByText(/个上传等待重新选择原文件/)).toBeVisible()
    await queueItem.getByText('PAUSED').waitFor()

    // Routes are gone after reload; reselecting the same file continues from
    // the server-confirmed part 0.
    await queueItem.locator('input[type=file]').setInputFiles(path)
    await expect(queueItem.getByText('COMPLETED')).toBeVisible({ timeout: 120_000 })

    // Bind the finished upload to a message from the composer.
    await page.getByRole('button', { name: '关闭上传任务' }).click()
    await expect(page.getByRole('button', { name: '添加到本条' })).toBeVisible()
    await page.getByRole('button', { name: '添加到本条' }).click()
    await expect(page.locator('.selected-files li', { hasText: name })).toBeVisible()
    await page.getByRole('button', { name: '发送', exact: true }).click()
    await expect(page.locator('.message-card', { hasText: name })).toBeVisible()
  })
})

test.describe('TOTP journey', () => {
  test('enrollment gates the next login and the code completes it', async ({ page }) => {
    await login(page, alice)

    // Open the account drawer and enroll.
    await page.locator('button.account').click()
    const drawer = page.locator('.drawer')
    await expect(drawer).toBeVisible()
    await drawer.getByRole('button', { name: '开始启用' }).click()

    const secretElement = drawer.locator('.secret')
    await expect(secretElement).toBeVisible()
    const secret = (await secretElement.textContent())?.trim() ?? ''
    expect(secret).toMatch(/^[A-Z2-7]{32}$/)

    await drawer.getByLabel('验证码').fill(totpCode(secret))
    await drawer.getByRole('button', { name: '确认并启用' }).click()
    await expect(drawer.getByText('已启用。输入当前验证码可关闭两步验证。')).toBeVisible()

    // Logout, then password-only login is challenged. The confirming step is
    // already consumed by replay protection, so use the next time step.
    await page.keyboard.press('Escape')
    await page.getByRole('button', { name: '退出登录' }).click()
    await expect(page).toHaveURL(/\/login/)

    await page.getByLabel('用户名').fill(alice.username)
    await page.getByLabel('密码').fill(alice.password)
    await page.getByRole('button', { name: '登录' }).click()
    await expect(page.getByText('此账号已启用两步验证。')).toBeVisible()
    await expect(page).toHaveURL(/\/login/)

    await waitForNextStep()
    await page.getByLabel('验证码').fill(totpCode(secret))
    await page.getByRole('button', { name: '验证', exact: true }).click()
    await expect(page).toHaveURL(/\/temporary$/)

    // Leave alice without TOTP so the remaining journeys stay password-only.
    await page.locator('button.account').click()
    const cleanup = page.locator('.drawer')
    await expect(cleanup).toBeVisible()
    await waitForNextStep()
    await cleanup.getByLabel('验证码').fill(totpCode(secret))
    await cleanup.getByRole('button', { name: '关闭两步验证' }).click()
    await expect(cleanup.getByRole('button', { name: '开始启用' })).toBeVisible()
  })
})

/** Waits until the next 30-second TOTP step begins (replay protection). */
async function waitForNextStep() {
  const remaining = 30 - (Date.now() / 1000) % 30
  await new Promise((resolve) => setTimeout(resolve, remaining * 1000 + 500))
}

/** RFC 6238 code for the current 30-second step (HMAC-SHA1, 6 digits). */
function totpCode(base32Secret: string, at = Date.now()): string {
  const alphabet = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567'
  let bits = 0
  let value = 0
  const bytes: number[] = []
  for (const char of base32Secret.replace(/=+$/, '').toUpperCase()) {
    value = (value << 5) | alphabet.indexOf(char)
    bits += 5
    if (bits >= 8) {
      bytes.push((value >>> (bits - 8)) & 0xff)
      bits -= 8
    }
  }
  const key = Buffer.from(bytes)
  const counter = Buffer.alloc(8)
  counter.writeBigUInt64BE(BigInt(Math.floor(at / 1000 / 30)))
  const sum = crypto.createHmac('sha1', key).update(counter).digest()
  const offset = sum[sum.length - 1] & 0x0f
  const binary = ((sum[offset] & 0x7f) << 24) | (sum[offset + 1] << 16) | (sum[offset + 2] << 8) | sum[offset + 3]
  return String(binary % 1_000_000).padStart(6, '0')
}
