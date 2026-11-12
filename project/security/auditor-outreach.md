# Auditor outreach

## Shortlist criteria

- Go and TypeScript review experience.
- Cryptographic protocol / key-management experience.
- Kubernetes and cloud identity experience.
- Prior secrets-manager, password-manager, or PKI work.
- Retest included.
- Public summary report allowed.

## Outreach email

Subject: Security review for Clavis, an open-source secrets manager

Hello,

We are preparing Clavis 1.0, an open-source secrets manager written in Go with
a React UI, SQLite/Postgres storage, Kubernetes operator, CSI driver, and
agent/MCP workflows. We are looking for a focused security review before public
launch.

Scope: crypto envelope/key hierarchy; authentication (passwords, MFA, passkeys,
OIDC/SAML/SCIM); authorization scopes; protected environments/JIT/approvals;
agent command allowlists and MCP; share links; dynamic secrets and rotation;
sync/webhook SSRF; audit chain; operator/CSI/Helm; release pipeline.

Proposed engagement: 2-week focused review or 3–4 week full review, with a
retest of critical/high findings and a short public summary.

We can provide a private repo, staging instance, synthetic data, and test
accounts for each role.

Could you share availability, pricing, and a sample report?

Thanks,
Clavis maintainers

## Evaluation matrix

| Firm | Crypto | Go | K8s/cloud | Secrets/PKI | Retest | Report | Price | Dates |
|---|---|---|---|---|---|---|---|---|
| | | | | | | | | |

## Decision notes

- Prefer a firm that will read code, not only run scanners.
- Require concrete reproduction and exploitability, not only best-practice notes.
- Reserve one maintainer week for fixes and one for retest coordination.
