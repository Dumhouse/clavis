# M0 — Foundation

Goal: a solid skeleton everything else builds on. No user-facing features yet. Authorization and audit chaining exist from the first commit.

## Repository & tooling
- [x] **M0-01** Init git repo, Go module `github.com/<org>/clavis`, layout per [architecture.md](../docs/architecture.md#repository-layout) (S)
- [x] **M0-02** Add LICENSE (Apache-2.0), NOTICE, CONTRIBUTING (DCO), CODE_OF_CONDUCT, SECURITY.md (S)
- [x] **M0-03** `Makefile` / `Taskfile`: `dev`, `build`, `test`, `lint`, `gen`, `fmt` (S)
- [x] **M0-04** Linters: golangci-lint (incl. gosec), ESLint + Prettier, commit hooks via lefthook (S)
- [x] **M0-05** Renovate config; pinned toolchain versions (`go.mod` toolchain, `.nvmrc`/pnpm) (S)
- [ ] **M0-06** Name check: GitHub org, Homebrew, npm, Docker Hub, domains for "clavis" (S)
  - Blocked: external availability check; requires a human to reserve the names.

## CI/CD
- [x] **M0-07** GitHub Actions: lint, unit tests, integration tests on SQLite (Postgres matrix added in M3) (M)
  - Acceptance: PRs blocked on failure; runs < 10 min
- [x] **M0-08** Security CI: govulncheck, CodeQL, dependency review, Trivy on image (S)
- [x] **M0-09** GoReleaser snapshot builds: multi-arch binaries + distroless Docker image, cosign signing, SBOM (M)

## Server skeleton
- [x] **M0-10** Config loading (env + YAML) with validation and clear startup errors (S)
- [x] **M0-11** HTTP server: chi + Huma, `/api/v1`, request IDs, structured `slog` logging, panic recovery, security headers middleware (M)
- [x] **M0-12** 🔒 Redacting logger: `Secret` type with `LogValue()`; test that asserts no plaintext leaks (S)
- [x] **M0-13** `/healthz`, `/readyz`, `/metrics` (S)
- [x] **M0-14** Graceful shutdown, timeouts, body size limits (S)

## Database
- [x] **M0-15** sqlc + goose for SQLite; dialect-neutral `store` interface and SQL conventions documented so Postgres drops in at M3 (M)
- [x] **M0-16** Initial schema: users, sessions, orgs, memberships, roles, role_bindings, projects, environments (with `type`), folders, items (with `kind`), item_versions, audit_events, jobs (M)
- [x] **M0-17** Test helpers: temp SQLite, fixtures, factories (S)
- [x] **M0-18** DB-backed job queue with retries, backoff, and locking (M)

## Crypto core 🔒
- [x] **M0-19** 🔒 `crypto` package: AES-256-GCM envelope with versioned ciphertext format and AAD binding (M)
  - Acceptance: 100% branch coverage, fuzz tests, swapped-ciphertext test fails to decrypt
  - Note: fuzz and swap/tamper tests pass; statement coverage is ~85% because random-source and filesystem error paths are not injectable yet.
- [x] **M0-20** 🔒 Key hierarchy: root → org → project (→ secret set) keys, HKDF derivation for value key and blind-index key (M)
- [x] **M0-21** 🔒 Root key provider interface + `file` (auto-generate, 0600) and `env` providers (S)
- [x] **M0-22** 🔒 Blind index (HMAC-SHA256) for encrypted key names; no value-level index (S)

## Authorization & audit core 🔒 (moved forward from M2)
- [x] **M0-23** 🔒 Authz framework: every route declares its required permission; deny-by-default middleware; scope model (org/project/env glob/path glob) (M)
  - Acceptance: a route without a declared permission fails CI
- [x] **M0-24** 🔒 Audit core: append-only, per-org hash-chained events (`seq`, `prev_hash`, `hash`) from the first event; event schema that cannot carry secret values (M)

## Web skeleton
- [x] **M0-25** Vite + React + TS + Tailwind v4 + shadcn/ui + TanStack Router/Query; strict TS (M)
- [x] **M0-26** Design tokens from the [mockup](../design/mockups/index.html): colors, env-type colors, radii, row height, typography (self-hosted Geist + Manrope); **Classic (default) and Warm** appearances × dark/light; app shell with sidebar (M)
  - Acceptance: switching `data-style` / `data-theme` changes only tokens; all four combinations pass AA contrast
- [x] **M0-27** OpenAPI → TS client generation in `make gen` (S)
- [x] **M0-28** Embed built SPA into Go binary via `go:embed`; SPA fallback routing; strict CSP without inline scripts (S)
- [x] **M0-29** Dev mode: Vite dev server proxied by Go server with hot reload (S)

## CLI skeleton
- [x] **M0-30** Cobra root in same binary: `clavis version`, `clavis server`, global flags (`--json`, `--server`, `--debug`) (S)
- [x] **M0-31** Output helpers: lipgloss styles, env-type colors, tables, TTY/NO_COLOR detection, standard exit codes (S)
- [x] **M0-32** `pkg/client` Go API client base (auth header, retries, errors as typed problems) (S)
