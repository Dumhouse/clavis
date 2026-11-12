import { cp, mkdir, readFile, rm, writeFile } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'

/*
 * Assemble public assets for the docs site. Brand files live at the repo root
 * so the app, landing page, and docs share one source of truth.
 */
const publicDir = fileURLToPath(new URL('../public', import.meta.url))
await mkdir(publicDir, { recursive: true })

const brandDir = fileURLToPath(new URL('../../brand', import.meta.url))
const target = `${publicDir}/brand`
await rm(target, { recursive: true, force: true })
await cp(brandDir, target, { recursive: true })

// Keep GitHub Pages from running Jekyll over the built assets.
await writeFile(`${publicDir}/.nojekyll`, '')

// The changelog lives at the repo root; publish it as a docs page.
const changelog = await readFile(new URL('../../CHANGELOG.md', import.meta.url), 'utf8')
await writeFile(
  new URL('../changelog.md', import.meta.url),
  `---\ndescription: Release notes for Clavis.\n---\n\n<!-- Generated from CHANGELOG.md by scripts/prepare.mjs. Do not edit. -->\n\n${changelog}`,
)

// Let crawlers in and point them at the sitemap.
const origin = (process.env.DOCS_HOSTNAME || '').replace(/\/$/, '')
const base = process.env.DOCS_BASE || '/'
await writeFile(
  `${publicDir}/robots.txt`,
  `User-agent: *\nAllow: /\n${origin ? `\nSitemap: ${origin}${base}sitemap.xml\n` : ''}`,
)
