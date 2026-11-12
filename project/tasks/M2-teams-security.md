# M2 — Teams, Security & Environments (`v0.2` beta)

Goal: safe for real companies. Everything others charge for, free. Production is safe by default.

## Access control 🔒
- [x] **M2-01** 🔒 Fine-grained permissions incl. `secrets:read-names` / `secrets:use` / `secrets:read-values` split; path-glob scopes (M)
  - Acceptance: property tests for scope matching
- [x] **M2-02** 🔒 Custom roles + visual role editor; no-privilege-escalation checks (M)
- [x] **M2-03** Groups; assign roles to groups (M)
- [x] **M2-04** Access matrix UI + "why does X have access" explainer; same logic as `clavis access explain` and `clavis members ls` (M)

## Strong authentication 🔒
- [x] **M2-05** 🔒 TOTP MFA + recovery codes (M)
- [x] **M2-06** 🔒 Passkeys/WebAuthn (login + MFA) (M)
- [x] **M2-07** 🔒 Org policy: require MFA, session lifetimes, IP allowlist (S)
- [x] **M2-08** 🔒 Re-authentication for sensitive actions (S)
- [x] **M2-09** 🔒 OIDC SSO (Google, GitHub, Microsoft, Okta, generic) + enforce-SSO option (L)
- [x] **M2-10** 🔒 SAML 2.0 SSO (L)
- [x] **M2-11** SCIM 2.0 user/group provisioning (M)
- [x] **M2-12** Optional HIBP breached-password check (k-anonymity, **off by default**) (S)

## Environments
- [x] **M2-13** 🔒 Environment types with default policies; production: protected, no overrides, reveal-with-reason, typed CLI confirmation, agents denied (M)
  - Server-side policies are enforced (protected, reveal reason, agents denied). Typed CLI confirmation is tracked separately as M2-16.
- [x] **M2-14** Protected environments (block direct writes; change requests arrive in M5, until then only Admins can write) (S)
- [x] **M2-15** UI: deviation-from-type-defaults indicator; env colors in grid, CLI, emails (S)
- [x] **M2-16** CLI production safety: banner, typed confirmation, org option to block CLI writes to prod (S)
- [x] **M2-17** Clone environment (keys only, or keys + values) (S)
- [x] **M2-18** Org environment templates applied to new projects (S)
- [x] **M2-19** Environment inheritance (parent/child resolution) (M)

## Secrets power features
- [x] **M2-20** Item kinds: **variables vs secrets** (plain display, no reveal audit, free export for variables) (M)
- [x] **M2-21** Owner group, notes/runbook, provider link per item (S)
  - Notes/runbook, provider links, and owner-group assignment are implemented.
- [x] **M2-22** Change messages on writes (UI field, `--message` in CLI, shown in history + audit) (S)
- [x] **M2-23** Secret references `${…}` with cycle detection and permission checks (M)
- [x] **M2-24** Folder imports (M)
- [x] **M2-25** Personal overrides (non-production only; grid indicator; `clavis secrets override`) (M)
- [x] **M2-26** Provenance in resolved output + UI badges (S)
- [x] **M2-27** Snapshots + point-in-time recovery UI (M)
- [x] **M2-28** Compare mode in grid (show only differences between two envs) (S)

## Audit 🔒
- [x] **M2-29** 🔒 Signed audit checkpoints (Ed25519) + `clavis admin audit verify` (M)
- [x] **M2-30** Audit exporters: webhook, syslog, S3, Datadog, Splunk HEC, Elastic/OpenSearch; JSON + CEF (M)
- [x] **M2-31** Reveal events recorded per item/version/channel (feeds offboarding in M5) (S)

## Hardening
- [x] **M2-32** 🔒 Per-identity rate limits with `RateLimit-*` headers (S)
- [x] **M2-33** 🔒 SSRF-safe outbound HTTP client (shared by OIDC discovery, webhooks, syncs) (M)
- [x] **M2-34** Personal access tokens (`clv_pat_…`) with scopes and expiry (S)
- [x] **M2-35** Security settings page (org) + user security page (MFA, passkeys, sessions, tokens) (M)
