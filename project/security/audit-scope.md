# External security audit scope

## In scope

- Crypto envelope and key hierarchy (root → org → project → value/blind index)
- Authentication: passwords, MFA, passkeys, sessions, SSO/OIDC/SAML, SCIM
- Authorization: scopes, glob matching, protected environments, JIT, break-glass
- Agent identities, command allowlists, MCP `run_with_secrets`
- Share links, client-side encryption, password gate, view limits
- Dynamic secrets and rotation providers
- Sync/webhook SSRF protections and credential handling
- Kubernetes operator, CSI driver, Helm chart/RBAC
- Audit chain, checkpoints, exporters
- Web UI: XSS, CSRF, CSP, clickjacking, accessibility of security controls
- Release pipeline: signing, SBOM, provenance, dependency policy

## Out of scope

- Third-party providers (AWS/GCP/Vault/etc.) themselves
- Fully compromised host or client
- Social engineering
- DoS against a single instance

## Deliverables

- Findings with severity, reproduction, impact, and fix guidance
- Retest of critical/high after fixes
- Public summary report

## Access

Provide a dedicated staging instance, synthetic data, test accounts for each
role, and a release candidate commit. No production data.
