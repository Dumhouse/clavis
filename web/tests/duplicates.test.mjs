// Unit tests for the duplicate and similar-environment finder (src/lib/duplicates.ts).
import assert from 'node:assert/strict'
import { test } from 'node:test'

const { findDuplicates, similarEnvironments } = await import('../src/lib/duplicates.ts')

const secret = (key, value, extra = {}) => ({ key, value, kind: 'secret', ...extra })

test('finds a value shared by keys and across environments', () => {
  const envs = [
    {
      slug: 'dev',
      name: 'Dev',
      type: 'development',
      secrets: [
        secret('STRIPE_KEY', 'sk-same-value-123'),
        secret('STRIPE_BACKUP', 'sk-same-value-123'),
        secret('PORT', '3000'),
        secret('FLAG', 'enabled-everywhere', { kind: 'variable' }),
        secret('INHERITED', 'inherited-value-9', { inherited: true }),
      ],
    },
    {
      slug: 'production',
      name: 'Production',
      type: 'production',
      secrets: [secret('STRIPE_KEY', 'sk-same-value-123'), secret('PORT', '3000')],
    },
  ]
  const { sameValue, sharedKey } = findDuplicates(envs)
  assert.deepEqual(sameValue, [{ env: 'dev', keys: ['STRIPE_BACKUP', 'STRIPE_KEY'] }])
  assert.deepEqual(sharedKey, [
    { key: 'STRIPE_KEY', envs: ['dev', 'production'], withProduction: true },
  ])
})

test('ranks environment pairs by identical values', () => {
  const envs = [
    {
      slug: 'dev',
      name: 'Dev',
      secrets: [secret('A', '1111111'), secret('B', '2222222'), secret('C', '3333333')],
    },
    {
      slug: 'staging',
      name: 'Staging',
      secrets: [secret('A', '1111111'), secret('B', 'different'), secret('D', '4444444')],
    },
    { slug: 'prod', name: 'Prod', secrets: [secret('Z', '9999999')] },
  ]
  const pairs = similarEnvironments(envs)
  assert.equal(pairs.length, 3)
  assert.deepEqual(pairs[0], {
    a: 'dev',
    b: 'staging',
    common: 2,
    same: 1,
    onlyA: 1,
    onlyB: 1,
    keyPercent: 50,
    valuePercent: 50,
  })
})
