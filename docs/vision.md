# Vision & Competitive Analysis

## One-liner

**Clavis is the open-source secrets manager for modern teams and their AI agents: beautiful, secure, and free.**

## Target users

1. **Primary: small and mid-size product teams (2–200 engineers)** who outgrew `.env` files in Slack, want Doppler/Infisical-level UX, and don't want to pay per seat or run Vault.
2. **Secondary: platform/security teams** at larger companies who need SSO, RBAC, audit, and rotation, and who were told those features are "enterprise only".
3. **Tertiary: solo developers and homelabbers** who want a nice self-hosted home for their secrets.

## Competitive landscape

| Tool | License / model | Strengths | Weaknesses |
|---|---|---|---|
| **Infisical** | MIT core + paid enterprise | Best-in-class developer experience among open tools, many integrations | SSO/SAML, advanced RBAC, rotation, approvals, audit retention are paid; heavy stack (Node + Postgres + Redis) |
| **Doppler** | Closed SaaS | Very polished; branch configs, personal configs, references | Not self-hostable; per-seat pricing |
| **HashiCorp Vault** | BSL 1.1 (not OSI open source) | Dynamic secrets, PKI, transit, the most capabilities | Hard to operate; unseal step; HCL policies; developer UX is poor |
| **OpenBao** | MPL-2.0 (Vault fork) | Vault's power, truly open | Same operational complexity as Vault |
| **Phase** | Open core | E2EE, nice UI | Key features paid; E2EE limits server-side integrations |
| **EnvKey** | MIT | E2EE, solid model | Low momentum, dated UX |
| **Bitwarden Secrets Manager** | Mixed / source-available parts | Trusted brand | Thin feature set; tied to the Bitwarden ecosystem |
| **1Password Secrets Automation** | Closed | `op run`, great UX, already widely used | Expensive; not designed for infrastructure |
| **Pulumi ESC** | Closed SaaS | Composable environments, dynamic cloud credentials via OIDC | Tied to Pulumi |
| **CyberArk Conjur OSS** | LGPL | Enterprise-grade | Clunky; enterprise-oriented |
| **SOPS / dotenvx / git-crypt** | Open source | Simple, git-native | No UI, RBAC, audit, or revocation; key distribution is manual |
| **AWS / GCP / Azure secret stores** | Cloud services | Native IAM, rotation | One cloud only; poor developer UX; no multi-env view |

### The gap

Nobody combines **Doppler/Infisical polish + Vault-class capabilities + a fully open license + trivial self-hosting**. That is Clavis.

## What we borrow (and do better)

| From | Feature | Clavis twist |
|---|---|---|
| Doppler | Branch configs, personal overrides, secret references | Plus auto-mapping from git branches |
| Infisical | Environment comparison grid, share links, point-in-time recovery, K8s operator | Grid is spreadsheet-grade: paste a `.env`, keyboard-driven, inline diff |
| Vault / OpenBao | Dynamic secrets, leases, policies | Policies edited in a visual editor; no HCL |
| 1Password | `op run` with output masking | Masking on by default; also for AI agents |
| Pulumi ESC | Composable envs, OIDC cloud credentials | Built into CI integrations from day one |
| SOPS / dotenvx | Git-friendly encrypted export | Used for backups, GitOps, and disaster recovery |

## Differentiators (our identity)

1. **Everything free.** SSO, SAML, SCIM, RBAC, audit export, rotation, approvals, dynamic secrets. No license keys, no feature flags by tier.
2. **Secret schema / typed config.** Each key can declare type, required-ness, validation regex, description, and example. `clavis check` fails CI if prod is missing a required key; `clavis codegen` emits typed config for TS/Go/Python/Rust.
3. **Agent-safe access (AI-native).** An MCP server plus "use without reveal" mode: an AI coding agent can list key *names* and run **human-approved commands** (`npm test`) with secrets injected, but cannot read values through Clavis. `clavis mcp --agent` starts a one-hour session with the person's own login and keeps it in memory, so the MCP config holds no token. Access is scoped to non-production envs by default, expiring, and audited. We are explicit that this holds when the agent's shell is sandboxed (an agent with an unrestricted shell as the user can act as the user), and that a command receiving secrets can still leak them; masking is a convenience, and dynamic credentials are the strong option.
4. **Five-minute self-host.** Single binary with embedded UI, SQLite by default, browser setup wizard, auto-unseal by default.
5. **Migration importers.** `clavis import --from infisical|doppler|vault|1password|dotenv|aws`. This lowers switching cost to near zero.
6. **Secret health.** Stale, unused, duplicated, never-rotated, and leaked secrets surfaced on a dashboard.
7. **Branch environments.** Working on git branch `feat/x` automatically resolves to `dev/feat-x`, which inherits from `dev`.
8. **Leak protection.** A pre-commit `clavis scan` that knows the *actual* values, not just regexes.
9. **Just-in-time production access.** Request access, approve in Slack/UI, and access expires automatically.
10. **Offline CLI.** An encrypted local cache lets `clavis run` work offline or during an outage.
11. **Shared secret sets.** Org-level sets (`stripe`, `sentry`) imported by many projects. Update once, applied everywhere, with a blast-radius preview.
12. **Promotion workflow.** Move config `dev → staging → prod` with a diff, schema checks, and approvals, just like code.
13. **Environment types with policies.** Production is safe by default (protected, approvals, no overrides, red in the CLI) without any configuration.
14. **Offboarding assistant.** When someone leaves, see exactly which secrets they revealed and rotate them in a guided flow.
15. **Variables vs secrets vs files.** Non-sensitive config stays readable; certificates and JSON credentials are first-class.

## Non-goals (for 1.0)

- **Not a password manager** for humans (use Bitwarden/1Password).
- **No PKI / certificate authority** before 1.0. Possibly later.
- **No end-to-end encryption by default.** Server-side envelope encryption enables integrations, rotation, and dynamic secrets. An optional per-project E2EE "vault mode" may come after 1.0 (see [security.md](security.md#future-optional-e2ee-projects)).
- **No hosted SaaS at launch.** Self-host first, but the design stays multi-tenant-safe so a hosted offering is possible later.

## Success metrics

- Time from `docker run` to first secret injected via CLI: **< 5 minutes**.
- Time from `clavis import --from infisical` to a working project: **< 2 minutes**.
- Zero critical findings in the pre-1.0 external security audit.
- GitHub stars / Docker pulls / active installs (opt-in anonymous telemetry, off by default).
