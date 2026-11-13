---
description: "Use Clavis secrets in GitHub Actions with a scoped machine token or short-lived OIDC exchange, without copying values into repository secrets."
---

# Secrets for GitHub Actions

You can use Clavis from a workflow in two ways: a scoped machine token stored
as one GitHub secret, or a short-lived token exchanged from GitHub's OIDC
identity so nothing long-lived is stored at all.

## Option 1: a machine token

Create the token in **Agents & machines**, or with the CLI:

```sh
clavis identities create ci-deploy --role Developer --project web-app --identity-env staging
```

Store it as the repository secret `CLAVIS_TOKEN`, then:

```yaml
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4 # brings the committed .clavis.yaml
      - run: clavis run npm test
        env:
          CLAVIS_SERVER: https://clavis.example.com
          CLAVIS_TOKEN: ${{ secrets.CLAVIS_TOKEN }}
```

Install the `clavis` binary earlier in the job. Scope the identity to one
project and the environments it needs, with the shortest expiry that works.

## Option 2: OIDC, no stored token

Configure an identity with OIDC trust for your repository, then exchange the
job's OIDC token for a short-lived Clavis token. The exchange is audited. The
full request and claim conditions are in [CI and machine tokens](../ci).

## Pushing values to GitHub instead

If a workflow needs values as native GitHub secrets, create a GitHub sync in
Clavis. It writes repository, environment, or organization secrets and variables
and reports drift. The sync is one-way, so Clavis stays the source of truth.

## Fail the build on missing config

```yaml
- run: clavis check --env production
```
