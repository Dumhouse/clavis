# Clavis GitHub Action

Sign in to Clavis from GitHub Actions without storing a static token, then
optionally export secrets into the job environment.

The action uses GitHub's OIDC token and exchanges it with Clavis using the
workload identity created with `clavis identities create --oidc github`.

## Usage

```yaml
name: Deploy

on:
  push:
    branches: [main]

permissions:
  contents: read
  id-token: write   # required for OIDC

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - uses: clavis/action@v1
        with:
          server: https://clavis.example.com
          org: acme
          identity: github-actions

      # The action exports CLAVIS_SERVER, CLAVIS_ORG, and CLAVIS_TOKEN.
      - run: clavis export --format dotenv > .env
```

Create the trusted identity once:

```sh
clavis identities create github-actions \
  --oidc github \
  --repo acme/repo \
  --branch main \
  --role Developer \
  --project web-app \
  --identity-env dev
```

## Inputs

| Input | Required | Description |
| --- | --- | --- |
| `server` | yes | Clavis server URL |
| `org` | yes | Organization slug |
| `identity` | yes | Workload identity name |
| `audience` | no | OIDC audience, default `clavis` |
| `token` | no | Static token fallback; skips OIDC |
| `export` | no | `true` to export secrets into `GITHUB_ENV` |
| `export-project` | no | Project slug for export |
| `export-env` | no | Environment slug for export |

## Outputs

| Output | Description |
| --- | --- |
| `token` | Short-lived Clavis token |
| `expires-at` | Token expiry timestamp |
| `secrets-json` | Exported secrets as JSON when `export` is enabled |

## Export mode

`export: true` requires the `clavis` CLI and `jq` on the runner. It exports the
resolved environment as JSON, masks every value, and appends the keys to
`GITHUB_ENV` for later steps.

Prefer `clavis run -- <command>` when possible: it injects values only into the
child process, masks output, and avoids leaving secrets in the job environment.

## Security notes

- No long-lived Clavis token is stored in GitHub.
- The identity's claim conditions (repository, branch, environment) decide
  whether the exchange succeeds.
- The issued token is short-lived, scoped, and masked in logs.
- The action does not print the OIDC token or Clavis token.
