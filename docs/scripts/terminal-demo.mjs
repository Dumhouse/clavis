import { writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'

/*
 * Build docs/assets/terminal-demo.svg: a looping, typed terminal session for
 * the README. CSS animations inside an SVG play on GitHub (it renders README
 * SVGs as images), stay sharp at any size, and need no GIF tooling.
 *
 *   node docs/scripts/terminal-demo.mjs
 */

const lines = [
  { t: 'cmt', s: '# sign in once and link the repo' },
  { t: 'cmd', s: 'clavis login' },
  { t: 'ok', s: '✓ Signed in as ada@acme.dev (token stored in OS keychain)' },
  { t: 'cmd', s: 'clavis link --project web-app --env development' },
  { t: 'ok', s: '✓ Wrote .clavis.yaml' },
  { t: 'cmt', s: '# run with secrets injected; output is masked' },
  { t: 'cmd', s: 'clavis run -- pnpm dev' },
  { t: 'out', s: '  ready  DATABASE_URL=postgres://app:******@db:5432/app' },
  { t: 'out', s: '  ready  STRIPE_SECRET_KEY=******' },
  { t: 'cmt', s: '# typed config fails CI before bad values ship' },
  { t: 'cmd', s: 'clavis check --env production' },
  { t: 'bad', s: '✕ SENTRY_DSN  required in production, missing' },
  { t: 'cmt', s: '# agents run allowlisted commands, never see values' },
  { t: 'cmd', s: 'clavis agents allow claude-code "pnpm test"' },
  { t: 'ok', s: '✓ claude-code may run: pnpm test' },
]

const W = 760
const PAD = 22
const TOP = 52
const LH = 22
const CHAR = 8.43 // advance width of a 14px monospace glyph
const H = TOP + lines.length * LH + 20

const colors = { cmd: '#f4f4f5', out: '#a1a1aa', cmt: '#71717a', ok: '#6ee7b7', bad: '#fca5a5' }

// Timeline in seconds.
let t = 0.6
const timing = lines.map((l) => {
  const start = t
  const dur = l.t === 'cmd' ? l.s.length * 0.045 : 0
  t = start + dur + (l.t === 'cmd' ? 0.35 : l.t === 'cmt' ? 0.5 : 0.25)
  return { start, end: start + dur }
})
const HOLD = 4
const T = t + HOLD
const pct = (s) => ((s / T) * 100).toFixed(3) + '%'
const FADE = pct(T - 0.4)

const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;')

let css = ''
let defs = ''
let body = ''

lines.forEach((l, i) => {
  const y = TOP + i * LH + 15
  const { start, end } = timing[i]
  css += `@keyframes v${i}{0%,${pct(start)}{opacity:0}${pct(start + 0.001)},${FADE}{opacity:1}100%{opacity:0}}\n`
  css += `.v${i}{animation:v${i} ${T.toFixed(2)}s linear infinite}\n`

  if (l.t === 'cmd') {
    const full = (l.s.length + 1) * CHAR
    css += `@keyframes w${i}{0%,${pct(start)}{width:0}${pct(end)},100%{width:${full.toFixed(1)}px}}\n`
    css += `.w${i}{animation:w${i} ${T.toFixed(2)}s steps(${l.s.length},end) infinite}\n`
    defs += `<clipPath id="c${i}"><rect class="w${i}" x="${PAD + 2 * CHAR}" y="${y - 15}" width="0" height="${LH}"/></clipPath>`
    body += `<g class="v${i}"><text x="${PAD}" y="${y}" fill="#34d399">$</text>`
    body += `<text x="${PAD + 2 * CHAR}" y="${y}" fill="${colors.cmd}" clip-path="url(#c${i})">${esc(l.s)}</text></g>`
  } else {
    body += `<text class="v${i}" x="${PAD}" y="${y}" fill="${colors[l.t]}">${esc(l.s)}</text>`
  }
})

const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${W}" height="${H}" viewBox="0 0 ${W} ${H}" role="img" aria-label="Terminal: clavis login, link, run with masked secrets, check, and agent allowlist">
<style>
text{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:14px;white-space:pre}
${css}@media (prefers-reduced-motion:reduce){*{animation:none!important}g,text{opacity:1}rect{width:${W}px}}
</style>
<defs>${defs}</defs>
<rect width="${W}" height="${H}" rx="12" fill="#0b0b0e"/>
<rect x=".5" y=".5" width="${W - 1}" height="${H - 1}" rx="11.5" fill="none" stroke="#2a2a31"/>
<path d="M0 12a12 12 0 0 1 12-12h${W - 24}a12 12 0 0 1 12 12v24H0Z" fill="#101014"/>
<path d="M0 36h${W}" stroke="#1c1c22"/>
<circle cx="22" cy="18" r="5.5" fill="#2a2a31"/><circle cx="40" cy="18" r="5.5" fill="#2a2a31"/><circle cx="58" cy="18" r="5.5" fill="#2a2a31"/>
<text x="${W / 2}" y="22" fill="#71717a" text-anchor="middle" style="font-family:-apple-system,Segoe UI,sans-serif;font-size:12px">clavis — zsh</text>
${body}
</svg>
`

const out = fileURLToPath(new URL('../assets/terminal-demo.svg', import.meta.url))
await writeFile(out, svg)
console.log(`wrote ${out} (${T.toFixed(1)}s loop, ${lines.length} lines)`)
