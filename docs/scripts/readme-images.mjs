import { mkdir } from 'node:fs/promises'
import { createRequire } from 'node:module'
import { fileURLToPath } from 'node:url'

/*
 * Capture the landing-page mockups as JPEGs for the README, in light and dark.
 * GitHub cannot run the Vue components, so the README shows these images and
 * swaps them with <picture> by the reader's theme.
 *
 *   cd docs && pnpm dev            # site on http://localhost:5174
 *   node docs/scripts/readme-images.mjs [base-url]
 *
 * Playwright comes from web/ so the docs package stays dependency-free.
 */

const require = createRequire(new URL('../../web/package.json', import.meta.url))
const { chromium } = require('@playwright/test')

const base = process.argv[2] || 'http://localhost:5174/'
const outDir = fileURLToPath(new URL('../assets/readme/', import.meta.url))
await mkdir(outDir, { recursive: true })

// name -> selector on the landing page. Blocks are captured with their stage.
const targets = [
  ['hero', '.hero-mock', { pad: 48 }],
  ['secrets', '.block:nth-of-type(4) .stage'],
  ['schema', '.block:nth-of-type(5) .stage'],
  ['access', '.block:nth-of-type(6) .stage'],
  ['agents', '.agents .flow-diagram', { pad: 32, onlyOnce: true }],
]

const browser = await chromium.launch()
for (const theme of ['light', 'dark']) {
  const context = await browser.newContext({
    viewport: { width: 1360, height: 1000 },
    deviceScaleFactor: 2,
    colorScheme: theme,
    // Static frames: the hero shows two keys ticked, terminals are complete.
    reducedMotion: 'reduce',
  })
  await context.addInitScript((t) => localStorage.setItem('vitepress-theme-appearance', t), theme)
  const page = await context.newPage()
  await page.goto(base, { waitUntil: 'networkidle' })
  await page.waitForTimeout(800)

  for (const [name, selector, opts = {}] of targets) {
    if (opts.onlyOnce && theme === 'dark') continue // already dark in both themes
    const el = page.locator(selector).first()
    await el.scrollIntoViewIfNeeded()
    const box = await el.boundingBox()
    if (!box) throw new Error(`no element for ${selector}`)
    const pad = opts.pad ?? 0
    const file = opts.onlyOnce ? `${name}.jpg` : `${name}-${theme}.jpg`
    await page.screenshot({
      path: outDir + file,
      type: 'jpeg',
      quality: 88,
      clip: { x: box.x - pad, y: box.y - pad, width: box.width + pad * 2, height: box.height + pad * 2 },
    })
    console.log('captured', file)
  }
  await context.close()
}
await browser.close()
