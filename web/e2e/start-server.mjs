import { spawn } from 'node:child_process'
import { mkdtempSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const webRoot = resolve(here, '..')
const dataDir = mkdtempSync(join(tmpdir(), 'clavis-e2e-'))
const marker = join(webRoot, '.e2e-data-dir')
writeFileSync(marker, dataDir)

const port = process.env.CLAVIS_E2E_PORT ?? '18110'
const binary = process.env.CLAVIS_E2E_BINARY ?? resolve(webRoot, '..', 'bin', 'clavis')

const child = spawn(binary, ['server'], {
  cwd: webRoot,
  env: {
    ...process.env,
    CLAVIS_DATA_DIR: dataDir,
    CLAVIS_SERVER_ADDR: `127.0.0.1:${port}`,
    CLAVIS_SERVER_BASE_URL: `http://127.0.0.1:${port}`,
    CLAVIS_DEV: 'true',
    CLAVIS_LOG_LEVEL: 'error',
  },
  stdio: 'inherit',
})

function shutdown() {
  child.kill('SIGTERM')
  setTimeout(() => {
    child.kill('SIGKILL')
    process.exit(0)
  }, 3000).unref()
}

process.on('SIGTERM', shutdown)
process.on('SIGINT', shutdown)
child.on('exit', (code) => process.exit(code ?? 0))
