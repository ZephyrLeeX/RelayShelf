import { expect, test } from '@playwright/test'
import { alice, bob, login, marker } from './helpers'

// Run against a built PWA preview with the isolated Dev API; Vite HMR has no SW.
test('POST share survives login, stays unpublished until confirmation, and isolates users', async ({ page, context }) => {
  await login(page, alice)
  await page.evaluate(async () => { await navigator.serviceWorker.ready })
  await page.reload()
  await expect.poll(() => page.evaluate(() => Boolean(navigator.serviceWorker.controller))).toBe(true)
  // End the API session without reloading the installed shell/worker.
  await page.evaluate(async () => {
    const response = await fetch('/api/v1/auth/session')
    const session = await response.json()
    await fetch('/api/v1/auth/logout', { method: 'POST', headers: { 'X-CSRF-Token': session.csrfToken } })
  })
  const text = marker('share')
  let sends = 0
  page.on('request', (request) => {
    if (request.method() === 'POST' && request.url().endsWith('/api/v1/messages')) sends++
    expect(request.url()).not.toContain(text)
  })
  await page.evaluate((body) => {
    const form = document.createElement('form')
    form.method = 'POST'; form.action = '/share'; form.enctype = 'multipart/form-data'
    for (const [name, value] of Object.entries({ title: '系统分享', text: body + ' https://example.com/a', url: 'https://example.com/a' })) {
      const input = document.createElement('input'); input.name = name; input.value = value; form.append(input)
    }
    document.body.append(form); form.submit()
  }, text)
  await expect(page).toHaveURL((url) => url.pathname === '/login' && url.searchParams.get('redirect') === '/share')
  await expect(page.getByText('分享内容暂存在本页内存中', { exact: false })).toBeVisible()
  await page.getByLabel('用户名').fill(alice.username)
  await page.getByLabel('密码').fill(alice.password)
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL(/\/share$/)
  expect(sends).toBe(0)
  await page.getByRole('button', { name: '带入编辑器', exact: true }).click()
  await expect(page.locator('#composer-body')).toHaveValue(text + ' https://example.com/a')
  expect(sends).toBe(0)
  await page.getByRole('button', { name: '发送', exact: true }).click()
  await expect(page.locator('#composer-body')).toHaveValue('')
  expect(sends).toBe(1)
  const other = await context.browser()!.newContext()
  const otherPage = await other.newPage()
  await login(otherPage, bob)
  await expect(otherPage.getByText(text, { exact: false })).toHaveCount(0)
  await other.close()
})
