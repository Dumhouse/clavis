// Turn the outputs saved by capture.sh into an animated GIF of a terminal
// session: each command is typed, then its real output appears.
// Usage: node scripts/demo/render.mjs CAPTURE_DIR OUT.gif
// Needs Playwright (from web/node_modules, Chrome channel) and ffmpeg.
import { execFileSync } from 'node:child_process'
import { mkdtempSync, readdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const [captureDir, outFile] = process.argv.slice(2)
if (!captureDir || !outFile) throw new Error('usage: render.mjs CAPTURE_DIR OUT.gif')
const here = dirname(fileURLToPath(import.meta.url))
const require = createRequire(resolve(here, '../../web/package.json'))
const { chromium } = require('@playwright/test')

const steps = readdirSync(captureDir)
  .filter((name) => name.endsWith('.json'))
  .sort()
  .map((name) => JSON.parse(readFileSync(join(captureDir, name), 'utf8')))

const escape = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')

// Minimal ANSI SGR to HTML: bold, dim, 16 colors and 24-bit colors.
const basic = ['#1f2937', '#f87171', '#34d399', '#fbbf24', '#60a5fa', '#c084fc', '#22d3ee', '#e5e7eb']
function ansiToHtml(text) {
  let html = ''
  let style = {}
  let open = false
  const flush = () => {
    if (open) html += '</span>'
    const css = []
    if (style.color) css.push(`color:${style.color}`)
    if (style.bold) css.push('font-weight:700')
    if (style.dim) css.push('opacity:.6')
    open = css.length > 0
    if (open) html += `<span style="${css.join(';')}">`
  }
  const parts = text.replace(/\r/g, '').split(/(\x1b\[[0-9;?]*[A-Za-z]|\x1b\][^\x07]*\x07)/)
  for (const part of parts) {
    if (!part) continue
    if (!part.startsWith('\x1b')) {
      html += escape(part)
      continue
    }
    if (!part.endsWith('m') || part.includes('?')) continue
    const codes = part.slice(2, -1).split(';').map(Number)
    for (let i = 0; i < codes.length; i++) {
      const c = codes[i]
      if (c === 0 || Number.isNaN(c)) style = {}
      else if (c === 1) style.bold = true
      else if (c === 2) style.dim = true
      else if (c === 22) style.bold = style.dim = false
      else if (c >= 30 && c <= 37) style.color = basic[c - 30]
      else if (c >= 90 && c <= 97) style.color = basic[c - 90]
      else if (c === 39) delete style.color
      else if (c === 38 && codes[i + 1] === 2) {
        style.color = `rgb(${codes[i + 2]},${codes[i + 3]},${codes[i + 4]})`
        i += 4
      }
    }
    flush()
  }
  if (open) html += '</span>'
  return html
}

const prompt = '<span style="color:#34d399">~/shop</span> <span style="color:#60a5fa">$</span> '
const page_html = `<!doctype html><html><head><meta charset="utf-8"><style>
  body { margin:0; background:#0b1020; }
  .win { width:1040px; height:600px; margin:0; background:#0f172a; border-radius:10px; overflow:hidden;
         font: 14.5px/1.45 "SF Mono", Menlo, Consolas, monospace; color:#e5e7eb; box-sizing:border-box; }
  .bar { height:30px; background:#1e293b; display:flex; align-items:center; padding:0 12px; gap:7px; }
  .dot { width:11px; height:11px; border-radius:50%; }
  .title { margin-left:12px; color:#94a3b8; font-size:12.5px; }
  .term { height:570px; padding:12px 18px; box-sizing:border-box; overflow:hidden;
          display:flex; flex-direction:column; justify-content:flex-end; }
  pre { margin:0; white-space:pre-wrap; word-break:break-word; font:inherit; }
  .cursor { background:#e5e7eb; color:#0f172a; }
</style></head><body><div class="win"><div class="bar">
  <span class="dot" style="background:#f87171"></span><span class="dot" style="background:#fbbf24"></span>
  <span class="dot" style="background:#34d399"></span><span class="title">clavis — ~/shop</span>
</div><div class="term"><pre id="t"></pre></div></div></body></html>`

const work = mkdtempSync(join(tmpdir(), 'clavis-gif-'))
const browser = await chromium.launch({ channel: process.env.PLAYWRIGHT_CHANNEL ?? 'chrome' })
const page = await browser.newPage({ viewport: { width: 1040, height: 600 }, deviceScaleFactor: 1 })
await page.setContent(page_html)

const frames = [] // { file, duration }
let screen = ''
async function frame(html, duration) {
  await page.evaluate((h) => {
    document.getElementById('t').innerHTML = h
  }, html)
  const file = join(work, `${String(frames.length).padStart(4, '0')}.png`)
  await page.screenshot({ path: file })
  frames.push({ file, duration })
}

for (const [index, step] of steps.entries()) {
  // Type the command a few characters at a time.
  const command = step.command
  for (let i = 0; i <= command.length; i += 3) {
    await frame(screen + prompt + escape(command.slice(0, i)) + '<span class="cursor"> </span>', 0.05)
  }
  screen += prompt + escape(command) + '\n'
  await frame(screen, 0.35)
  screen += ansiToHtml(step.output.replace(/\n+$/, '')) + '\n\n'
  const lines = step.output.split('\n').length
  await frame(screen + prompt + '<span class="cursor"> </span>', Math.min(4.5, 1.6 + lines * 0.09))
  if (index < steps.length - 1 && lines > 12) {
    screen = '' // clear before a long output so it fits
  }
}
await browser.close()

const list = frames.map((f) => `file '${f.file}'\nduration ${f.duration}`).join('\n') + `\nfile '${frames.at(-1).file}'\n`
writeFileSync(join(work, 'list.txt'), list)
const palette = join(work, 'palette.png')
execFileSync('ffmpeg', ['-y', '-loglevel', 'error', '-f', 'concat', '-safe', '0', '-i', join(work, 'list.txt'),
  '-vf', 'fps=12,scale=880:-1:flags=lanczos,palettegen=max_colors=96', palette])
execFileSync('ffmpeg', ['-y', '-loglevel', 'error', '-f', 'concat', '-safe', '0', '-i', join(work, 'list.txt'),
  '-i', palette, '-lavfi', 'fps=12,scale=880:-1:flags=lanczos[x];[x][1:v]paletteuse=dither=none', outFile])
rmSync(work, { recursive: true, force: true })
console.log(`wrote ${outFile} from ${steps.length} commands, ${frames.length} frames`)
