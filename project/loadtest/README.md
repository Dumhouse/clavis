# Load tests

Targets:

- 10,000 secrets in one project.
- 1,000 reads/second with p95 under 200 ms.

## Seed

```sh
BASE_URL=http://127.0.0.1:8080 TOKEN=clv_mt_... ORG=acme PROJECT=web-app ENV=dev \
  COUNT=10000 node loadtest/seed.mjs
```

The seed script uses the batch endpoint in 100-key chunks.

## Read load

```sh
BASE_URL=http://127.0.0.1:8080 TOKEN=clv_mt_... ORG=acme PROJECT=web-app ENV=dev \
  RATE=1000 DURATION=60s k6 run loadtest/reads.js
```

The read endpoint returns key names only; no values are decrypted. Run against a
dedicated Postgres-backed instance and record hardware, latency percentiles, and
error rate in the milestone results.
