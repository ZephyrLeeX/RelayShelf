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

export async function logout(page: Page) {
  await page.getByRole('button', { name: /退出/ }).click()
  await expect(page).toHaveURL(/\/login/)
}

export async function composeAndSend(page: Page, body: string, options: { sensitive?: boolean; lifecycle?: 'TEMPORARY' | 'PERMANENT' } = {}) {
  await page.locator('#composer-body').fill(body)
  if (options.sensitive) {
    await page.getByLabel('高级选项').click()
    await page.getByLabel('敏感内容').check()
  }
  if (options.lifecycle === 'PERMANENT') await page.getByLabel('保存位置').selectOption('PERMANENT')
  await page.getByRole('button', { name: '发送', exact: true }).click()
}

export async function selectComposerFiles(page: Page, paths: string | string[]) {
  const chooser = page.waitForEvent('filechooser')
  await page.getByRole('button', { name: '文件', exact: true }).click()
  await (await chooser).setFiles(paths)
}

/** Fetch the current user's ID from the authenticated bootstrap endpoint. */
export async function currentUserId(page: Page): Promise<string> {
  const response = await page.request.get('/api/v1/auth/session')
  expect(response.ok()).toBeTruthy()
  return (await response.json()).user.id
}
