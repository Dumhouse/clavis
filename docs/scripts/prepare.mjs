import { cp, mkdir, rm, writeFile } from 'node:fs/promises'
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
