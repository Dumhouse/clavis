import { cp, rm, mkdir } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'

const target = fileURLToPath(new URL('../.docs', import.meta.url))
const source = fileURLToPath(new URL('../../docs', import.meta.url))
await rm(target, { recursive: true, force: true })
await mkdir(target, { recursive: true })
await cp(source, target, {
  recursive: true,
  filter: (path) => !path.includes('/.vitepress') && !path.includes('/node_modules'),
})
