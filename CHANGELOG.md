# Changelog

## v0.2.5 — unreleased

Includes everything that v0.2.3 and v0.2.4 were meant to ship.


### Added

- Releases upload `.deb` and `.rpm` packages to Cloudsmith, so Debian, Ubuntu,
  Fedora and RHEL systems can install from an apt or rpm repository.
- A post-release check pulls and runs both container images and confirms the
  release assets, Homebrew cask and Scoop manifest list the new version.
- Releases open a pull request to `microsoft/winget-pkgs`, so Windows users can
  install with `winget install Dumhouse.Clavis` once the package is accepted.
- Release notes open with a download table by operating system and CPU, then
  install commands, and explain which files are only for verification.
- A Download page on the docs site builds the same table from the latest release.
- Releases sign archives and `checksums.txt` only, so a release has about 27
  files instead of 39. Packages and SBOMs are verified through the signed
  checksums.
- Publishing a release now waits for a maintainer's approval, and release tags
  are protected from being moved or deleted.
- `SUPPORT.md`, a pull request template, an OpenSSF Scorecard workflow, and real
  security and conduct contact addresses.
- The container image carries OCI title, description, documentation and vendor
  labels, and its Docker Hub overview is published on every release.

### Fixed

- Publishing the Homebrew, Scoop and winget updates failed in v0.2.3 because of
  how the repository tokens were written in the release configuration.
- The winget submission is off until `WINGET_ENABLED` is set, so a problem with
  it can no longer stop a release before the later steps run.
- GoReleaser no longer fails a local snapshot when the Homebrew or Scoop token
  is unset.
- `deploy/compose.yaml` pulls `ghcr.io/dumhouse/clavis` instead of an image
  name that does not exist.

## v0.2.4 — 2026-11-13

Incomplete release: the winget submission failed and stopped publishing before the
Cloudsmith upload, Docker Hub description and verification. Use v0.2.5.

## v0.2.3 — 2026-11-13

Incomplete release: publishing stopped partway. Use v0.2.5.

## v0.2.2 — 2026-11-13

### Added

- Container image published to Docker Hub (`dumhouse/clavis`) as well as
  `ghcr.io/dumhouse/clavis`, as one multi-arch image.
- Homebrew install through a cask: `brew install dumhouse/tap/clavis`. It
  replaces the formula published in v0.2.1.
- Releasing guide covering tags, tokens and each distribution channel.

### Changed

- Release configuration moved to GoReleaser's `dockers_v2` and
  `homebrew_casks`.

## v0.2.1 — 2026-11-13

### Added

- `.deb`, `.rpm` and `.apk` packages attached to every release.
- Scoop bucket for Windows: `scoop bucket add dumhouse https://github.com/Dumhouse/scoop-bucket`.
- Homebrew tap with a formula (replaced by a cask in v0.2.2).
- Helm chart metadata for Artifact Hub, and a workflow that publishes the chart
  to `oci://ghcr.io/dumhouse/charts`.
- Documentation: guides for replacing `.env` files, GitHub Actions and coding
  agents; comparison pages for Vault, Infisical and Doppler; a Changelog page; a
  custom 404 page; per-page meta descriptions.

### Fixed

- Install documentation described Homebrew, Scoop, winget, apt and a Docker Hub
  image that did not exist yet; it now describes what ships.
- The account menu and appearance switch now include their visible text in their
  accessible names.
- A malformed SAML response now fails the login instead of risking a panic in the
  response parser.

## v0.2.0 — 2026-11-13

### Added

- Secrets manager with SQLite/Postgres, embedded React UI, and single binary.
- Typed schema, validation, `clavis check`, `schema pull/push/infer`, and
  TypeScript/Go/Python codegen.
- Dynamic PostgreSQL and AWS STS credentials with leases and revocation.
- Rotation policies for PostgreSQL, AWS IAM, and webhooks.
- JIT access requests, multi-approver email/Slack links, break-glass, change
  freezes, and access reviews.
- AI agent identities, server-side command allowlists, `clavis mcp`, and the
  Identities activity view.
- Forgejo Actions sync (Codeberg, self-hosted Forgejo, and Gitea) for
  repository and organization secrets and variables, with drift checks.
- `clavis mcp --agent NAME`: a one-hour agent session started with your own
  login and kept in memory, so MCP configs hold no token. Logging out or
  revoking the parent token ends it.
- Client-side encrypted share links, offline encrypted cache, scan, export,
  importers for Infisical/Doppler/Vault/1Password/AWS/GCP.
- Kubernetes operator, Helm chart, External Secrets webhook integration, and
  Secrets Store CSI driver.
- Audit hash chain, signed checkpoints, exporters, health dashboard, offboarding
  assistant, Shamir unseal, and encrypted backup/restore.

### Security

- Argon2id passwords, TOTP/passkeys, OIDC/SAML/SCIM, scoped tokens, OIDC/AWS/GCP
  workload federation.
- AES-256-GCM envelope encryption, encrypted key names, blind indexes, CSRF,
  CSP, SSRF guard, audit redaction.
- cosign-signed releases, SBOMs, SLSA provenance workflow, fuzz targets.

## v0.4.0

- Developer magic: schema, checks, codegen, agents, MCP, overlays, branch
  environments, generator, expiry, shell, watch, cache, scan, share links,
  Vault sync.

## v0.3.0

- Postgres, multi-replica, workload identity, KMS, shared sets, previews, syncs,
  operator, ESO.

## v0.2.0

- Teams, RBAC, audit, MFA/passkeys, sessions, protected environments, personal
  overrides.

## v0.1.0

- Core secrets, environments, encryption, CLI, web UI.
