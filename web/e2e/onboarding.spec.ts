import { expect, test } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'
import { execFile, spawn } from 'node:child_process'
import { mkdtempSync, readFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { promisify } from 'node:util'
import { fileURLToPath } from 'node:url'

const marker = fileURLToPath(new URL('../.e2e-data-dir', import.meta.url))
const binary =
  process.env.CLAVIS_E2E_BINARY ?? fileURLToPath(new URL('../../bin/clavis', import.meta.url))
const serverURL = 'http://127.0.0.1:18110'
const execFileAsync = promisify(execFile)

test('setup → project → secret → reveal → audit', async ({ page }) => {
  const dataDir = readFileSync(marker, 'utf8').trim()
  const token = readFileSync(join(dataDir, 'setup-token'), 'utf8').trim()

  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Set up Clavis' })).toBeVisible()

  await page.getByLabel(/Setup token/i).fill(token)
  await page.getByLabel('Your name').fill('E2E Owner')
  await page.getByLabel('Email').fill('e2e@example.com')
  await page.locator('input[type="password"]').fill('correct horse battery staple')
  await page.getByLabel('Organization').fill('E2E Org')
  await page.getByLabel('First project').fill('Payments API')
  await page.getByRole('button', { name: 'Create instance' }).click()

  await expect(page.getByRole('heading', { name: 'Projects', level: 1 })).toBeVisible()
  const project = page.getByRole('link', { name: /Payments API/ })
  await expect(project).toBeVisible()

  await project.click()
  await expect(page.getByRole('heading', { name: 'Payments API', level: 1 })).toBeVisible()

  const accessibility = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'])
    .analyze()
  expect(accessibility.violations).toEqual([])

  await page.getByRole('button', { name: 'Add secret' }).first().click()
  await page.getByLabel('Key', { exact: true }).fill('STRIPE_SECRET_KEY')
  await page.getByLabel('Value', { exact: true }).fill('sk_test_first_value')
  await page.getByRole('button', { name: /^Add to / }).click()
  await expect(page.getByText('Added STRIPE_SECRET_KEY to dev.')).toBeVisible()

  await page.getByRole('button', { name: 'Reveal STRIPE_SECRET_KEY in dev' }).click()
  await expect(page.getByText('sk_test_first_value')).toBeVisible()

  // Inline edit, review, and atomic save through the batch API.
  await page.getByRole('button', { name: 'Edit STRIPE_SECRET_KEY in dev' }).click()
  await page.getByLabel('New value for STRIPE_SECRET_KEY in dev').fill('sk_test_edited_value')
  await page.getByRole('button', { name: 'Review & save' }).click()
  await expect(page.getByRole('heading', { name: 'Review changes' })).toBeVisible()
  await page.getByRole('button', { name: /^Save 1 change/ }).click()
  await expect(page.getByText('Saved 1 changes.')).toBeVisible()
  await page.getByRole('button', { name: /^(Hide|Reveal) STRIPE_SECRET_KEY in dev/ }).click()
  await page.getByRole('button', { name: 'Reveal STRIPE_SECRET_KEY in dev' }).click()
  await expect(page.getByText('sk_test_edited_value')).toBeVisible()

  await page.getByRole('link', { name: 'Audit log' }).click()
  await expect(page.getByRole('heading', { name: 'Audit log', level: 1 })).toBeVisible()
  await expect(page.getByText('Chain verified')).toBeVisible()
  await expect(page.getByText('secret.reveal').first()).toBeVisible()

  // CLI: device login approved in the browser, then read and run with the secret.
  const cliEnv = {
    ...process.env,
    CLAVIS_SERVER: serverURL,
    CLAVIS_CREDENTIAL_FILE: join(mkdtempSync(join(tmpdir(), 'clavis-cli-')), 'credentials.json'),
  }
  const login = spawn(binary, ['login', '--no-browser'], { env: cliEnv })
  let output = ''
  login.stdout.on('data', (chunk: Buffer) => {
    output += chunk.toString()
  })
  login.stderr.on('data', (chunk: Buffer) => {
    output += chunk.toString()
  })

  await expect
    .poll(() => /Code:\s+([A-Z0-9-]+)/.exec(output)?.[1] ?? '', { timeout: 20_000 })
    .not.toBe('')
  const userCode = /Code:\s+([A-Z0-9-]+)/.exec(output)?.[1] ?? ''

  await page.goto(`/device?code=${userCode}`)
  await expect(page.getByRole('heading', { name: 'Approve CLI login' })).toBeVisible()
  await page.getByRole('button', { name: 'Approve' }).click()
  await expect(page.getByText('CLI approved')).toBeVisible()

  await new Promise<void>((resolve, reject) => {
    const timer = setTimeout(() => {
      reject(new Error(`clavis login timed out: ${output}`))
    }, 30_000)
    login.on('exit', (code) => {
      clearTimeout(timer)
      if (code === 0) resolve()
      else reject(new Error(`clavis login failed (${String(code)}): ${output}`))
    })
  })

  const flags = [
    '--server',
    serverURL,
    '--org',
    'e2e-org',
    '--project',
    'payments-api',
    '--env',
    'dev',
  ]
  const got = await execFileAsync(binary, [...flags, 'secrets', 'get', 'STRIPE_SECRET_KEY'], {
    env: cliEnv,
  })
  expect(got.stdout.trim()).toBe('sk_test_edited_value')

  const run = await execFileAsync(
    binary,
    [...flags, 'run', '--', 'sh', '-c', 'echo "$STRIPE_SECRET_KEY"'],
    { env: cliEnv },
  )
  expect(run.stdout).not.toContain('sk_test_edited_value')
  expect(run.stdout).toContain('••••••')
})
