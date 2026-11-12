# M4 — Developer Magic (`v0.4`)

Goal: the features that make people choose Clavis over everything else.

## Secret schema / typed config
- [ ] **M4-01** Schema model: type (string, url, int, bool, json, enum), kind, required (per env type), pattern, description, example (M)
- [ ] **M4-02** Schema editor UI + inline validation in the grid (M)
- [ ] **M4-03** `clavis check` with clear report + CI exit code 6 (S)
- [ ] **M4-04** `clavis codegen` for TypeScript (zod), Go (struct + loader), Python (pydantic) (L)
- [ ] **M4-05** `clavis schema pull/push` (schema as code, file committed to repo) (S)
- [ ] **M4-06** Auto-infer schema from existing secrets (suggestions) (S)

## AI agents 🔒
- [ ] **M4-07** 🔒 Agent identity type (`clv_at_…`): `read-names` + `use` only, short TTL, dev/preview envs by default (M)
- [ ] **M4-08** 🔒 **Required server-side command allowlist, deny-all by default**; approval UI + `clavis agents allow` (M)
  - Acceptance: an agent cannot modify its own allowlist; unlisted commands are rejected and audited
- [ ] **M4-09** 🔒 `clavis mcp` MCP server (stdio): `list_secrets`, `check_env`, `run_with_secrets`, `request_secret` (L)
- [ ] **M4-10** Agent activity view in UI (commands run, when, which keys) (S)
- [ ] **M4-11** Docs: setup guides for Claude Code, Cursor, Codex, Windsurf, etc. + prominent limitations section (S)

## Migration importers
- [ ] **M4-12** Importer framework with dry-run preview + mapping UI (M)
- [ ] **M4-13** Infisical, Doppler importers (M)
- [ ] **M4-14** Vault/OpenBao KV v1/v2 importer (M)
- [ ] **M4-15** 1Password, AWS SM, GCP SM, dotenv-directory importers (M)

## Environment workflow
- [ ] **M4-16** Promotion workflow: diff (added / changed / missing / unchanged), select keys, schema checks, snapshot, change request for protected targets (L)
- [ ] **M4-17** Promotion UI (per mockup) + `clavis promote` with interactive picker (M)
- [ ] **M4-18** Overlays (region/tenant): data model, resolution layer, grid sub-columns, `--overlay` flag (L)
- [ ] **M4-19** Branch environments: auto-create from git branch, inheritance, inactivity GC (M)

## Secrets & CLI
- [ ] **M4-20** Secret generator: password, API key, hex, UUID, Ed25519/RSA keypair, JWT secret; UI dialog with entropy meter + `clavis secrets generate` (M)
- [ ] **M4-21** Expiry dates + reminders to owner group (30/7/1 days) via email/Slack (S)
- [ ] **M4-22** `clavis shell` (subshell with secrets, colored prompt) (S)
- [ ] **M4-23** `clavis run --watch` (restart on change via SSE) (S)
- [ ] **M4-24** 🔒 Encrypted offline cache (key in OS keychain, TTL, per-project opt-out) (M)
- [ ] **M4-25** 🔒 `clavis scan`: token-format scanning for all; real-value matching only with `read-values`, in memory; `--install-hook` (M)
- [ ] **M4-27** Vault / OpenBao sync target (KV v2; lets apps keep reading Vault during migration) (M)
- [ ] **M4-28** `clavis audit ls`, `clavis identities ls|create|revoke`, `clavis agents allow|revoke` (S)
- [ ] **M4-26** 🔒 One-time share links (client-side encryption, key in URL fragment, view limit, expiry, password) (M)
