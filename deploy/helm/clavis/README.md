# Clavis Helm chart

Installs the single-binary Clavis server.

```sh
helm install clavis ./deploy/helm/clavis \
  --namespace clavis --create-namespace \
  --set server.baseURL=https://clavis.example.com \
  --set ingress.enabled=true \
  --set ingress.className=nginx \
  --set ingress.hosts[0].host=clavis.example.com
```

## Persistence

The chart creates a PVC mounted at `/data`. It contains the SQLite database,
the generated root key, and the one-time setup token. Back up `/data`, or at
minimum the root key, before pointing production traffic at the install.

## Root key

Default: a file key generated at `/data/root.key` with `0600` permissions.

Environment-provider alternative:

```yaml
keys:
  provider: env
  existingSecret: clavis-root-key
  secretKey: root-key
```

The secret must contain a base64 32-byte key.

## Email

The chart never stores the SMTP password in values. Use an existing secret:

```yaml
email:
  enabled: true
  host: smtp.example.com
  from: "Clavis <no-reply@example.com>"
  existingSecret: clavis-email
  passwordKey: email-password
```

## Audit exporters

Pass the same exporter structure as `clavis.yaml`:

```yaml
auditExporters:
  - type: webhook
    url: https://siem.example.com/clavis
    format: json
```

## Postgres and scaling

```yaml
database:
  driver: postgres
  existingSecret: clavis-postgres
  dsnKey: database-dsn

replicaCount: 3
```

SQLite stays at one replica and uses the `Recreate` strategy. Postgres allows
multiple replicas with `RollingUpdate`; SSE events fan out over Postgres
`LISTEN/NOTIFY` and job workers claim with `SKIP LOCKED`.

Split process roles with `role: api` or `role: worker`; the default `all` runs
both in each replica.

## KMS root keys

AWS KMS:

```yaml
keys:
  provider: aws-kms
  awsRegion: eu-central-1
  awsKMSKeyID: arn:aws:kms:...
  awsAccessKey: AKIA...
  awsExistingSecret: clavis-aws
  awsSecretKey: aws-secret-key
```

GCP KMS:

```yaml
keys:
  provider: gcp-kms
  gcpKMSKey: projects/p/locations/l/keyRings/r/cryptoKeys/k
  gcpServiceAccountExistingSecret: clavis-gcp
  gcpServiceAccountKey: gcp-service-account.json
```

The wrapped root key file still lives on `/data`; use shared storage if several
replicas must read it. KMS protects the plaintext key, not the wrapped blob.

## Values

See `values.yaml` for probes, resources, affinity, ingress, ServiceMonitor,
extra environment variables, and storage options.
