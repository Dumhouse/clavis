# Integrations

Edition-specific glue lives here:

- `github-action/` — inject secrets into GitHub Actions runs via OIDC.
- `gitlab/` — an include file for GitLab CI (`.clavis`, `.clavis-env`).
- `github/` — a ready workflow that fails a pull request on a leaked token.
- `../.pre-commit-hooks.yaml` — the `clavis-scan` hook for pre-commit.
- `terraform-provider/` — manage projects, environments, and secrets as code.
- `vscode/` — view key names and request changes without revealing values.

Each integration is released independently but shares `pkg/client` and
`pkg/schema`. See [tasks/M3-integrations.md](../tasks/M3-integrations.md).
