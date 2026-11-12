# M4 — Developer Magic (`v0.4`)

Goal: the features that make people choose Clavis over everything else.

## Secret schema / typed config
- [x] **M4-01** Schema model: type (string, url, int, bool, json, enum), kind, required (per env type), pattern, description, example (M)
- [x] **M4-02** Schema editor UI + inline validation in the grid (M)
- [x] **M4-03** `clavis check` with clear report + CI exit code 6 (S)
- [x] **M4-04** `clavis codegen` for TypeScript (zod), Go (struct + loader), Python (pydantic) (L)
- [x] **M4-05** `clavis schema pull/push` (schema as code, file committed to repo) (S)
- [x] **M4-06** Auto-infer schema from existing secrets (suggestions) (S)

## AI agents 🔒
- [x] **M4-07** 🔒 Agent identity type (`clv_at_…`): `read-names` + `use` only, short TTL, dev/preview envs by default (M)
- [x] **M4-08** 🔒 **Required server-side command allowlist, deny-all by default**; approval UI + `clavis agents allow` (M)
  - Acceptance: an agent cannot modify its own allowlist; unlisted commands are rejected and audited
- [x] **M4-09** 🔒 `clavis mcp` MCP server (stdio): `list_secrets`, `check_env`, `run_with_secrets`, `request_secret` (L)
- [x] **M4-10** Agent activity view in UI (commands run, when, which keys) (S)
- [x] **M4-11** Docs: setup guides for Claude Code, Cursor, Codex, Windsurf, etc. + prominent limitations section (S)

## Migration importers
- [x] **M4-12** Importer framework with dry-run preview + mapping UI (M)
- [x] **M4-13** Infisical, Doppler importers (M)
- [x] **M4-14** Vault/OpenBao KV v1/v2 importer (M)
- [x] **M4-15** 1Password, AWS SM, GCP SM, dotenv-directory importers (M)

## Environment workflow
- [x] **M4-16** Promotion workflow: diff (added / changed / missing / unchanged), select keys, schema checks, snapshot, change request for protected targets (L)
- [x] **M4-17** Promotion UI (per mockup) + `clavis promote` with interactive picker (M)
- [x] **M4-18** Overlays (region/tenant): data model, resolution layer, grid sub-columns, `--overlay` flag (L)
- [x] **M4-19** Branch environments: auto-create from git branch, inheritance, inactivity GC (M)

## Secrets & CLI
- [x] **M4-20** Secret generator: password, API key, hex, UUID, Ed25519/RSA keypair, JWT secret; UI dialog with entropy meter + `clavis secrets generate` (M)
- [x] **M4-21** Expiry dates + reminders to owner group (30/7/1 days) via email/Slack (S)
- [x] **M4-22** `clavis shell` (subshell with secrets, colored prompt) (S)
- [x] **M4-23** `clavis run --watch` (restart on change via SSE) (S)
- [x] **M4-24** 🔒 Encrypted offline cache (key in OS keychain, TTL, per-project opt-out) (M)
- [x] **M4-25** 🔒 `clavis scan`: token-format scanning for all; real-value matching only with `read-values`, in memory; `--install-hook` (M)
- [x] **M4-27** Vault / OpenBao sync target (KV v2; lets apps keep reading Vault during migration) (M)
- [x] **M4-28** `clavis audit ls`, `clavis identities ls|create|revoke`, `clavis agents allow|revoke` (S)
- [x] **M4-26** 🔒 One-time share links (client-side encryption, key in URL fragment, view limit, expiry, password) (M)
