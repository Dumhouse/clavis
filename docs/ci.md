# CI and machine tokens

Local use needs only `clavis login` and `clavis init`. CI runners and other
non-interactive machines cannot complete the browser login, so they authenticate
with a token instead.

## Minimum setup

If the repository commits `.clavis.yaml` (written by `clavis init`), a job needs
only the token, plus the server URL when the instance is not on localhost:

```sh
export CLAVIS_TOKEN=clv_mt_…                       # from the CI secret store
export CLAVIS_SERVER=https://clavis.example.com    # only when not localhost
clavis run -- npm test
```

`.clavis.yaml` supplies the organization, project, and default environment.

## When to set the scope variables

Set these when there is no `.clavis.yaml`, or when a job targets a different
environment than the default:

```sh
export CLAVIS_SERVER=https://clavis.example.com
export CLAVIS_ORG=acme
export CLAVIS_PROJECT=web-app
export CLAVIS_ENV=staging
export CLAVIS_TOKEN=clv_mt_…
clavis run -- npm test
```

Precedence for every setting is: **CLI flag → environment variable →
`.clavis.yaml` → default**.

- `CLAVIS_ENV` defaults to `dev`.
- `CLAVIS_TOKEN` always wins over the token saved by `clavis login`, so unset it
  when you switch back to interactive use.
- `.clavis.yaml` is safe to commit; it never contains secrets.

## Create the token

1. Open **Agents & machines** in the web app.
2. Choose **Add agent or machine → A machine**.
3. Pick a role, project, environment glob, and expiry. Prefer the narrowest
   scope and shortest expiry that work.
4. Copy the `clv_mt_…` token once and store it in the CI secret store.

The same operation from the CLI:

```sh
clavis identities create ci-deploy --role Developer --project web-app --identity-env staging
```

Tokens are stored hashed and cannot be read again. Rotate them on the same page
or with `clavis identities` commands.

## GitHub Actions

```yaml
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4 # brings the committed .clavis.yaml
      - run: clavis run -- npm test
        env:
          CLAVIS_SERVER: https://clavis.example.com
          CLAVIS_TOKEN: ${{ secrets.CLAVIS_TOKEN }}
```

Install the Clavis binary earlier in the job. If `.clavis.yaml` is not
committed, add `CLAVIS_ORG`, `CLAVIS_PROJECT`, and `CLAVIS_ENV` to `env`.

## GitLab CI

```yaml
test:
  script:
    - clavis run -- npm test
  variables:
    CLAVIS_SERVER: https://clavis.example.com
    CLAVIS_ENV: staging
  # CLAVIS_TOKEN comes from a masked, protected CI/CD variable.
```

## Workload identity instead of a long-lived token

If the pipeline can request a short-lived OIDC token (GitHub Actions, GitLab,
Kubernetes), configure an identity with OIDC trust and exchange the platform
token for a Clavis token:

```sh
TOKEN=$(curl -sS https://clavis.example.com/api/v1/auth/oidc \
  -H 'Content-Type: application/json' \
  -d "{\"org\":\"acme\",\"identity\":\"github-actions\",\"token\":\"$OIDC_TOKEN\"}" |
  jq -r .token)
export CLAVIS_TOKEN="$TOKEN"
```

The exchange is audited and the resulting token is short-lived. See
[agents.md](agents.md) for identity setup and claim conditions, and
`clavis identities create --help` for the OIDC flags.

## Security notes

- Never commit a token or pass it as a command-line argument; use the CI secret
  store and an environment variable.
- Scope each identity to one project and the environments it needs.
- Use the shortest practical expiry and rotate on a schedule.
- Revoke the token when a pipeline or person no longer needs it.
- Prefer workload OIDC for cloud pipelines; use static machine tokens only where
  the platform cannot issue an OIDC token.
