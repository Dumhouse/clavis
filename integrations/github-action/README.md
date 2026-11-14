# Clavis action

Load secrets from a Clavis environment into a CI job. Works on GitHub,
Forgejo and Gitea Actions.

```yaml
steps:
  - uses: actions/checkout@v4 # brings the committed .clavis.yaml
  - uses: dumhouse/clavis/integrations/github-action@v0.3.3
    with:
      server: https://clavis.example.com
      token: ${{ secrets.CLAVIS_TOKEN }}
      environment: staging
  - run: ./deploy.sh # every key of "staging" is an environment variable here
```

On Forgejo and Gitea, give the full URL:
`uses: https://github.com/dumhouse/clavis/integrations/github-action@v0.3.3`.

What it does:

1. Installs the `clavis` CLI for the runner's OS and CPU and checks it against
   the release's `checksums.txt`, unless `clavis` is already on `PATH`.
2. Signs in with the `token` input, or on GitHub without a stored token
   through OIDC (`identity` input and `permissions: id-token: write`).
3. With `environment` set, runs `clavis export --format github-env`: every key
   becomes an environment variable for later steps, multi-line values
   included, and secret values are masked in the log.

It also sets `CLAVIS_SERVER`, `CLAVIS_TOKEN` and `CLAVIS_ENV` for later
steps, so `clavis run …` works there too.

## Staging and production on Forgejo

Forgejo has no deployment environments, so keep one machine token per
environment and choose at run time:

```yaml
- uses: https://github.com/dumhouse/clavis/integrations/github-action@v0.3.3
  with:
    server: https://clavis.example.com
    environment: ${{ github.ref_name == 'main' && 'prod' || 'staging' }}
    token: ${{ github.ref_name == 'main' && secrets.CLAVIS_TOKEN_PRODUCTION || secrets.CLAVIS_TOKEN_STAGING }}
```

Full guide: <https://dumhouse.github.io/clavis/guides/forgejo-actions-secrets>

## Inputs

| Input | Default | Meaning |
| --- | --- | --- |
| `server` | | Clavis URL (required) |
| `token` | | Machine token. Empty: GitHub OIDC |
| `environment` | | Environment to load. Empty: only sign in |
| `project` | `.clavis.yaml` | Project slug |
| `org` | `.clavis.yaml` | Organization slug; required for OIDC |
| `server-fingerprint` | | SHA-256 of a self-signed server certificate |
| `ca-cert` | | Your CA, as a file path or PEM text |
| `version` | `latest` | CLI version to install |
| `install` | `auto` | `auto` (if missing), `true`, `false` |
| `download-base` | GitHub releases | Mirror for runners without GitHub access |
| `identity` | | Workload identity, for GitHub OIDC |
| `audience` | `clavis` | OIDC audience |

`export`, `export-env` and `export-project` are older names for
`environment` and `project` and still work.

## When it fails

Each failure is reported as an error annotation on the run page, with the
cause and what to change:

| Error | Usual cause |
| --- | --- |
| Clavis: empty token | The secret passed as `token` is not set in this repository (check the name), or the run comes from a fork |
| Clavis: token rejected | The token is wrong, expired or revoked: rotate it in Clavis and update the secret |
| Clavis: no access to `<env>` | The token is for another environment or project: each CI token reads one environment |
| Clavis: certificate not trusted | Self-signed server without `server-fingerprint`, or a private CA without `ca-cert` |
| Clavis: server unreachable | Wrong `server`, or the runner's network can't reach it |
| Clavis: which project? | No `project` input and no `.clavis.yaml`: run `actions/checkout` first |

An environment with no values is a warning, not a failure.

## Outputs

| Output | Meaning |
| --- | --- |
| `token` | The Clavis token (masked) |
| `expires-at` | Expiry of an OIDC token; empty for machine tokens |

## OIDC on GitHub (no stored token)

```sh
clavis identities create github-actions --oidc github \
  --repo acme/repo --branch main --role Developer \
  --project web-app --identity-env prod
```

```yaml
permissions:
  contents: read
  id-token: write
steps:
  - uses: dumhouse/clavis/integrations/github-action@v0.3.3
    with:
      server: https://clavis.example.com
      org: acme
      identity: github-actions
      environment: prod
```
