import { expect, test, type Page } from '@playwright/test'
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

const owner = { email: 'e2e@example.com', password: 'correct horse battery staple' }

/** Sign in with the owner created by the first test. */
async function signIn(page: Page) {
  await page.goto('/')
  const email = page.getByLabel('Email')
  const projects = page.getByRole('heading', { name: 'Projects', level: 1 })
  await email.or(projects).first().waitFor()
  if (await email.isVisible()) {
    await email.fill(owner.email)
    await page.getByLabel('Password').fill(owner.password)
    await page.getByRole('button', { name: 'Sign in with password' }).click()
  }
  await expect(projects).toBeVisible()
}

async function openProject(page: Page, name = 'Payments API') {
  await page.getByRole('link', { name: new RegExp(name) }).click()
  await expect(page.getByRole('heading', { name, level: 1 })).toBeVisible()
}

async function expectNoViolations(page: Page) {
  const result = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'])
    .analyze()
  expect(result.violations).toEqual([])
}

test('setup → project → secret → reveal → audit', async ({ page }) => {
  const dataDir = readFileSync(marker, 'utf8').trim()
  const token = readFileSync(join(dataDir, 'setup-token'), 'utf8').trim()

  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Set up Clavis' })).toBeVisible()

  await page.getByLabel(/Setup token/i).fill(token)
  await page.getByLabel('Your name').fill('E2E Owner')
  await page.getByLabel('Email').fill('e2e@example.com')
  await page.locator('input[type="password"]').fill('correct horse battery staple')
  await page.getByRole('button', { name: 'Create owner account' }).click()

  // Welcome flow: organization, first project and stack, connect commands.
  await expect(page.getByRole('heading', { name: /^Welcome/ })).toBeVisible()
  await page.getByLabel('Organization name').fill('E2E Org')
  await page.getByRole('button', { name: 'Continue' }).click()

  await expect(page.getByRole('heading', { name: 'Your first project' })).toBeVisible()
  await page.getByLabel('Project name').fill('Payments API')
  await page.getByRole('radio', { name: 'Python' }).click()
  await page.getByRole('button', { name: 'Create project' }).click()

  await expect(page.getByRole('heading', { name: 'Connect your app' })).toBeVisible()
  await expect(page.getByText('clavis run python app.py')).toBeVisible()
  await expect(page.getByText('clavis init --org e2e-org --project payments-api')).toBeVisible()
  await expectNoViolations(page)
  await page.getByRole('button', { name: 'Add your first secret' }).click()

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

test('schema editor defines a pattern and validates the grid inline', async ({ page }) => {
  await signIn(page)
  await openProject(page)

  await page.getByRole('button', { name: 'Tools' }).click()
  await page.getByRole('link', { name: 'Schema', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Schema', level: 1 })).toBeVisible()
  await expectNoViolations(page)

  await page.getByRole('button', { name: 'Add key' }).click()
  await page.getByLabel('Key 1', { exact: true }).fill('API_TOKEN')
  await page.getByLabel('Pattern 1', { exact: true }).fill('^sk_')
  await page.getByLabel('Description 1', { exact: true }).fill('Stripe key')
  await page.getByRole('button', { name: 'Save schema' }).click()
  await expect(page.getByText(/Schema saved/)).toBeVisible()

  await page.getByRole('link', { name: 'Secrets', exact: true }).first().click()
  await page.getByRole('button', { name: 'Add secret' }).first().click()
  await page.getByLabel('Key', { exact: true }).fill('API_TOKEN')
  await page.getByLabel('Value', { exact: true }).fill('sk_test_allowed')
  await page.getByRole('button', { name: /^Add to / }).click()
  await expect(page.getByText('Added API_TOKEN to dev.')).toBeVisible()

  await page.getByRole('button', { name: 'Edit API_TOKEN in dev' }).click()
  await page.getByLabel('New value for API_TOKEN in dev').fill('not-a-stripe-key')
  await expect(page.getByText(/Does not match the required pattern/i)).toBeVisible()
  await page.keyboard.press('Escape')
})

test('promotion copies a selected key to another environment', async ({ page }) => {
  await signIn(page)
  await openProject(page)

  await page.getByRole('button', { name: 'Add secret' }).first().click()
  await page.getByLabel('Key', { exact: true }).fill('PROMOTE_KEY')
  await page.getByLabel('Value', { exact: true }).fill('promo-value')
  await page.getByRole('button', { name: /^Add to / }).click()
  await expect(page.getByText('Added PROMOTE_KEY to dev.')).toBeVisible()

  await page.getByRole('link', { name: 'Promote', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Promote', level: 1 })).toBeVisible()
  await page.getByLabel('From environment').selectOption('dev')
  await page.getByLabel('To environment').selectOption('staging')

  await expect(page.getByRole('checkbox', { name: 'Select PROMOTE_KEY' })).toBeVisible()
  const selected = page.getByRole('checkbox', { name: /^Select / })
  for (const box of await selected.all()) {
    if (await box.isChecked()) await box.uncheck()
  }
  await page.getByRole('checkbox', { name: 'Select PROMOTE_KEY' }).check()
  await page.getByRole('button', { name: /Promote 1 key/ }).click()
  await expect(page.getByText(/Promoted 1 key/)).toBeVisible()
  await expectNoViolations(page)

  await page.getByRole('link', { name: 'Secrets', exact: true }).first().click()
  await page.getByRole('button', { name: 'Reveal PROMOTE_KEY in staging' }).click()
  await expect(page.getByText('promo-value')).toBeVisible()
})

test('agent identities start deny-all and accept an allowlist', async ({ page }) => {
  await signIn(page)
  await page.getByRole('link', { name: 'Agents & machines', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Agents & machines', level: 1 })).toBeVisible()

  await page.getByRole('button', { name: 'Add agent or machine' }).click()
  await page.getByText('An AI agent', { exact: true }).click()
  await page.getByLabel('Name', { exact: true }).fill('e2e-agent')
  await page.getByLabel('Project', { exact: true }).selectOption('payments-api')
  await page.getByRole('button', { name: 'Create token' }).click()

  await expect(
    page
      .locator('code')
      .filter({ hasText: /^clv_at_/ })
      .first(),
  ).toBeVisible()
  await expectNoViolations(page)

  await page.getByRole('button', { name: 'Commands', exact: true }).first().click()
  await page.getByLabel('Command patterns').fill('go test ./...\nnpm test')
  await page.getByRole('button', { name: 'Save allowlist' }).click()
  await expect(page.getByText(/2 allowed command patterns/)).toBeVisible()
})

test('keyboard shortcuts from the hint bar work on every page that advertises them', async ({
  page,
}) => {
  await signIn(page)

  // Projects: N opens New project.
  await page.keyboard.press('n')
  await expect(page.getByRole('dialog').getByText('New project').first()).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('dialog')).toHaveCount(0)

  // Global: G then A goes to members; J scrolls without error.
  await page.keyboard.press('g')
  await page.keyboard.press('a')
  await expect(page).toHaveURL(/\/members$/)
  // Members: N opens the invite form.
  await expect(page.getByRole('button', { name: 'Invite member' })).toBeVisible()
  await page.keyboard.press('n')
  await expect(page.getByRole('heading', { name: 'Invite member' })).toBeVisible()
  await page.keyboard.press('Escape')

  // Global: G then S returns to projects.
  await page.keyboard.press('g')
  await page.keyboard.press('s')
  await expect(page.getByRole('heading', { name: 'Projects', level: 1 })).toBeVisible()

  // Project secrets: N add, P paste, / filter.
  await openProject(page)
  await page.keyboard.press('n')
  await expect(page.getByRole('heading', { name: 'Add secret' })).toBeVisible()
  await page.keyboard.press('Escape')
  await page.keyboard.press('p')
  await expect(page.getByRole('dialog')).toBeVisible()
  await page.keyboard.press('Escape')
  await page.keyboard.press('/')
  await expect(page.getByLabel('Filter keys')).toBeFocused()
  await page.keyboard.press('Escape')
  await page.locator('body').click({ position: { x: 1, y: 1 } })

  // Typing in a field must not trigger shortcuts.
  await page.getByLabel('Filter keys').fill('npn')
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await page.getByLabel('Filter keys').fill('')

  // Environments: N opens New environment.
  await page.getByRole('button', { name: 'Tools' }).click()
  await page.getByRole('link', { name: 'Environments', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Environments', level: 1 })).toBeVisible()
  await page.locator('body').click({ position: { x: 1, y: 1 } })
  await page.keyboard.press('n')
  await expect(page.getByRole('heading', { name: 'New environment' })).toBeVisible()
  await page.keyboard.press('Escape')

  // Agents & machines: N opens the add dialog.
  await page.getByRole('link', { name: 'Agents & machines', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Agents & machines', level: 1 })).toBeVisible()
  await page.keyboard.press('n')
  await expect(page.getByRole('heading', { name: 'Add agent or machine' })).toBeVisible()
  await page.keyboard.press('Escape')

  // Audit: / focuses the event filter.
  await page.getByRole('link', { name: 'Audit log', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Audit log', level: 1 })).toBeVisible()
  await page.keyboard.press('/')
  await expect(page.getByLabel('Filter events')).toBeFocused()

  // ? opens the shortcuts sheet.
  await page.locator('body').click({ position: { x: 1, y: 1 } })
  await page.keyboard.press('?')
  await expect(page.getByRole('heading', { name: 'Shortcuts & commands' })).toBeVisible()
})
