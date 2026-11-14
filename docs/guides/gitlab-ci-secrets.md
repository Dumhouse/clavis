---
description: "Give GitLab CI jobs staging or production secrets from Clavis with a ready include file: one masked token per environment, the same secret names in every job."
---

# Secrets for GitLab CI

A GitLab job fetches its environment from Clavis when it runs. The project
stores only a machine token per environment, and nothing is copied into
GitLab. Inside the job the names are the same as on your laptop:
`DATABASE_URL`, not `PRODUCTION_DATABASE_URL`.

::: tip The quick way
In the app, open **Integrations → Set up an integration → GitLab → Set up
fetching**. Clavis creates the tokens and writes the `.gitlab-ci.yml` for your
server. The steps below do the same by hand, with a shared include file.
:::

## 1. Create a token per environment

```sh
clavis identities create gitlab-staging --type machine --project web-app --identity-env staging
```

In GitLab, open **Settings → CI/CD → Variables** and add:

| Variable | Value | Flags |
| --- | --- | --- |
| `CLAVIS_TOKEN_STAGING` | the `clv_mt_…` token | **Masked**, **Protected** |
| `CLAVIS_SERVER` | `https://clavis.example.com` | |
| `CLAVIS_SERVER_FINGERPRINT` | from `clavis admin tls-info` | only for a self-signed server |

Mark production's token **Protected** and the `main` branch protected, so only
pipelines on protected branches can read it.

## 2. Include the template

```yaml
include:
  - remote: https://raw.githubusercontent.com/dumhouse/clavis/main/integrations/gitlab/clavis.gitlab-ci.yml

deploy-staging:
  extends: .clavis
  rules:
    - if: $CI_COMMIT_BRANCH == "develop"
  variables:
    CLAVIS_ENV: staging
    CLAVIS_TOKEN: $CLAVIS_TOKEN_STAGING
  script:
    - clavis run ./deploy.sh

deploy-production:
  extends: .clavis
  rules:
    - if: $CI_COMMIT_BRANCH == "main"
  variables:
    CLAVIS_ENV: production
    CLAVIS_TOKEN: $CLAVIS_TOKEN_PRODUCTION
  script:
    - clavis run ./deploy.sh
```

`.clavis` installs the CLI if the image doesn't have it (checksum-verified by
the install script), prints its version, and leaves `clavis` on `PATH`. Set
`CLAVIS_VERSION: 0.3.3` in `variables` to pin a release, and replace `main` in
the include URL with a release tag for a fully pinned pipeline.

To load every value as environment variables for the whole job instead of
wrapping one command, extend `.clavis-env`.

## Which one: `clavis run` or a sync?

`clavis run` fetches at run time, shows every read in the audit log, and works
with staging and production in one project. A [sync](./sync-connections) still
fits when a tool must read GitLab variables directly.
