# Internal threat-model review

Date: 2026-11-12
Scope: server, CLI, web UI, operator, CSI driver, sync/webhook integrations,
release pipeline.
Reference: [security.md](security.md).

## Method

1. Re-read the asset inventory and adversary table.
2. Traced each asset through authentication, authorization, encryption, audit,
   and deletion paths.
3. Compared the documented behavior with the implementation and tests.
4. Ran `go test ./...`, Postgres integration tests, `govulncheck`, CodeQL,
   Trivy, and the fuzz smoke targets.

## Findings closed in this review

| Finding | Fix |
|---|---|
| Security doc claimed agent production `use` was policy-gated, but the implementation hard-denies staging and production. | Documentation now matches the stricter behavior. |
| Security doc claimed all protected writes need an approved change request, while Admin/Owner writes are allowed. | Documented the actual Admin/Owner exception. |
| Root-key provider list mentioned Azure Key Vault, which is not implemented. | Documented `file`, `env`, `aws-kms`, `gcp-kms`, and Shamir sealing only. |
| Share-link password handling was underspecified. | Documented ciphertext-only storage and bcrypt password hashing. |
| Fuzzing was described but had no committed targets or CI smoke pass. | Added token, dotenv, and reference-expansion fuzz targets plus CI. |
| Supply-chain claims were not backed by a verification guide. | Added supply-chain and reproducible-build documentation. |
| Offboarding did not revoke personal tokens. | Member removal now deletes personal tokens before offboarding tasks are reviewed. |
| Dynamic credentials and rotation introduced new secret-bearing configs. | Configs are encrypted with org keys; leases and rotated values are audited. |

## Remaining risks and owners

| Risk | Severity | Status | Mitigation |
|---|---|---|---|
| External audit has not been performed. | High | Open | Schedule before 1.0; publish findings. |
| A host-level compromise can read decrypted values in memory. | High | Accepted | OS hardening, KMS, short-lived dynamic secrets, no core dumps. |
| Allowed agent commands can intentionally exfiltrate values. | High | Accepted | Documented limitation; prefer dynamic secrets for agent workflows. |
| Postgres RLS is not used; authorization is application-side. | Medium | Accepted | Deny-by-default engine, scoped queries, tests. |
| Root key backup is an operator responsibility. | Medium | Accepted | Backup command warns; Shamir sealing available. |
| External secret-scanning partner registration is pending. | Medium | Open | Token prefix and checksum are ready. |
| Release provenance/SLSA workflow needs repository settings. | Medium | Open | Release workflow signs and emits SBOMs; SLSA generator wiring documented. |

## Verification checklist

- [x] Every route declares a permission (startup registry validation).
- [x] Secret values never appear in logs or audit events.
- [x] Key names are encrypted and blind-indexed.
- [x] Root key is not stored in the database.
- [x] Session, PAT, identity-token, and lease expiry paths tested.
- [x] CSRF, CSP, HSTS, body limits, and SSRF guard present.
- [x] Audit hash chain and signed checkpoints verified.
- [x] Agent staging/production denial tested.
- [x] Share links are ciphertext-only with view/expiry/password controls.
- [x] Backup encryption and Shamir seal/unseal tested.
- [x] Fuzz targets run in CI.
- [ ] External audit and disclosure program publication.
