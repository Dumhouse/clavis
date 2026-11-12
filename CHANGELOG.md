# Changelog

## v1.0.0 — unreleased

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
