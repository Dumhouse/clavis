# Roadmap

Each milestone ends in something usable and releasable. Estimates assume **2 full-time developers** and are deliberately conservative. The total to 1.0 is roughly **12–15 months**.

| # | Milestone | Goal | Est. | Release |
|---|---|---|---|---|
| M0 | [Foundation](../tasks/M0-foundation.md) | Repo, CI, crypto core, **authz + audit chain core**, skeletons (SQLite) | 3–4 wks | — |
| M1 | [Core MVP](../tasks/M1-core-mvp.md) | A small team can store and inject secrets safely | 8–10 wks | `v0.1` alpha |
| M2 | [Teams, Security & Environments](../tasks/M2-teams-security.md) | Safe for real companies: roles, SSO, MFA, env types, variables | 8–10 wks | `v0.2` beta |
| M3 | [Integrations & Scale](../tasks/M3-integrations.md) | Postgres/HA, CI/cloud/K8s, shared sets, preview envs, files | 10–12 wks | `v0.3` |
| M4 | [Developer Magic](../tasks/M4-developer-magic.md) | Schema, agents/MCP, importers, promotion, overlays | 8–10 wks | `v0.4` |
| M5 | [Advanced & Governance](../tasks/M5-advanced.md) | Dynamic secrets, rotation, JIT, approvals, offboarding, freezes | 8–10 wks | `v0.5` |
| M6 | [Hardening & Launch](../tasks/M6-launch.md) | External audit, docs, performance, public 1.0 | 6–8 wks | `v1.0` |

## Key sequencing decisions

- **Authorization and the hash-chained audit log are built in M0**, not retrofitted. Every route declares a permission from the first commit; every event is chained from the first event.
- **SQLite only until M3.** Postgres arrives together with the Helm chart and multi-replica support. The `store` interface is dialect-neutral from day one.
- **Environment safety before integrations.** Environment types and production policies (M2) exist before syncs push values anywhere (M3).

## Exit criteria

**M0:** `make dev` runs server + web with hot reload; CI green; crypto package has 100% branch coverage and fuzz tests; a route without a declared permission fails CI; audit events are hash-chained; signed snapshot builds produced.

**M1:** From an empty machine: `docker run` → setup wizard (with setup token) → project → `clavis login && clavis init && clavis run -- env` shows secrets, in **under 5 minutes**. Basic roles and invites work. Secrets grid supports edit, reveal, history, paste `.env`. Machine tokens work in CI.

**M2:** Custom roles scoped per env/path; OIDC + SAML SSO + SCIM; TOTP + passkeys; environment types with production defaults; variables vs secrets; owner/notes/change messages; inheritance, references, personal overrides; point-in-time recovery; env clone and templates.

**M3:** Postgres + multi-replica; GitHub Actions use OIDC (no stored token); syncs to GitHub, Vercel, AWS, GCP, Cloudflare, Netlify; K8s operator; Helm chart; KMS root keys; shared secret sets; preview env per PR; file secrets.

**M4:** `clavis check` / `codegen` from schema; `clavis mcp` agent mode with required allowlists; importers for Infisical, Doppler, Vault/OpenBao, 1Password, dotenv; promotion workflow; overlays; branch envs; generator; expiry reminders; offline cache; `clavis shell`; scan; share links.

**M5:** Dynamic secrets (Postgres, AWS STS); scheduled rotation; JIT access and change requests with Slack approvals; change freezes; offboarding assistant; health dashboard; optional Shamir unseal; backup/restore.

**M6:** External audit with no open critical/high findings; load test results published; docs site; migration guides; landing page; signed 1.0 release; launch.

## Cut from 1.0 (moved to backlog)

Cut to keep 1.0 achievable. Each is still planned:

- Azure: Key Vault root key provider, workload identity, Key Vault sync
- Fly.io, Railway, Render syncs
- Terraform provider
- VS Code extension
- MySQL/MariaDB dynamic engine and rotation
- Rust codegen (TS, Go, Python stay)
- PDF access reports (CSV stays)

## After 1.0 (backlog)

- Everything in "Cut from 1.0"
- Runtime SDKs (Node, Python, Go, Java, .NET) with hot reload
- "Who uses this key" analytics per service/identity
- Org-wide search across projects (names only, permission-filtered)
- Honeytokens (fake credentials that alert on use)
- Scheduled encrypted backups to S3 or another server
- Mobile-friendly approvals / mobile app
- Optional E2EE "vault mode" projects
- PKI / internal certificates, SSH certificate signing
- More dynamic engines (MongoDB, Redis, GCP service-account keys)
- Hosted cloud offering (same code, same license)
