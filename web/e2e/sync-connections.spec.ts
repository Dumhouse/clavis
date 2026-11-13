import { expect, test, type Page } from '@playwright/test'
import AxeBuilder from '@axe-core/playwright'

// Runs after onboarding.spec.ts (files run in name order with one worker),
// which creates the owner and the Payments API project.
const owner = { email: 'e2e@example.com', password: 'correct horse battery staple' }

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

test('a saved connection is picked instead of pasting the token again', async ({ page }) => {
  await signIn(page)
  await page.getByRole('link', { name: 'Integrations', exact: true }).first().click()
  await expect(page.getByRole('heading', { name: 'Integrations', level: 1 })).toBeVisible()
  const connections = page.getByRole('region', { name: 'Connections' })
  await expect(connections.getByText('No connections yet.', { exact: false })).toBeVisible()

  // Save the Forgejo credential once.
  await connections.getByRole('button', { name: 'Add connection' }).click()
  const dialog = page.getByRole('dialog', { name: /Add a connection/ })
  await dialog.getByLabel('Provider').selectOption('forgejo')
  await dialog.getByLabel('Name').fill('E2E Forgejo')
  await dialog.getByLabel('Forgejo URL (optional)').fill('https://git.example.test')
  await dialog.getByLabel('Access token').fill('fj-e2e-token')
  await dialog.getByRole('button', { name: 'Save connection' }).click()
  await expect(dialog).toBeHidden()
  await expect(connections.getByText('E2E Forgejo')).toBeVisible()
  await expect(connections.getByText('Not used by any sync', { exact: false })).toBeVisible()
  await expect(page.getByText('fj-e2e-token')).toHaveCount(0)

  // Scoped to the new section. The decorative provider marks use brand colors
  // that fail contrast across the whole page; that predates connections.
  const accessibility = await new AxeBuilder({ page })
    .include('section[aria-labelledby="connections-heading"]')
    .exclude('span[aria-hidden="true"]')
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa', 'wcag22aa'])
    .analyze()
  expect(accessibility.violations).toEqual([])

  // A new sync only asks for the repository; the connection is preselected.
  await page.getByRole('button', { name: /Forgejo Actions/ }).click()
  const create = page.getByRole('dialog', { name: /Sync to Forgejo Actions/ })
  await create.getByLabel('Repository or organization').fill('team/web')
  await expect(create.getByRole('radio', { name: /E2E Forgejo/ })).toBeChecked()
  await expect(create.getByLabel('Access token')).toHaveCount(0)
  await create.getByRole('button', { name: 'Create sync' }).click()
  await expect(create).toBeHidden()
  await expect(page.getByText('Sync to Forgejo Actions created', { exact: false })).toBeVisible()
  await expect(page.getByTitle('Uses a shared connection')).toContainText('E2E Forgejo')

  // The connection shows where it is used and cannot be deleted while in use.
  await expect(connections.getByText(/Used by 1 sync: payments-api\/dev/)).toBeVisible()
  await expect(
    connections.getByRole('button', { name: 'Delete connection E2E Forgejo' }),
  ).toBeDisabled()

  // Testing an unreachable server reports a clear failure.
  await connections.getByRole('button', { name: 'Test' }).click()
  const result = connections.getByRole('status')
  await expect(result).toContainText('Something needs fixing')
  await expect(result).toContainText(/Cannot reach https:\/\/git\.example\.test/)
  await result.getByRole('button', { name: 'Dismiss test result' }).click()
  await expect(result).toBeHidden()
})
