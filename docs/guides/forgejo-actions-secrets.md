---
description: "Give Forgejo and Gitea Actions jobs staging or production secrets from Clavis, without copying values into repository secrets and without prefixed names."
---

# Secrets for Forgejo and Gitea Actions

GitHub has *deployment environments*: one secret name such as `DATABASE_URL`
can hold a different value for staging and for production. Forgejo and Gitea
Actions don't have them. A repository has a single set of secrets, so syncing
two Clavis environments into the same repository makes them overwrite each
other.

The fix is to let the job fetch its environment from Clavis when it runs. The
repository only stores one machine token per environment, and your workflow
says which environment it wants. Inside the job the names are the same as on
your laptop: `DATABASE_URL`, not `PRODUCTION_DATABASE_URL`.

::: tip The quick way
In the app, open **Integrations → Set up an integration → Forgejo / Gitea
Actions → Set up fetching**. Pick the environments and their branches, paste the repository URL, and
Clavis creates the tokens, stores them as repository secrets and writes the
workflow for your server (fingerprint included). The steps below do the same
by hand.
:::

Two kinds of tokens are easy to mix up:

| Token | Who uses it | Needed here? |
| --- | --- | --- |
| Clavis machine token (`clv_mt_…`) | Your Forgejo job, to read secrets from Clavis | **Yes**, one per environment, stored as a Forgejo secret |
| Forgejo access token | The Clavis server, to write secrets into Forgejo (syncs) | No, only for syncs |

## 1. Create a token per environment

In **Agents & machines** → **New machine**, or with the CLI:

```sh
clavis identities create forgejo-staging    --role Developer --project nexo --identity-env staging
clavis identities create forgejo-production --role Developer --project nexo --identity-env prod
```

Each token can read only its own environment. In Forgejo, open the repository
→ **Settings → Actions → Secrets** and add them as `CLAVIS_TOKEN_STAGING` and
`CLAVIS_TOKEN_PRODUCTION`. These two tokens are the only secrets Forgejo
stores.

## 2. Load the environment in the workflow

One load step per environment. Its `if:` picks the step for the branch that
was pushed, or for the environment chosen when you start the workflow by hand.
Any number of environments works the same way:

```yaml
# .forgejo/workflows/deploy.yaml
on:
  push:
    branches: [develop, staging, main]
  workflow_dispatch: # or run it by hand and pick the environment
    inputs:
      environment:
        type: choice
        options: [dev, staging, prod]

jobs:
  deploy:
    runs-on: docker
    steps:
      - uses: actions/checkout@v4 # brings the committed .clavis.yaml

      - name: Load dev from Clavis
        if: inputs.environment == 'dev' || (!inputs.environment && github.ref_name == 'develop')
        uses: https://github.com/dumhouse/clavis/integrations/github-action@v0.3.3
        with:
          server: https://clavis.example.com
          environment: dev
          token: ${{ secrets.CLAVIS_TOKEN_DEV }}

      - name: Load staging from Clavis
        if: inputs.environment == 'staging' || (!inputs.environment && github.ref_name == 'staging')
        uses: https://github.com/dumhouse/clavis/integrations/github-action@v0.3.3
        with:
          server: https://clavis.example.com
          environment: staging
          token: ${{ secrets.CLAVIS_TOKEN_STAGING }}

      - name: Load prod from Clavis
        if: inputs.environment == 'prod' || (!inputs.environment && github.ref_name == 'main')
        uses: https://github.com/dumhouse/clavis/integrations/github-action@v0.3.3
        with:
          server: https://clavis.example.com
          environment: prod
          token: ${{ secrets.CLAVIS_TOKEN_PRODUCTION }}

      - name: Stop when no environment matched
        if: env.CLAVIS_ENV == ''
        run: echo "::error::No Clavis environment for ${{ github.ref_name }}" && exit 1

      # Every key of that environment is now an environment variable.
      - run: bin/rails db:migrate
      - run: ./deploy.sh
```

To deploy an environment only by hand (production, say), leave its branch
out of `push.branches` and use just `inputs.environment == 'prod'` as its
condition.

The action:

- uses `clavis` from the runner image when it is there, and otherwise installs
  the CLI from GitHub releases (checksum verified);
