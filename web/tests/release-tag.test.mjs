// Unit tests for src/lib/release-tag.ts. Run with `npm test`.
import assert from 'node:assert/strict'
import { test } from 'node:test'

const { releaseTag } = await import('../src/lib/release-tag.ts')

test('a release maps to its tag', () => {
  assert.equal(releaseTag('0.3.3'), 'v0.3.3')
  assert.equal(releaseTag('v1.2.0'), 'v1.2.0')
})

test('a development build maps to the release it comes after', () => {
  assert.equal(releaseTag('0.3.2+dev.752e7e5.dirty'), 'v0.3.2')
})

test('an unknown version falls back to main', () => {
  assert.equal(releaseTag('dev'), 'main')
  assert.equal(releaseTag(undefined), 'main')
})
