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
  // The server serves the UI built into bin/clavis (`pnpm e2e` runs make
  // build first), so no Vite server is needed.
  webServer: [
    {
      command: 'node e2e/start-server.mjs',
      url: 'http://127.0.0.1:18110/healthz',
      reuseExistingServer: false,
      timeout: 90_000,
    },
  ],
})
