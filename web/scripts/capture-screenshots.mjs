/**
 * Captures marketing screenshots from a disposable Clavis instance.
 *
 * Usage (from web/): node scripts/capture-screenshots.mjs
 * Requires a current binary + web build: make build
 */
import { spawn } from 'node:child_process'
import { mkdirSync, readFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

import { chromium, request } from '@playwright/test'

const here = dirname(fileURLToPath(import.meta.url))
const webRoot = resolve(here, '..')
const repoRoot = resolve(webRoot, '..')
const marker = join(webRoot, '.e2e-data-dir')
const outDir = join(repoRoot, 'docs', 'assets', 'screenshots')
const port = Number(process.env.CLAVIS_SHOT_PORT ?? 18110)
const base = `http://127.0.0.1:${port}`
const password = 'correct horse battery staple'

mkdirSync(outDir, { recursive: true })

const server = spawn(process.execPath, ['e2e/start-server.mjs'], {
  cwd: webRoot,
  env: { ...process.env, CLAVIS_E2E_PORT: String(port) },
  stdio: ['ignore', 'ignore', 'inherit'],
})

async function waitFor(url, timeout = 60_000) {
  const started = Date.now()
  while (Date.now() - started < timeout) {
    try {
      const res = await fetch(url)
      if (res.ok) return
    } catch {
      // Keep waiting for the server.
    }
    await new Promise((r) => setTimeout(r, 250))
  }
  throw new Error(`timeout waiting for ${url}`)
}

async function seed() {
  const token = readFileSync(join(readFileSync(marker, 'utf8').trim(), 'setup-token'), 'utf8').trim()
  const api = await request.newContext({ baseURL: base })
  await api.post('/api/v1/setup', {
    data: {
      token,
      email: 'demo@example.com',
      name: 'Demo Owner',
      password,
      org_name: 'Acme',
      org_slug: 'acme',
      project_name: 'Payments API',
      project_slug: 'payments-api',
    },
  })
  const state = await api.storageState()
  const csrf = state.cookies.find((c) => c.name === 'clavis_csrf')?.value ?? ''
  const headers = { 'X-CSRF-Token': csrf }

  const schema = [
    {
      key: 'STRIPE_SECRET_KEY',
      type: 'string',
      kind: 'secret',
      required: { production: true, staging: true },
      pattern: '^sk_(test|live)_',
      description: 'Server-side Stripe key used by the payments service.',
      example: 'sk_test_51N…',
    },
    {
      key: 'DATABASE_URL',
      type: 'url',
      kind: 'secret',
      required: { production: true, staging: true },
      pattern: '^postgres://',
      description: 'Primary PostgreSQL connection string.',
      example: 'postgres://app@db.internal:5432/payments',
    },
    {
      key: 'REDIS_URL',
      type: 'url',
      kind: 'secret',
      required: { production: true },
      description: 'Session and rate-limit cache.',
      example: 'redis://cache:6379/0',
    },
    {
      key: 'JWT_SECRET',
      type: 'string',
      kind: 'secret',
      required: { production: true, staging: true },
      pattern: '^[A-Za-z0-9+/=]{32,}$',
      description: 'Signs customer session tokens.',
    },
    {
      key: 'LOG_LEVEL',
      type: 'enum',
      kind: 'variable',
      required: {},
      description: 'Runtime log verbosity.',
      enum: ['debug', 'info', 'warn', 'error'],
    },
    {
      key: 'FEATURE_FLAGS',
      type: 'json',
      kind: 'variable',
      required: {},
      description: 'Checkout and navigation rollout flags.',
      example: '{"checkout":true,"newNav":false}',
    },
    {
      key: 'PORT',
      type: 'int',
      kind: 'variable',
      required: { production: true },
      description: 'HTTP listen port.',
      example: '3000',
    },
  ]
  await api.put('/api/v1/orgs/acme/projects/payments-api/schema', { headers, data: { schema } })

  const secrets = {
    dev: [
      ['STRIPE_SECRET_KEY', 'demo_key_51NdemoDevKey000000000000'],
      ['DATABASE_URL', 'postgres://app:dev@db.dev.internal:5432/payments'],
      ['REDIS_URL', 'redis://cache.dev.internal:6379/0'],
      ['JWT_SECRET', 'devOnlyJwtSecretValue0000000000000000'],
      ['LOG_LEVEL', 'debug'],
      ['FEATURE_FLAGS', '{"checkout":true,"newNav":true}'],
      ['PORT', '3000'],
    ],
    staging: [
      ['STRIPE_SECRET_KEY', 'demo_key_51NdemoStagingKey0000000000'],
      ['DATABASE_URL', 'postgres://app:staging@db.staging.internal:5432/payments'],
      ['JWT_SECRET', 'stagingJwtSecretValue000000000000000'],
      ['LOG_LEVEL', 'info'],
      ['FEATURE_FLAGS', '{"checkout":true,"newNav":false}'],
      ['PORT', '3000'],
    ],
    prod: [
      ['STRIPE_SECRET_KEY', 'demo_key_51NdemoProduction000000000'],
      ['DATABASE_URL', 'postgres://app@db.prod.internal:5432/payments'],
      ['REDIS_URL', 'redis://cache.prod.internal:6379/0'],
      ['JWT_SECRET', 'productionJwtSecretValue000000000000'],
      ['LOG_LEVEL', 'warn'],
      ['FEATURE_FLAGS', '{"checkout":true,"newNav":false}'],
      ['PORT', '3000'],
    ],
  }

  for (const [env, entries] of Object.entries(secrets)) {
    for (const [key, value] of entries) {
      await api.post(`/api/v1/orgs/acme/projects/payments-api/environments/${env}/secrets`, {
        headers,
        data: { key, value, kind: 'secret', message: 'Demo data' },
      })
    }
  }

  const identity = await api.post('/api/v1/orgs/acme/identities', {
    headers,
    data: { name: 'claude-code', type: 'agent', project: 'payments-api', environment: '*' },
  })
  const created = await identity.json()
  if (created?.identity?.id) {
    await api.put(`/api/v1/orgs/acme/identities/${created.identity.id}/commands`, {
      headers,
      data: { commands: ['go test ./...', 'npm test', 'pnpm dev'] },
    })
  }

  await api.dispose()
  return state
}

async function shoot(state) {
  const browser = await chromium.launch()
  const context = await browser.newContext({
    viewport: { width: 1440, height: 900 },
    deviceScaleFactor: 1.5,
    colorScheme: 'dark',
  })
  await context.addCookies(state.cookies)
  await context.addInitScript(() => {
    localStorage.setItem('clavis.theme', 'dark')
    localStorage.setItem('clavis.appearance', 'classic')
    localStorage.setItem('clavis.nav.tools', 'closed')
  })
  const page = await context.newPage()

  async function capture(name, path, { fullPage = false, before } = {}) {
    await page.goto(base + path, { waitUntil: 'networkidle' })
    if (before) await before(page)
    await page.waitForTimeout(600)
    await page.screenshot({
      path: join(outDir, `${name}.jpg`),
      type: 'jpeg',
      quality: 84,
      fullPage,
    })
    console.log('captured', name)
  }

  const promoteSetup = async (p) => {
    const from = p.getByLabel('From environment')
    const to = p.getByLabel('To environment')
    if (await from.count()) {
      await from.selectOption('dev')
      await to.selectOption('staging')
      await p.waitForTimeout(800)
    }
  }
  const shots = [
    ['secrets', '/projects/payments-api'],
    ['schema', '/projects/payments-api/schema'],
    ['promote', '/projects/payments-api/promote', { before: promoteSetup }],
    ['agents', '/identities'],
    ['audit', '/audit'],
    ['docs', '/docs'],
  ]

  // Dark shots keep their historical names (secrets-dark.jpg, schema.jpg, …);
  // light shots get a -light suffix so the docs can swap them by theme.
  for (const [name, path, opts] of shots) {
    await capture(name === 'secrets' ? 'secrets-dark' : name, path, opts)
  }
  await page.addInitScript(() => localStorage.setItem('clavis.theme', 'light'))
  await page.emulateMedia({ colorScheme: 'light' })
  for (const [name, path, opts] of shots) {
    await capture(`${name}-light`, path, opts)
  }

  await browser.close()
}

try {
  await waitFor(`${base}/healthz`)
  const state = await seed()
  await shoot(state)
} finally {
  server.kill('SIGTERM')
}
