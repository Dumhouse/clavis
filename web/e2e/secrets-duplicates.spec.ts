import { expect, test, type Page } from '@playwright/test'

// Runs after onboarding.spec.ts (files run in name order with one worker),
// which creates the owner and the Payments API project.
const owner = { email: 'e2e@example.com', password: 'correct horse battery staple' }
const base = '/api/v1/orgs/e2e-org/projects/payments-api'

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

async function addSecret(page: Page, env: string, key: string, value: string) {
  const cookies = await page.context().cookies()
  const csrf = cookies.find((cookie) => cookie.name === 'clavis_csrf')?.value ?? ''
  const response = await page.request.post(`${base}/environments/${env}/secrets`, {
    headers: { 'X-CSRF-Token': csrf },
    data: { key, value, kind: 'secret' },
  })
  expect(
    response.ok(),
    `create ${key} in ${env}: ${String(response.status())} ${await response.text()}`,
  ).toBeTruthy()
}

test('the compare view lists repeated values without showing them in the summary', async ({
  page,
}) => {
  await signIn(page)
  const shared = 'dup-check-value-4f9a1c'
  await addSecret(page, 'dev', 'DUP_PRIMARY', shared)
  await addSecret(page, 'dev', 'DUP_COPY', shared)
  await addSecret(page, 'prod', 'DUP_PRIMARY', shared)

  await page.getByRole('link', { name: /Payments API/ }).click()
  await expect(page.getByRole('heading', { name: 'Payments API', level: 1 })).toBeVisible()
  await page.getByRole('button', { name: 'Compare' }).click()

  const summary = page.locator('details', {
    hasText: 'Repeated values and look-alike environments',
  })
  await expect(summary).toBeVisible()
  await summary.locator('summary').click()
  await expect(summary.getByText('Same value under different keys')).toBeVisible()
  await expect(summary.getByText('DUP_COPY, DUP_PRIMARY')).toBeVisible()
  await expect(summary.getByText('shared with production')).toBeVisible()
  await expect(summary.getByText('How alike the environments are')).toBeVisible()
  // The summary names keys and environments only.
  await expect(summary).not.toContainText(shared)
})
