# Roadmap

The detailed milestone list lives in [docs/roadmap.md](docs/roadmap.md) and the
task checklists in [project/tasks/](project/tasks/).

## Now

- 1.0 hardening: external audit, load results, accessibility review.
- Migration guides and docs site.

## Done recently

- Shell completions in every package, and `clavis prompt` for your shell prompt.
- `clavis run --reveal KEY` for consoles, and a masked-output hint.
- `clavis doctor` checks consoles, the commit hook and old secrets.
- Signed releases (cosign), SBOMs and provenance; hosted apt and rpm repositories.
- Guides for GitHub, GitLab, Forgejo, CircleCI, Kubernetes and hosting platforms.
- The CLI is tested against a real server in CI, and on macOS.

## Next

- E2EE project mode (post-1.0).
- Azure Key Vault root keys.
- More rotation providers.
- Richer health automation and one-click fixes.
- Community importer/sync providers.

## Principles

- Open-source core, no feature gates.
- Deny-by-default authorization.
- No secret values in logs, audit events, or URLs.
- Agent-safe by default, with honest limitations.

## Good first issues

Issues that suit newcomers get the `good first issue` label on GitHub. Good
places to start: a guide for a platform you use, a doctor check for a problem
you hit, or an importer for another secrets tool. See
[CONTRIBUTING.md](CONTRIBUTING.md).
