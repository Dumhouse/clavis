import assert from 'node:assert/strict'
import { createServer } from 'node:http'
import { test } from 'node:test'

import { fetchSecrets, loadSecrets } from './clavis.mjs'

function fakeServer(handler) {
  return new Promise((resolve) => {
    const server = createServer(handler).listen(0, '127.0.0.1', () => {
      resolve({ server, url: `http://127.0.0.1:${server.address().port}` })
    })
  })
}

test('fetches an environment with the bearer token', async () => {
  let seen
  const { server, url } = await fakeServer((req, res) => {
    seen = { path: req.url, auth: req.headers.authorization }
    res.setHeader('content-type', 'application/json')
    res.end(JSON.stringify({ secrets: [{ key: 'A', value: '1' }, { key: 'B', value: 'two' }] }))
  })
  try {
    const secrets = await fetchSecrets({ server: url, token: 'tok', org: 'acme', project: 'web', env: 'dev' })
    assert.deepEqual(secrets, { A: '1', B: 'two' })
    assert.equal(seen.auth, 'Bearer tok')
    assert.equal(seen.path, '/api/v1/orgs/acme/projects/web/environments/dev/secrets:resolve?channel=api')
  } finally {
    server.close()
  }
})

test('loadSecrets keeps values that are already set', async () => {
  const { server, url } = await fakeServer((req, res) => {
    res.end(JSON.stringify({ secrets: [{ key: 'CLAVIS_SDK_A', value: 'from-clavis' }, { key: 'CLAVIS_SDK_B', value: 'x' }] }))
  })
  process.env.CLAVIS_SDK_A = 'from-shell'
  try {
    const set = await loadSecrets({ server: url, token: 't', org: 'o', project: 'p' })
    assert.deepEqual(set, ['CLAVIS_SDK_B'])
    assert.equal(process.env.CLAVIS_SDK_A, 'from-shell')
    await loadSecrets({ server: url, token: 't', org: 'o', project: 'p', override: true })
    assert.equal(process.env.CLAVIS_SDK_A, 'from-clavis')
  } finally {
    server.close()
    delete process.env.CLAVIS_SDK_A
    delete process.env.CLAVIS_SDK_B
  }
})

test('says what is missing and what the server refused', async () => {
  await assert.rejects(() => fetchSecrets({ server: 'http://x', org: 'o', project: 'p' }), /missing token/)
  const { server, url } = await fakeServer((req, res) => {
    res.statusCode = 403
    res.end('{}')
  })
  try {
    await assert.rejects(() => fetchSecrets({ server: url, token: 't', org: 'o', project: 'p' }), /403/)
  } finally {
    server.close()
  }
})