- loads every key of the environment into the job, multi-line values (keys,
  certificates) included;
- masks secret values in the job log; *variables* stay readable;
- sets `CLAVIS_SERVER`, `CLAVIS_TOKEN` and `CLAVIS_ENV`, so later steps can also
  run `clavis run …` or `clavis export --format dotenv > .env` with the same
  environment.

### Where the runner gets the action

Your workflow always runs on Forgejo. Forgejo has no action store of its own:
a workflow names an action by its Git address, and the Clavis action is
published in the Clavis repository on GitHub (`integrations/github-action`,
versioned with Clavis releases; pin a tag, 0.3.3 or later). So you can:

| Way | `uses:` | When |
| --- | --- | --- |
| Action from GitHub | `https://github.com/dumhouse/clavis/integrations/github-action@v0.3.3` | The runner can reach github.com |
| Action from a mirror on your Forgejo | `https://git.example.com/mirrors/clavis/integrations/github-action@v0.3.3` | No internet access. Create a pull mirror of `github.com/dumhouse/clavis` once (New migration → GitHub → *This repository will be a mirror*); Forgejo keeps its tags current |
| No action | plain `run:` steps, below | You prefer a few readable lines of shell |

Without the action, each load step runs the CLI directly:

```yaml
      - name: Load staging from Clavis
        if: inputs.environment == 'staging' || (!inputs.environment && github.ref_name == 'staging')
        env:
          CLAVIS_SERVER: https://clavis.example.com
          CLAVIS_ENV: staging
          CLAVIS_TOKEN: ${{ secrets.CLAVIS_TOKEN_STAGING }}
        run: |
          if ! command -v clavis >/dev/null; then   # skip if the runner image has it
            curl -fsSL https://raw.githubusercontent.com/dumhouse/clavis/v0.3.3/scripts/install.sh \
              | CLAVIS_INSTALL_DIR="$RUNNER_TEMP/bin" bash
            export PATH="$RUNNER_TEMP/bin:$PATH"
          fi
          clavis export --format github-env
          echo "CLAVIS_ENV=$CLAVIS_ENV" >> "$GITHUB_ENV"
```

`clavis export --format github-env` does what the action does: it loads the
keys for the next steps, masks secret values, and on failure shows the cause
and fix as an error on the run page.

Without internet access the CLI download also needs a source: bake `clavis`
into the runner image, or (with the action) set `download-base` to a mirror of
the release files.

## Self-signed Clavis server

Pass the certificate fingerprint, which `docker exec clavis clavis admin tls-info`
prints. Nothing has to be installed on the runner:

```yaml
        with:
          server: https://192.168.1.20
          server-fingerprint: ${{ vars.CLAVIS_FINGERPRINT }}
```

With your own certificate authority, pass `ca-cert` instead (a file path, or
the PEM text from a secret).

## Inputs

| Input | Default | Meaning |
| --- | --- | --- |
| `server` | | Clavis URL (required) |
| `token` | | Machine token, from a repository secret |
| `environment` | | Environment to load. Empty: only sign in and set `CLAVIS_*` |
| `project` | `.clavis.yaml` | Project slug, if the repository has no `.clavis.yaml` |
| `org` | `.clavis.yaml` | Organization slug |
| `server-fingerprint` | | SHA-256 of a self-signed certificate |
| `ca-cert` | | Your CA, as a path or PEM text |
| `version` | `latest` | CLI version to install |
| `install` | `auto` | `auto` uses `clavis` from `PATH` or downloads it; `true` always downloads; `false` never does |
| `download-base` | GitHub releases | Mirror for the CLI download |

## Who can read production?

Forgejo makes every repository secret available to every workflow run in that
repository (except runs from forks). So anyone who can push a workflow
file to the repository can use `CLAVIS_TOKEN_PRODUCTION`. To limit that:

- protect `main` and the `.forgejo/workflows` folder, and limit who can push;
- keep the production token scoped to one project and the `prod` environment,
  with an expiry, and rotate it in Clavis when people leave;
- check **Audit log** in Clavis: every export names the token that read it.

## Sync or fetch?

[Syncs](../reference/syncs-create) still fit when a tool must read repository
secrets directly and can't run a step first, or when the repository only ever
needs one environment. For staging and production in one repository, fetch
at run time as shown here.
