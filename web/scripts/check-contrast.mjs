import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'

// Verifies the WCAG 2.2 AA contrast acceptance criterion for all four
// appearance combinations (M0-26). Parses the token blocks in styles.css so
// the test cannot drift from the real values.

const css = readFileSync(fileURLToPath(new URL('../src/styles.css', import.meta.url)), 'utf8')

function block(selector) {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  const match = css.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`, 'm'))
  if (!match) throw new Error(`missing token block ${selector}`)
  const tokens = {}
  for (const line of match[1].split('\n')) {
    const m = line.match(/--([\w-]+):\s*(#[0-9a-fA-F]{6})\s*;/)
    if (m) tokens[m[1]] = m[2]
  }
  return tokens
}

const base = block(':root')
const light = block(":root[data-theme='light']")
const warm = block(":root[data-style='warm']")
const warmDark = block(":root[data-style='warm'][data-theme='dark']")
const warmLight = block(":root[data-style='warm'][data-theme='light']")

const combinations = {
  'classic dark': { ...base },
  'classic light': { ...base, ...light },
  'warm dark': { ...base, ...warm, ...warmDark },
  'warm light': { ...base, ...light, ...warm, ...warmLight },
}

function luminance(hex) {
  const channels = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255)
  const [r, g, b] = channels.map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4))
  return 0.2126 * r + 0.7152 * g + 0.0722 * b
}

function contrast(a, b) {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x)
  return (hi + 0.05) / (lo + 0.05)
}

const foregrounds = [
  'text',
  'muted',
  'dim',
  'accent-text',
  'blue-fg',
  'violet-fg',
  'amber-fg',
  'green-fg',
]
const backgrounds = ['bg', 'surface', 'panel']
let failures = 0
let minimum = Infinity

for (const [name, tokens] of Object.entries(combinations)) {
  for (const fg of foregrounds) {
    for (const bg of backgrounds) {
      if (!tokens[fg] || !tokens[bg]) continue
      const ratio = contrast(tokens[fg], tokens[bg])
      minimum = Math.min(minimum, ratio)
      if (ratio < 4.5) {
        failures++
        console.error(`${name}: --${fg} on --${bg} = ${ratio.toFixed(2)} (< 4.5)`)
      }
    }
  }
}

if (failures > 0) {
  console.error(`\n${failures} contrast failure(s)`)
  process.exit(1)
}
console.log(`contrast: all combinations pass AA (minimum ${minimum.toFixed(2)}:1)`)
