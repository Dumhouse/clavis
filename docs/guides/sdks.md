---
description: "Fetch Clavis secrets from a Node.js, Python or Ruby app at start-up with a tiny dependency-free client, instead of installing the CLI."
---

# Node.js, Python and Ruby clients

If you can run `clavis run`, do: it needs no code changes. When you can't
install the CLI (a serverless function, a slim container), these single-file
clients fetch an environment's secrets at start-up. All have no dependencies.

You need a token with the `secrets:read-values` permission for the environment,
for example a machine identity limited to it:

```sh
clavis identities create web-prod --type machine --project web-app --identity-env production
```

Set `CLAVIS_SERVER`, `CLAVIS_TOKEN`, `CLAVIS_ORG`, `CLAVIS_PROJECT` and
`CLAVIS_ENV` (default `dev`) in the host's settings.

## Node.js (18 or later)

```sh
npm install @dumhouse/clavis-client
```

Or copy [`sdk/node/clavis.mjs`](https://github.com/Dumhouse/clavis/blob/main/sdk/node/clavis.mjs)
into your project and import `./clavis.mjs` instead.

```js
import { loadSecrets } from '@dumhouse/clavis-client'

await loadSecrets() // fills process.env; values already set win
```

`fetchSecrets()` returns the values as an object instead, and both accept
`server`, `token`, `org`, `project`, `env` and `timeoutMs` options.

## Python (3.8 or later)

```sh
pip install clavis-client
```

Or copy [`sdk/python/clavis_client.py`](https://github.com/Dumhouse/clavis/blob/main/sdk/python/clavis_client.py)
into your project; the import is the same.

```python
import clavis_client

clavis_client.load_secrets()  # fills os.environ; values already set win
```

`fetch_secrets()` returns a dict, and both functions accept `server`, `token`,
`org`, `project`, `env` and `timeout`.

## Ruby (3.1 or later)

Copy [`sdk/ruby/clavis_client.rb`](https://github.com/Dumhouse/clavis/blob/main/sdk/ruby/clavis_client.rb)
into your project and require it:

```ruby
require_relative "clavis_client"

Clavis.load_secrets # fills ENV; values already set win
```

`fetch_secrets` returns a hash instead, and both methods accept `server`,
`token`, `org`, `project`, `env` and `timeout` keyword arguments.

## Things to know

- Every fetch is a read of the environment's values and is recorded in the
  audit log.
- Nothing is cached: if the server is down at start-up, the call raises an
  error. Handle it, or fail the start-up on purpose.
- These clients do not mask output. Do not log what they return.
