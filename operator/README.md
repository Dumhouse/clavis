# Clavis Kubernetes operator

Reconciles `ClavisSecret` resources into Kubernetes `Secret` objects and
restarts annotated Deployments when values change.

## Install

```sh
kubectl apply -f config/crd/clavissecrets.yaml
kubectl create namespace clavis-system
kubectl apply -f config/rbac/rbac.yaml
kubectl apply -f config/manager/manager.yaml
```

## Token

Create a scoped Clavis identity and store its token in a Secret:

```sh
kubectl create secret generic clavis-token \
  --from-literal=token=clv_mt_...
```

The identity needs `secrets:read-values` for the configured environment.

## ClavisSecret

```yaml
apiVersion: clavis.dumhouse.github.io/v1alpha1
kind: ClavisSecret
metadata:
  name: web-app-dev
spec:
  server: https://clavis.example.com
  org: acme
  project: web-app
  environment: dev
  secretName: web-app-dev
  tokenSecretRef:
    name: clavis-token
    key: token
  refreshInterval: 5m
  restartDeployments: [web-app]
  keys: [DATABASE_URL, STRIPE_KEY]
```

- `secretName` defaults to the resource name.
- `keys` empty means every resolved key.
- `restartDeployments` are annotated with
  `clavis.dumhouse.github.io/restartedAt` only when values change.
- File secrets are written as normal Secret keys; mount them where needed.

## Development

```sh
go test ./...
go run . --leader-elect=false
```
