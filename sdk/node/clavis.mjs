// A tiny Clavis client for Node.js 18+ (no dependencies). It fetches one
// environment's secrets at start-up, so an app does not need the CLI.
//
//   import { loadSecrets } from './clavis.mjs'
//   await loadSecrets()            // fills process.env from CLAVIS_* settings
//
// Settings come from the arguments or from CLAVIS_SERVER, CLAVIS_TOKEN,
// CLAVIS_ORG, CLAVIS_PROJECT and CLAVIS_ENV. The token needs the
// secrets:read-values permission for the environment. Reads are audited.

/**
 * Fetch an environment's secrets as an object of NAME -> value.
 * File secrets and empty values are included as they are stored.
 */
export async function fetchSecrets(options = {}) {
  const env = process.env
  const server = (options.server ?? env.CLAVIS_SERVER ?? '').replace(/\/+$/, '')
  const token = options.token ?? env.CLAVIS_TOKEN
  const org = options.org ?? env.CLAVIS_ORG
  const project = options.project ?? env.CLAVIS_PROJECT
  const environment = options.env ?? env.CLAVIS_ENV ?? 'dev'
  for (const [name, value] of Object.entries({ server, token, org, project })) {
    if (!value) throw new Error(`clavis: missing ${name} (pass it or set CLAVIS_${name.toUpperCase()})`)
  }
  const path = [org, project, environment].map(encodeURIComponent)
  const url = `${server}/api/v1/orgs/${path[0]}/projects/${path[1]}/environments/${path[2]}/secrets:resolve?channel=api`
  const response = await (options.fetch ?? fetch)(url, {
    headers: { Authorization: `Bearer ${token}`, 'User-Agent': 'clavis-node-sdk' },
    signal: options.signal ?? AbortSignal.timeout(options.timeoutMs ?? 10_000),
  })
  if (!response.ok) {
    throw new Error(`clavis: the server answered ${response.status} ${response.statusText}`)
  }
  const body = await response.json()
  return Object.fromEntries((body.secrets ?? []).map((secret) => [secret.key, secret.value ?? '']))
}

/**
 * Fetch the secrets and put them in process.env. Values already set in the
 * environment win unless `override: true`. Returns the names that were set.
 */
export async function loadSecrets(options = {}) {
  const secrets = await fetchSecrets(options)
  const set = []
  for (const [key, value] of Object.entries(secrets)) {
    if (options.override || process.env[key] === undefined) {
      process.env[key] = value
      set.push(key)
    }
  }
  return set
}
