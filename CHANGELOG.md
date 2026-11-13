# Changelog

## Unreleased

## v0.3.1 — 2026-11-13

Finishes the v0.3.0 release, whose run stopped at the winget step before the
Cloudsmith packages, the Docker Hub description and the Helm chart.

### Added

- The Helm chart is published to `oci://ghcr.io/dumhouse/charts/clavis` with
  every release. The chart version and `appVersion` are the release version, so
  `--version 0.3.1` installs Clavis 0.3.1. Each chart is signed with cosign,
  is only published from a release tag, and a published version is never
  overwritten. The docs site has an install, verify and upgrade guide.
- CI installs the chart into a kind cluster on every pull request.
- A manual Release backfill workflow re-runs the Cloudsmith, Docker Hub and
  Helm steps for a tag, from that tag.

### Fixed

- A failure to open the winget pull request no longer fails the release; it is
  a warning, and the pull request is opened by its own step.
- `cloudsmith-cli` is installed into a virtual environment, which newer Ubuntu
  runners require.

## v0.3.0 — 2026-11-13

### Added

- Built-in HTTPS with `CLAVIS_TLS`: `self-signed` (a certificate for the base URL
  host, renewed automatically), `file` (your certificate, reloaded when it
  changes) and `acme` (Let's Encrypt). The default stays `off` for localhost and
  reverse proxies. With TLS on, the HTTP port redirects to HTTPS and keeps
  `/healthz` and `/readyz`.
- `clavis setup-token` prints the first-run token, so
  `docker exec clavis clavis setup-token` works in the distroless image.
- The server log prints a setup link with the token built in
  (`/setup#token=…`), and the setup page fills the token from it.
- `CLAVIS_SETUP_TOKEN` presets the setup token and `CLAVIS_SETUP_OWNER_EMAIL`
  locks the owner email, for compose files and automation.
- An install wizard on the docs site writes a `compose.yaml` for localhost, a
  home lab, a public domain, or a reverse proxy.
- New docs page: HTTPS and certificates.
- First-run setup only asks for the owner account. A welcome flow after sign-in
  names the organization, creates the first project and shows how to connect
  an app for your OS (macOS, Windows, Linux) and stack (Node.js, Python, Go,
  Ruby, PHP, Java, .NET, Rust, Docker Compose, or any command). The setup API
  accepts `skip_project`, and `org_name` is now optional.
- The setup page explains where to find the token, with copy buttons, and warns
  when the connection is not encrypted. The projects page shows the four CLI
  steps (install, login, init, run) with copy buttons.

- `clavis run` no longer needs `--`: Clavis stops reading its own flags at the
  first word of your command, so `clavis run rails s -p 3001` works. The old
  `clavis run -- …` form still works.
- `run:` in `.clavis.yaml` saves a default command, so a plain `clavis run`
  starts your app. `clavis init` fills it in for npm, pnpm, yarn and bun
  scripts, Rails, Django, Laravel, Go, Rust, Maven, Gradle and Docker Compose.
- The CLI handles self-signed servers without copying certificates around: on
  first contact `clavis login` shows the SHA-256 fingerprint and asks once, like
  SSH. The answer is pinned to the server's public key, which the server keeps
  across renewals and host changes; a changed key stops the CLI with a warning
  and needs `trust` typed to accept. New `clavis trust add|ls|rm` (with
  `--fingerprint` for scripts) and the variables `CLAVIS_SERVER_FINGERPRINT`,
  `CLAVIS_CA_CERT` and `CLAVIS_TRUST_FILE`.
- `clavis admin tls-info` shows the served certificate and its fingerprints.
  On a self-signed server, the app's connect guide and Docs page show the
  same fingerprint next to `clavis login`.
- Let's Encrypt with the DNS challenge (`CLAVIS_ACME_DNS=cloudflare|duckdns`)
  gives a LAN-only server a publicly trusted certificate, wildcards included.
  A temporary certificate keeps HTTPS up until issuance, failures retry with
  backoff, renewal runs in the background, and propagation is checked on the
  zone's own nameservers (works with split DNS). `CLAVIS_ACME_DNS_TOKEN_FILE`
  reads the token from a Docker secret.
- Self-signed certificates now renew while the server runs, keeping the key.
- `clavis admin reset-password EMAIL` prints a one-time password reset link,
  for servers without email (SMTP). The reset is audited.
- A new CLI reference with a page per command (usage, flags with defaults,
  titled examples, related commands) plus pages for `.clavis.yaml`,
  environment variables, exit codes, output, offline cache, overlays and shell
  completion. It is generated from the command tree and
  `internal/cli/reference.yaml` (`make docs-cli`), and the in-app docs, the
  docs site and `clavis <command> --help` examples all use it. Tests fail when
  a command is undocumented or an example uses a flag that doesn't exist.

### Fixed

- Signing in at a plain `http://` LAN address no longer fails with "Sign in to
  continue": Clavis uses non-Secure cookies there and logs a warning.

### Changed

- Docs and examples publish the web UI on port 80 (`-p 80:8080`) and use
  `--restart unless-stopped`.

## v0.2.5 — 2026-11-13

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
- The docs site includes an OCI Helm installation and upgrade guide.

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
