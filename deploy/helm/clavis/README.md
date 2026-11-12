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

## Scaling

SQLite supports one writer. Keep `replicaCount: 1` until the Postgres backend
in M3-01 lands. The deployment uses the `Recreate` strategy to avoid two pods
writing the same PVC.

## Values

See `values.yaml` for probes, resources, affinity, ingress, ServiceMonitor,
extra environment variables, and storage options.
