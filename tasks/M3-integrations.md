# M3 — Integrations & Scale (`v0.3`)

Goal: Postgres and HA; secrets flow everywhere they're needed without long-lived credentials; shared sets, preview envs, and file secrets.

## Postgres & scale
- [x] **M3-01** Postgres dialect: sqlc queries + goose migrations; CI matrix on SQLite + Postgres (testcontainers) (L)
- [x] **M3-02** SQLite → Postgres migration tool (`clavis admin migrate-db`) (S)
- [x] **M3-03** Multi-replica support: Postgres LISTEN/NOTIFY event fan-out, `SKIP LOCKED` workers, `--role=api|worker` (M)

## Workload identity (no static tokens) 🔒
- [x] **M3-04** 🔒 OIDC federation framework: trust config per identity (issuer, audience, claim conditions) → short-lived token (L)
- [x] **M3-05** 🔒 GitHub Actions OIDC (repo/branch/environment claim conditions) (S)
- [x] **M3-06** 🔒 GitLab CI OIDC (S)
- [x] **M3-07** 🔒 Kubernetes service account token auth (TokenReview or OIDC) (M)
- [x] **M3-08** 🔒 AWS IAM auth (signed `GetCallerIdentity`) (M)
- [x] **M3-09** 🔒 GCP workload identity (S)

## Root key providers 🔒
- [x] **M3-10** 🔒 AWS KMS and GCP KMS root key providers (M)
- [x] **M3-11** 🔒 Root key rotation command (`clavis admin rotate-root-key`) (S)

## Shared sets, previews, files
- [x] **M3-12** 🔒 Shared secret sets: org-level sets with own keys and access, imported by environments, resolution layer, `${set:…}` references (L)
- [x] **M3-13** Shared sets UI with blast-radius preview before save; `clavis sets` commands (M)
- [x] **M3-14** Preview environments per PR via GitHub integration: create on open, delete on close/merge, inactivity expiry (M)
- [x] **M3-15** 🔒 File secrets (`kind=file`, ≤ 1 MiB): upload/preview in UI, `clavis run --mount` to private temp dir removed on exit (M)

## Syncs (push secrets to platforms)
- [x] **M3-16** Sync framework: provider interface, job-based execution, retries, status, error messages with fix hints, drift detection, respects freezes (L)
- [x] **M3-17** GitHub (Actions secrets/variables, environments, org-level) (M)
- [x] **M3-18** Vercel (M)
- [x] **M3-19** AWS Secrets Manager + SSM Parameter Store (M)
- [x] **M3-20** GCP Secret Manager (S)
- [x] **M3-21** Cloudflare Workers/Pages, Netlify (M)
- [x] **M3-22** Integrations gallery UI (by category, connected filter) + per-sync page: drift card with Overwrite / Import (change request) / Stop managing, failure card with fix steps, mapping, run history, behavior toggles; `clavis syncs ls|run [--dry-run]|logs` (M)
- [x] **M3-30** Clavis GitHub App (secrets write, PR events for preview environments; replaces PAT-based GitHub sync) (M)
- [x] **M3-31** GitLab CI/CD variables sync (project + group, protected/masked flags) (S)
- [x] **M3-32** Generic HTTP sync: signed POST (HMAC) of the resolved set to a user endpoint, SSRF-safe client (S)
- [x] **M3-33** First-run preview: every new sync shows the diff and requires confirmation before its first push; "delete keys not in Clavis" off by default (S)

## Kubernetes
- [ ] **M3-23** Operator: `ClavisSecret` CRD → K8s `Secret`, periodic + event-driven refresh, file secrets as keys for volume mounts (L)
- [ ] **M3-24** Operator: rolling restart of annotated Deployments on change (S)
- [ ] **M3-34** External Secrets Operator provider (Clavis as an ESO `SecretStore`, auth via K8s service account) (M)
- [ ] **M3-25** Helm chart for server (HA, external Postgres, KMS config, ServiceMonitor) (M)

## Developer integrations
- [x] **M3-26** GitHub Action `clavis/action` (OIDC login, export to env / step outputs, masking) (M)
- [x] **M3-27** Docker/Compose helper: docs + `--format docker` env-file (S)

## Notifications
- [x] **M3-28** Outgoing webhooks (HMAC-signed, retries, delivery log) (M)
- [x] **M3-29** Slack + Microsoft Teams + Discord notifications (changes in protected envs, sync failures) (M)
