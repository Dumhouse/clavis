// Seed a project with 10,000 secrets through the batch API.
// Usage: BASE_URL=... TOKEN=... ORG=... PROJECT=... ENV=... COUNT=10000 node loadtest/seed.mjs
const base = process.env.BASE_URL
const token = process.env.TOKEN
const org = process.env.ORG
const project = process.env.PROJECT
const env = process.env.ENV || 'dev'
const count = Number(process.env.COUNT || 10000)
if (!base || !token || !org || !project) {
  console.error('BASE_URL, TOKEN, ORG, and PROJECT are required')
  process.exit(1)
}
const path = `${base}/api/v1/orgs/${org}/projects/${project}/environments/${env}/secrets`
const headers = { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' }
const batchSize = 100
for (let offset = 0; offset < count; offset += batchSize) {
  const changes = []
  for (let i = offset; i < Math.min(offset + batchSize, count); i++) {
    changes.push({ key: `LOAD_SECRET_${i}`, value: `value-${i}`, kind: 'secret' })
  }
  const list = await fetch(path, { headers })
  if (!list.ok) {
    console.error('list failed', list.status, await list.text())
    process.exit(1)
  }
  const etag = list.headers.get('ETag') ?? ''
  const response = await fetch(`${path}:batch`, {
    method: 'PATCH',
    headers: { ...headers, 'If-Match': etag },
    body: JSON.stringify({ changes, message: 'Load test seed' }),
  })
  if (!response.ok) {
    console.error('batch failed', response.status, await response.text())
    process.exit(1)
  }
  console.log(`seeded ${Math.min(offset + batchSize, count)}/${count}`)
}
