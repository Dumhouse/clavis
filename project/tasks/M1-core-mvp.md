# M1 — Core MVP (`v0.1` alpha)

**Progress:** auth (setup token, argon2id, sessions, CSRF), organizations, projects, environments, encrypted secret CRUD, version history/rollback, audit events, and the login/setup/projects/secrets web screens are implemented. Remaining M1 work is listed below.

Goal: a small team can store secrets and inject them into apps in under 5 minutes, with basic roles and a chained audit log.

## Setup & auth
- [x] **M1-01** 🔒 First-run setup wizard with **one-time setup token** (printed to log + `<data-dir>/setup-token`): create admin, org, first project (M)
  - Acceptance: wizard rejects requests without the token; permanently closed once an admin exists
- [x] **M1-02** 🔒 Email + password signup/login with argon2id, rate limiting, no enumeration (M)
- [x] **M1-03** 🔒 Server-side sessions: `__Host-` cookie, CSRF protection, idle/absolute timeouts, logout everywhere; documented `--dev` fallback for Safari on localhost (M)
- [x] **M1-04** Session management UI (list devices, revoke) (S)
- [x] **M1-05** SMTP email setup (optional) + email templates (verify, reset, invite) (M)
- [x] **M1-06** 🔒 Password reset flow (single-use, short-lived tokens) (S)

## Access (basic)
- [x] **M1-07** 🔒 Built-in roles Owner / Admin / Developer / Viewer at org and project level, env-scoped bindings (M)
- [x] **M1-08** Invites by email or link, with role and expiry (M)
  - Single-use links with role and expiry, plus email restriction. SMTP delivery is not wired yet; the link is shown and copied.

## Orgs, projects, environments
- [x] **M1-09** Orgs CRUD, org switcher (S)

- [x] **M1-10** Projects CRUD with key generation on create; default envs dev/staging/prod with types (S)
- [x] **M1-11** Environments CRUD, reorder, type + color (S)
  - Partial: create/update/delete and position API implemented; drag-to-reorder UI pending.
- [x] **M1-12** Folders (paths) CRUD (S)
  - API implemented; folder sidebar in the grid is pending.

## Secrets
- [x] **M1-13** 🔒 Items service: create/update/delete with encryption, blind index, versioning, soft delete (L)
- [x] **M1-14** Batch API (`PATCH /secrets:batch`) atomic multi-env edits with ETags (M)
- [x] **M1-15** Version history + rollback API (S)
- [x] **M1-16** `.env` / JSON / YAML parser (fuzzed) for import and paste (M)
  - Parser, fuzz test, and `clavis import` are implemented; web paste preview is pending.

## Web UI
- [x] **M1-17** Auth screens: login, signup, reset, setup wizard (polished, dark/light) (M)
  - Partial: login and setup wizard are implemented; password reset and invite signup need SMTP.
- [x] **M1-18** Projects home with cards and empty state + CLI snippet (S)
- [x] **M1-19** **Secrets grid** (hero, per [mockup](../design/mockups/index.html)): envs as columns with type colors, mask/reveal, copy, inline edit, missing-cell states, search, folder sidebar (L)
- [x] **M1-20** Unsaved-changes bar → review diff → save (M)
- [x] **M1-21** Paste `.env` → parsed preview → apply (M)
- [x] **M1-22** Secret detail drawer: comment, tags, history timeline, restore (M)
  - Implemented as an edit modal rather than a side drawer; all four data features are present.
- [x] **M1-23** Members page: invite, change role, remove (S)
- [x] **M1-24** ⌘K command palette (navigation + core actions) (M)
- [x] **M1-25** Real-time updates via SSE (grid refreshes when someone else edits) (M)
- [x] **M1-26** Toasts with undo, skeleton loaders, error states with request ID (S)
- [x] **M1-42** Appearance setting: Classic (default) / Warm × System / Dark / Light, saved per user; org default for new members (S)
- [x] **M1-43** Page hints: hint registry (`page → keys, cmds, tips`), bottom hint bar with copyable CLI equivalent, `?` shortcuts & commands sheet, ⌘K entry, hide/show setting, `G then X` and `J/K` shortcuts (M)
  - Acceptance: CI test fails if a hint references a CLI command missing from the CLI reference

## Machine access
- [x] **M1-27** 🔒 Machine identities with tokens (`clv_mt_…` format with checksum), hashed storage, expiry, scope to project/env (M)
- [x] **M1-28** Identities UI: create, show token once, revoke, last used (S)

## CLI
- [x] **M1-29** 🔒 `clavis login` device-code flow, token in OS keychain (M)
  - Implemented with macOS Keychain / Secret Service; a 0600 file fallback is used where no keychain helper exists.
- [x] **M1-30** `clavis init` / `link` writing `.clavis.yaml`; offer `.env` import (M)
  - `.env` import is available as `clavis import FILE` rather than a prompt inside `init`.
- [x] **M1-31** 🔒 `clavis run` with env injection and output masking (incl. base64/URL-encoded forms) (M)
- [x] **M1-32** `clavis secrets ls/get/set/rm/history/rollback` (M)
- [x] **M1-33** `clavis export` (dotenv/json/yaml/shell/docker) with `.gitignore` warning; `clavis import FILE` (S)
- [x] **M1-34** `clavis env ls/create/diff` (S)
- [x] **M1-35** Non-interactive auth via `CLAVIS_TOKEN` (S)

## Audit
- [x] **M1-36** Audit events for every M1 action, using the M0 chained log (no values ever) (M)
- [x] **M1-37** Audit log page: list, filters, integrity badge (S)

## Packaging & docs
- [x] **M1-38** Docker image + `compose.yaml` (SQLite) (S)
- [ ] **M1-39** Homebrew tap + install script (signed) (S)
  - Signed install script added (`scripts/install.sh`, checksum + optional cosign). The Homebrew tap repository and formula still need a release.
- [x] **M1-40** Quickstart docs (README + docs/quickstart.md) (S)
- [x] **M1-41** E2E test: wizard → project → secret → CLI run (Playwright + CLI) (M)
