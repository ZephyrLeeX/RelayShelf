import { defineConfig, devices } from '@playwright/test'

// Build first and run Vite preview against the isolated relayshelf_dev API.
// This deliberately does not invoke the broad E2E server/seed workflow.
export default defineConfig({
  testDir: './e2e', testMatch: 'share-target.spec.ts',
  timeout: 60_000, expect: { timeout: 15_000 }, workers: 1,
  use: { ...devices['Desktop Chrome'], baseURL: process.env.SHARE_PREVIEW_URL || 'http://127.0.0.1:5175', trace: 'off', screenshot: 'off', video: 'off' },
})
