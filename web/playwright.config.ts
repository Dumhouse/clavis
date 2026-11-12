import { defineConfig } from '@playwright/test'

const channel = process.env.PLAYWRIGHT_CHANNEL

export default defineConfig({
  testDir: './e2e',
  timeout: 60_000,
  expect: { timeout: 10_000 },
  fullyParallel: false,
  workers: 1,
  reporter: process.env.CI ? 'github' : 'list',
  use: {
    baseURL: 'http://127.0.0.1:18110',
    trace: 'retain-on-failure',
    ...(channel ? { channel } : {}),
  },
  webServer: [
    {
      command: 'pnpm dev --host 127.0.0.1 --port 5173',
      url: 'http://127.0.0.1:5173',
      reuseExistingServer: !process.env.CI,
      timeout: 60_000,
    },
    {
      command: 'node e2e/start-server.mjs',
      url: 'http://127.0.0.1:18110/healthz',
      reuseExistingServer: false,
      timeout: 90_000,
    },
  ],
})
