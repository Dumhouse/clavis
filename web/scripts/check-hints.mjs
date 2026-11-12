import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

// Fails CI when a hint references a CLI command that is not in the CLI
// reference. Keeps the hint bar, shortcut sheet, and docs from drifting.
const hints = readFileSync(fileURLToPath(new URL('../src/lib/hints.ts', import.meta.url)), 'utf8')
const docs = readFileSync(fileURLToPath(new URL('../../docs/cli.md', import.meta.url)), 'utf8')

const commands = [...hints.matchAll(/cmd:\s*'([^']+)'/g)].map((match) => match[1])
const missing = []
for (const command of commands) {
  const tokens = command.split(/\s+/)
  let prefix = tokens.slice(0, 2).join(' ')
  const third = tokens[2]
  if (third && !third.startsWith('-') && !/^[A-Z<>]/.test(third)) {
    prefix = tokens.slice(0, 3).join(' ')
  }
  if (!docs.includes(prefix)) missing.push(`${command} (expected “${prefix}”)`)
}

if (missing.length > 0) {
  console.error('Hint commands missing from docs/cli.md:')
  for (const item of missing) console.error(`  - ${item}`)
  process.exit(1)
}
console.log(`hints: ${String(commands.length)} CLI references match docs/cli.md`)
