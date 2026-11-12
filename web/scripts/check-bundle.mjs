import { gzipSync } from 'node:zlib'
import { readFileSync, readdirSync } from 'node:fs'
import { join } from 'node:path'

/**
 * Bundle budget for the embedded SPA. Route-level code splitting keeps the
 * initial chunk small; this guard catches accidental imports of a heavy
 * dependency into the entry bundle.
 */
const assets = join(import.meta.dirname, '..', 'dist', 'assets')
const LIMIT_PER_CHUNK = 180 * 1024
const LIMIT_TOTAL = 340 * 1024

const chunks = readdirSync(assets)
  .filter((name) => name.endsWith('.js'))
  .map((name) => {
    const gzip = gzipSync(readFileSync(join(assets, name))).length
    return { name, gzip }
  })
  .sort((a, b) => b.gzip - a.gzip)

const total = chunks.reduce((sum, chunk) => sum + chunk.gzip, 0)
const failures = chunks.filter((chunk) => chunk.gzip > LIMIT_PER_CHUNK)

for (const chunk of chunks.slice(0, 10)) {
  console.log(`${(chunk.gzip / 1024).toFixed(1).padStart(7)} KB  ${chunk.name}`)
}
if (chunks.length > 10) console.log(`  … ${String(chunks.length - 10)} more chunks`)
console.log(`${(total / 1024).toFixed(1).padStart(7)} KB  total (${String(chunks.length)} chunks)`)

if (failures.length > 0) {
  for (const chunk of failures) {
    console.error(
      `bundle budget exceeded: ${chunk.name} is ${(chunk.gzip / 1024).toFixed(1)} KB gzip (limit ${(LIMIT_PER_CHUNK / 1024).toFixed(0)} KB)`,
    )
  }
  process.exit(1)
}
if (total > LIMIT_TOTAL) {
  console.error(
    `bundle budget exceeded: total is ${(total / 1024).toFixed(1)} KB gzip (limit ${(LIMIT_TOTAL / 1024).toFixed(0)} KB)`,
  )
  process.exit(1)
}
console.log('bundle budget ok')
