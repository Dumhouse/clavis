# Install with Helm

Clavis publishes its Helm chart to the GitHub Container Registry as an OCI
artifact. Each Clavis release publishes a chart with the same version, so chart
`0.3.0` installs Clavis 0.3.0. The examples below use `0.3.0`; use the release
you want to install.

## Install

Helm 3.8 or newer can install OCI charts. Save your non-secret settings in a
file such as `clavis-values.yaml`, then install the chart:

```sh
helm install clavis oci://ghcr.io/dumhouse/charts/clavis \
  --namespace clavis --create-namespace \
  --version 0.3.0 \
  --values clavis-values.yaml
```

For example, this values file sets the public URL and enables an existing
ingress controller:

```yaml
server:
  baseURL: https://clavis.example.com

ingress:
  enabled: true
  className: nginx
  hosts:
    - host: clavis.example.com
      paths:
        - path: /
          pathType: Prefix
  tls:
    - hosts:
        - clavis.example.com
      secretName: clavis-tls

persistence:
  enabled: true
  size: 10Gi
```

Create the TLS Secret and install an ingress controller separately. The chart
defaults to a 10 Gi persistent volume claim mounted at `/data`. Set
`persistence.storageClass` or `persistence.existingClaim` to use your storage
class or an existing claim.

## Image and database settings

The chart runs the server image of its own release by default: chart `0.3.0`
uses `ghcr.io/dumhouse/clavis:0.3.0`. Set `image.tag` only to run a different
server image than the chart was released with, and `image.repository` to pull
from a mirror.

SQLite is the default database and stores `/data/clavis.db` on the persistent
volume. The default file key is `/data/root.key` on that volume as well. SQLite
supports one replica; the chart rejects a higher `replicaCount` when
`database.driver` is `sqlite`.

For PostgreSQL, create a Kubernetes Secret in the `clavis` namespace with a
`database-dsn` key, then configure the chart to read it:

```yaml
database:
  driver: postgres
  existingSecret: clavis-postgres
  dsnKey: database-dsn
```

Keep the DSN in a Secret manager or another protected workflow; do not put
credentials in a values file committed to Git. PostgreSQL is required for more
than one replica. If you scale replicas while using the file key provider, all
replicas must be able to read the same root key file. The default volume uses
`ReadWriteOnce`; configure suitable shared storage or a key provider that gives
each replica access to the same key.

## Back up the root key

The root key is required to decrypt stored secrets. Keep a protected recovery
copy of it separate from database backups, and test that your backup and restore
procedure works. With SQLite, make a consistent backup of the database and
root key from the persistent volume. With PostgreSQL, back up the database and
protect the root key independently. Do not store the root key in Helm values or
in source control. See [self-hosting](./self-hosting.md#backups) for Clavis's
encrypted backup and restore commands.

## Upgrade

Back up the database and root key before upgrading. Keep using the same values
file so settings such as the database Secret, ingress, and persistence are
preserved:

```sh
helm upgrade clavis oci://ghcr.io/dumhouse/charts/clavis \
  --namespace clavis \
  --version 0.3.0 \
  --values clavis-values.yaml
```

Change `--version` to the release you are upgrading to; the server image
follows it. After the upgrade, check the rollout with `kubectl -n clavis
rollout status deployment/clavis` (adjust the deployment name if you set a Helm
name override).
