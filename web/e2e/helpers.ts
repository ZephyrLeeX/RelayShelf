import { expect, type Page } from '@playwright/test'

export const alice = { username: 'e2e-alice', password: 'e2e-alice-pass-12345' }
export const bob = { username: 'e2e-bob', password: 'e2e-bob-pass-123456' }
export const admin = { username: 'e2e-admin', password: 'e2e-admin-pass-12345' }

export function marker(label: string) {
  return `e2e-${label}-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`
}

export async function login(page: Page, credentials: { username: string; password: string }) {
  await page.goto('/login')
  await page.getByLabel('用户名').fill(credentials.username)
  await page.getByLabel('密码').fill(credentials.password)
  await page.getByRole('button', { name: '登录' }).click()
  await expect(page).toHaveURL(/\/temporary$/)
}

export async function openSessions(page: Page) {
  const width = await page.evaluate(() => window.innerWidth)
  if (width >= 1180) await page.locator('.app-sidebar .account-button').click()
  else {
    await page.getByRole('button', { name: '我的', exact: true }).click()
    await page.getByRole('button', { name: '设备与会话', exact: true }).click()
  }
  return page.getByRole('dialog', { name: /设备与会话/ })
}

export async function logout(page: Page) {
  await page.getByRole('button', { name: /退出/ }).click()
  await expect(page).toHaveURL(/\/login/)
}

export async function composeAndSend(page: Page, body: string, options: { sensitive?: boolean; lifecycle?: 'TEMPORARY' | 'PERMANENT'; contentType?: string } = {}) {
  await page.locator('#composer-body').fill(body)
  if (options.contentType) {
    await page.getByRole('button', { name: '内容类型' }).click()
    await page.getByRole('option', { name: options.contentType, exact: true }).click()
  }
  if (options.sensitive) {
    await page.getByRole('button', { name: '敏感内容', exact: true }).click()
  }
  if (options.lifecycle === 'PERMANENT') await page.getByLabel('保存位置').selectOption('PERMANENT')
  await page.getByRole('button', { name: '发送', exact: true }).click()
}

export async function selectComposerFiles(page: Page, paths: string | string[]) {
  const chooser = page.waitForEvent('filechooser')
  await page.getByRole('button', { name: '附件', exact: true }).click()
  await (await chooser).setFiles(paths)
}

/** Fetch the current user's ID from the authenticated bootstrap endpoint. */
export async function currentUserId(page: Page): Promise<string> {
  const response = await page.request.get('/api/v1/auth/session')
  expect(response.ok()).toBeTruthy()
  return (await response.json()).user.id
}
