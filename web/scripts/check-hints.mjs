import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

// Fails CI when a hint references a CLI command that is not in the generated
// CLI reference. Keeps the hint bar, shortcut sheet, and docs from drifting.
const hints = readFileSync(fileURLToPath(new URL('../src/lib/hints.ts', import.meta.url)), 'utf8')
const reference = JSON.parse(
  readFileSync(fileURLToPath(new URL('../src/lib/cli-reference.json', import.meta.url)), 'utf8'),
)
const paths = new Set(reference.commands.map((command) => command.path))

const commands = [...hints.matchAll(/cmd:\s*'([^']+)'/g)].map((match) => match[1])
const missing = []
for (const command of commands) {
  const tokens = command.split(/\s+/)
  if (tokens[0] !== 'clavis') {
    missing.push(`${command} (does not start with “clavis”)`)
    continue
  }
  // Walk down the command tree while tokens are words; stop at flags/arguments.
  let path = ''
  for (const token of tokens.slice(1)) {
    if (token.startsWith('-') || /^[A-Z<>"]/.test(token)) break
    const next = path ? `${path} ${token}` : token
    if (!paths.has(next)) break
    path = next
  }
  if (path === '') missing.push(`${command} (no matching command in the CLI reference)`)
}

if (missing.length > 0) {
  console.error('Hint commands missing from the CLI reference:')
  for (const item of missing) console.error(`  - ${item}`)
  process.exit(1)
}
console.log(`hints: ${String(commands.length)} CLI references match the CLI reference`)
