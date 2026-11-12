# External Secrets Operator

Clavis exposes a flat resolved-secret endpoint for ESO's webhook provider:

```text
GET /api/v1/orgs/{org}/projects/{project}/environments/{env}/secrets:eso
```

The response is a plain JSON map:

```json
{ "DATABASE_URL": "postgres://…", "STRIPE_KEY": "sk_live_…" }
```

Access requires `secrets:read-values`. Create a scoped machine identity and
store its `clv_mt_…` token in a Secret:

```sh
kubectl create secret generic clavis-token \
  --from-literal=token=clv_mt_… \
  --dry-run=client -o yaml | kubectl apply -f -
kubectl label secret clavis-token external-secrets.io/type=webhook
```

## SecretStore

```yaml
apiVersion: external-secrets.io/v1
kind: SecretStore
metadata:
  name: clavis
  namespace: default
spec:
  provider:
    webhook:
      url: https://clavis.example.com/api/v1/orgs/acme/projects/web-app/environments/dev/secrets:eso
      method: GET
      result:
        jsonPath: "$"
      headers:
        Authorization: Bearer {{ .auth.token }}
      secrets:
        - name: auth
          secretRef:
            name: clavis-token
```

## ExternalSecret

Use `dataFrom.extract` to copy every key into one Kubernetes Secret:

```yaml
apiVersion: external-secrets.io/v1
kind: ExternalSecret
metadata:
  name: web-app-dev
  namespace: default
spec:
  refreshInterval: 5m
  secretStoreRef:
    name: clavis
    kind: SecretStore
  target:
    name: web-app-dev
  dataFrom:
    - extract:
        key: dev
```

`remoteRef.key` is unused by the Clavis endpoint; the project/environment are
part of the URL.

## Kubernetes service account auth

The webhook provider can send any bearer token stored in a labeled Secret.
For clusters that issue workload identity tokens, use Clavis' OIDC federation
(M3-04) to exchange a short-lived service-account token for a scoped Clavis
token, then store the result in the Secret consumed by ESO. A native ESO
provider plugin is not required for this flow.

## Notes

- The endpoint is read-only.
- File secrets are returned as strings; mount them from the resulting Secret.
- Every refresh is a reveal of all keys in the environment, recorded in audit.
