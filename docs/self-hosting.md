# Self-hosting

## Docker

```sh
docker run -d --name clavis -p 8080:8080 \
  -v clavis-data:/data \
  -e CLAVIS_SERVER_BASE_URL=https://clavis.example.com \
  ghcr.io/dumhouse/clavis:latest
```

The first-run setup token is printed in the logs and written to
`/data/setup-token`. Back up the root key file (`/data/keys/root.key` by
default) separately from database backups.

## Kubernetes

The Helm chart lives in `deploy/helm/clavis`. It supports:

- SQLite or external Postgres
- multiple API/worker replicas
- `--role=all|api|worker`
- AWS KMS or GCP KMS root keys
- ServiceMonitor and PodDisruptionBudget
- Ingress and TLS

```sh
helm install clavis deploy/helm/clavis \
  --namespace clavis --create-namespace \
  --set config.server.baseURL=https://clavis.example.com \
  --set database.driver=postgres \
  --set database.dsnSecret=clavis-postgres
```

## Postgres

Use a managed Postgres with TLS, automated backups, and PITR. Clavis uses
`LISTEN/NOTIFY` for multi-replica events and `SKIP LOCKED` for jobs.

## Root key

Choose one:

- `file`: mounted secret volume, 0600; back it up separately.
- `env`: injected by the platform secret manager.
- `aws-kms` / `gcp-kms`: no root key at rest in the app.
- Shamir sealing: `clavis admin shamir split --seal`; unseal before start.

## Backups

Manual backup and restore:

```sh
clavis admin backup --out clavis-backup.enc
clavis admin restore clavis-backup.enc --yes
```

Scheduled backups run inside the server when enabled:

```yaml
backup:
  enabled: true
  dir: "/var/lib/clavis/backups"
  interval: 24h
  keep: 7
```

Each run writes an encrypted `clavis-<timestamp>.clvbk` file (0600) and prunes
all but the newest `keep` files. The same superuser is required to restore, so
keep the root key and the backups in separate trust domains. Test a restore
quarterly.

## Upgrades

1. Snapshot the database.
2. Read the release notes and verify signatures.
3. Run migrations by starting the new binary once.
4. Roll API replicas, then workers.
5. Run `clavis health` and `clavis admin audit verify`.

## Reverse proxy

Terminate TLS at the proxy, forward `X-Forwarded-For`, and keep request body
limits aligned with `server.max_body_bytes`. Do not strip the `Authorization`
header or cookies.
