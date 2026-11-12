# M5 — Advanced & Governance (`v0.5`)

Goal: Vault-class capabilities with Clavis-class UX, plus governance features for security teams.

## Dynamic secrets 🔒
- [ ] **M5-01** 🔒 Lease framework: create, renew, revoke, max TTL, expiry job, revoke-on-identity-delete (L)
- [ ] **M5-02** 🔒 PostgreSQL engine (role templates, revoke statements) (M)
- [ ] **M5-03** 🔒 AWS IAM / STS engine (assume-role, temporary credentials) (M)
- [ ] **M5-04** `clavis lease … -- <command>` (auto-revoke when command exits) (S)
- [ ] **M5-05** Dynamic secrets UI: configs, active leases, revoke (M)

## Rotation 🔒
- [ ] **M5-06** 🔒 Rotation framework: schedules, two-phase rotation (create new → sync → verify → revoke old), rollback on failure (L)
- [ ] **M5-07** Rotation providers: PostgreSQL passwords, AWS IAM access keys, generic webhook (M)
- [ ] **M5-08** Rotation status and reminders for manually rotated secrets (S)

## Approvals & just-in-time access 🔒
- [ ] **M5-09** 🔒 Access requests: role/env for a duration with a reason; approval policies (N approvers, specific groups) (M)
- [ ] **M5-10** 🔒 Change requests for protected envs (also used by promotion): diff, review, approve, apply (M)
- [ ] **M5-11** Requests inbox UI + email/Slack approve buttons (signed, re-auth required) (M)
- [ ] **M5-12** Break-glass access (immediate, reason required, loudly audited, admins notified) (S)

## Governance
- [ ] **M5-13** Change freezes: scheduled windows per environment, block writes/promotions/syncs, break-glass bypass, `clavis env freeze` (M)
- [ ] **M5-14** 🔒 Offboarding assistant: on member removal, revoke sessions/PATs/grants immediately; list revealed/exported secrets; guided rotate / mark-safe flow with progress (L)
- [ ] **M5-15** Access review report ("who can read prod") with CSV export (S)

## Health & insights
- [ ] **M5-16** Health jobs: stale, unused, never-rotated, expiring, no-MFA members, non-expiring tokens; **opt-in per-project duplicate detection** via fingerprint key (M)
- [ ] **M5-17** Health dashboard UI with scores and one-click fixes; `clavis health [--json]` (M)

## Ops
- [ ] **M5-18** 🔒 Optional manual unseal with Shamir shares (`clavis admin unseal`) (M)
- [ ] **M5-19** `clavis admin backup/restore` (encrypted, consistent snapshot of DB + wrapped keys) (M)
- [ ] **M5-20** Secrets Store CSI driver provider (mount secrets as files without K8s `Secret` objects) (M)
