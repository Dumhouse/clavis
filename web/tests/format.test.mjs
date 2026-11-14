// Unit tests for date parsing and formatting (src/lib/format.ts).
// Run with `npm test`; Node strips the TypeScript types itself.
import assert from 'node:assert/strict'
import { beforeEach, test } from 'node:test'

const store = new Map()
globalThis.localStorage = {
  getItem: (key) => (store.has(key) ? store.get(key) : null),
  setItem: (key, value) => store.set(key, String(value)),
  removeItem: (key) => store.delete(key),
}
globalThis.window = new EventTarget()
globalThis.CustomEvent ??= class extends Event {}

const format = await import('../src/lib/format.ts')
const { formatDate, formatISODate, parseDateInput, datePlaceholder, setFormatPrefs } = format

beforeEach(() => {
  setFormatPrefs({ date: 'dmy-dot', time: '24h' })
})

test('reads day-first dates with any separator', () => {
  assert.equal(parseDateInput('1.3.2027'), '2027-03-01')
  assert.equal(parseDateInput('01.03.2027'), '2027-03-01')
  assert.equal(parseDateInput('1/3/2027'), '2027-03-01')
  assert.equal(parseDateInput(' 31-12-2026 '), '2026-12-31')
})

test('reads month-first dates when that is the setting', () => {
  setFormatPrefs({ date: 'mdy' })
  assert.equal(parseDateInput('3/1/2027'), '2027-03-01')
  assert.equal(parseDateInput('12/31/2026'), '2026-12-31')
})

test('a four-digit first part is always year-first', () => {
  assert.equal(parseDateInput('2027-03-01'), '2027-03-01')
  setFormatPrefs({ date: 'mdy' })
  assert.equal(parseDateInput('2027.3.1'), '2027-03-01')
})

test('rejects dates that do not exist or are incomplete', () => {
  for (const text of [
    '31.02.2026',
    '29.02.2027',
    '1.13.2026',
    '0.1.2026',
    '1.3',
    '1.3.27',
    'abc',
    '',
    '1.3.2027x',
  ]) {
    assert.equal(parseDateInput(text), null, text)
  }
  assert.equal(parseDateInput('29.02.2028'), '2028-02-29')
})

test('shows dates in the chosen format', () => {
  const day = new Date(2026, 11, 31)
  assert.equal(formatDate(day), '31.12.2026')
  setFormatPrefs({ date: 'dmy-slash' })
  assert.equal(formatDate(day), '31/12/2026')
  setFormatPrefs({ date: 'mdy' })
  assert.equal(formatDate(day), '12/31/2026')
  setFormatPrefs({ date: 'iso' })
  assert.equal(formatDate(day), '2026-12-31')
  assert.equal(formatDate(null), '—')
  assert.equal(formatDate('not a date', 'never'), 'never')
})

test('round-trips stored dates and gives a matching placeholder', () => {
  assert.equal(formatISODate('2027-03-01'), '01.03.2027')
  assert.equal(formatISODate('bad'), '')
  assert.equal(datePlaceholder(), 'dd.mm.yyyy')
  setFormatPrefs({ date: 'mdy' })
  assert.equal(datePlaceholder(), 'mm/dd/yyyy')
  setFormatPrefs({ date: 'iso' })
  assert.equal(datePlaceholder(), 'yyyy-mm-dd')
})

test('account preferences apply only known values', () => {
  format.applyAccountFormatPrefs('iso', 'bogus')
  assert.equal(formatDate(new Date(2026, 0, 2)), '2026-01-02')
  format.applyAccountFormatPrefs(undefined, undefined)
  assert.equal(formatDate(new Date(2026, 0, 2)), '2026-01-02')
})
