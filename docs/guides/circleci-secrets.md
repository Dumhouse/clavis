---
description: "Give CircleCI jobs staging or production secrets from Clavis: one machine token per environment, installed and used with clavis run."
---

# Secrets for CircleCI

A CircleCI job fetches its environment from Clavis when it runs. CircleCI
stores only a machine token; the secrets themselves are never copied into
CircleCI, and inside the job they keep the names they have on your laptop.

## 1. Create a token per environment

```sh
clavis identities create circleci-staging --type machine --project web-app --identity-env staging
```

In CircleCI, add the token to a [context](https://circleci.com/docs/contexts/)
(or a project's environment variables), so only the jobs you choose can read
it:

| Variable | Value |
| --- | --- |
| `CLAVIS_TOKEN` | the `clv_mt_…` token |
| `CLAVIS_SERVER` | `https://clavis.example.com` |
| `CLAVIS_SERVER_FINGERPRINT` | from `clavis admin tls-info`, only for a self-signed server |

Use a separate context for production and restrict it to the people and
branches that may deploy.

## 2. Use it in a job

```yaml
version: 2.1

jobs:
  deploy:
    docker:
      - image: cimg/base:stable
    environment:
      CLAVIS_ORG: acme
      CLAVIS_PROJECT: web-app
      CLAVIS_ENV: staging
    steps:
      - checkout
      - run:
          name: Install Clavis
          command: |
            curl -fsSL https://raw.githubusercontent.com/dumhouse/clavis/main/scripts/install.sh \
              | CLAVIS_INSTALL_DIR="$HOME/bin" CLAVIS_VERSION=0.3.3 bash
            echo 'export PATH="$HOME/bin:$PATH"' >> "$BASH_ENV"
      - run:
          name: Deploy
          command: clavis run ./deploy.sh

workflows:
  deploy:
    jobs:
      - deploy:
          context: clavis-staging
```

The install script checks the download's checksum (and its cosign signature
when `cosign` is installed). Pin `CLAVIS_VERSION` to a release for
reproducible builds.

## Things to know

- `clavis run` masks secret values in the job log. Masking is a safety net,
  not a boundary: do not `echo` secrets on purpose.
- To make every value available to later steps, add
  `eval "$(clavis export --format shell)"` instead of wrapping each command.
  CircleCI does not mask values that way, so prefer `clavis run`.
- Run `clavis doctor` in a step while you set this up. It tells you whether
  the token, server and project are reachable.
