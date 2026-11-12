# Security Design

Security is the product. This document is the reference for every security-relevant decision. Changes to it require review from two maintainers.

## 1. Threat model

### Assets
- Secret values, secret key names, and comments
- Root key and wrapped org/project keys
- User credentials, sessions, access tokens, MFA secrets
- Audit log integrity
- Sync provider credentials (GitHub tokens, cloud credentials)

### Adversaries and mitigations (summary)

| Adversary | Example | Primary mitigations |
|---|---|---|
| Stolen DB dump / backup | Leaked Postgres snapshot | Everything sensitive encrypted; root key not in DB |
| Compromised user account | Phished password | MFA/passkeys, SSO, session binding, anomaly alerts, least-privilege roles |
| Malicious or curious insider | Developer reads prod secrets | Env-scoped RBAC, protected envs, JIT access, audit log, approvals |
| Leaked machine token | Token committed to git | Short TTLs, IP allowlists, OIDC federation (no static tokens), token prefix for secret scanning, instant revocation |
| AI agent misuse / prompt injection | Agent exfiltrates `.env` | Agent identities cannot read values through Clavis; allowlisted `run` with masking only; `clavis mcp --agent` keeps a one-hour session in memory, so no agent token sits in a file the agent can read; scoped and expiring. Residual risk: an agent with an unrestricted shell as the user can act as the user, so sandbox agents |
| Web attacks | XSS, CSRF, SSRF | Strict CSP, SameSite cookies + CSRF tokens, SSRF-safe HTTP client for syncs/webhooks |
| Supply chain | Malicious dependency or release | Minimal dependencies, pinned versions, signed releases, SBOM, provenance |
| Tampering with audit log | Admin deletes evidence | Hash chain, external export, signed checkpoints |

### Out of scope
- A fully compromised server host at runtime (an attacker with root on the server can read decrypted secrets in memory). Mitigate with OS hardening, KMS, and short-lived dynamic secrets. Optional E2EE projects address this in the future.
- A compromised client machine.

## 2. Encryption & key hierarchy

```
Root Key (KEK)                         ← never stored in DB
  │  source: key file / env var (default) | AWS KMS | GCP KMS | Azure Key Vault | manual unseal (Shamir)
  ▼
Org Key (256-bit, wrapped by root)      ← orgs.wrapped_org_key
  ▼
Project Key (256-bit, wrapped by org)   ← projects.wrapped_project_key
  ▼  HKDF-SHA256(project_key, info="clavis/secret-value/v1")  → value encryption key
  ▼  HKDF-SHA256(project_key, info="clavis/blind-index/v1")   → HMAC key for key-name lookup
Secret ciphertexts
```

- **Algorithm:** AES-256-GCM (Go stdlib), 96-bit random nonces. The project key encrypts far fewer than 2³² values per key version; rotation is enforced well before limits.
- **AAD binding:** every ciphertext's associated data includes `org_id | project_id | env_id | secret_id | field | version`. A ciphertext copied to another row or field fails to decrypt (prevents swap/replay attacks by someone with DB write access).
- **Ciphertext format:** `v1:<key_version>:<nonce>:<ciphertext+tag>` (binary in DB). It is versioned for crypto agility.
- **Key names are encrypted** and looked up via an HMAC blind index, so a DB dump reveals neither values nor which services you use.
- **No value-level index.** Values have no blind index, so a DB dump cannot reveal which secrets share a value. Duplicate detection (health dashboard) is **opt-in per project** and uses a separate per-project HMAC "fingerprint" key. It never compares values across projects.
- **Variables** (non-sensitive config such as `LOG_LEVEL`) are encrypted at rest exactly like secrets. They differ only in UI masking, reveal auditing, and `use`-mode handling.
- **File secrets** (certificates, JSON credentials, SSH keys) are stored as values up to 1 MiB, encrypted the same way.
- **Rotation:**
  - Root key rotation re-wraps org keys only (fast).
  - Org/project key rotation creates a new key version; a background job re-encrypts ciphertexts lazily, and old versions are destroyed after completion.
- **Root key providers:** `file` (default, auto-generated on first start, 0600 permissions, with a loud warning to back it up), `env`, `aws-kms`, `gcp-kms`, and optional Shamir sealing for the file provider. Manual unseal uses M-of-N shares and a sealed marker.
- **Memory hygiene:** decrypted values are held for the shortest possible time and are never logged. Byte slices are zeroed best-effort. Core dumps are disabled in the server process.

### Future: optional E2EE projects
A per-project "vault mode" where the project key is wrapped to each member's client-side key (X25519) and the server never sees plaintext. Trade-off: syncs, rotation, dynamic secrets, and web search are unavailable for those projects. This is post-1.0.

## 3. Authentication

### Humans
- **First-run setup:** the setup wizard requires a **one-time setup token** printed to the server log and written to `<data-dir>/setup-token` (0600). This stops whoever reaches a fresh server first from claiming admin. The wizard closes permanently once the first admin exists.
- **Passwords:** argon2id (m=64 MiB, t=3, p=2; parameters stored with the hash and upgradable on login). Optional breached-password check against the HIBP k-anonymity API, **off by default** because it contacts a third party. Minimum length 12, no composition rules.
- **MFA:** TOTP (RFC 6238) and **passkeys/WebAuthn** (recommended). Recovery codes hashed. Orgs can require MFA.
- **Passwordless:** passkeys as primary login; email magic links optional.
- **SSO:** OIDC (Google, Microsoft, Okta, generic) and GitHub OAuth; SAML 2.0 with per-org SP metadata, signed AuthnRequests, replay protection, and optional enforcement that blocks password sign-in for members.
- **SSO:** OIDC (Google, GitHub, Microsoft, Okta, Keycloak, any generic) and SAML 2.0. **SCIM 2.0** for user/group provisioning. Orgs can enforce SSO-only login.
- **Sessions:** 256-bit random opaque token in `__Host-` prefixed, `HttpOnly`, `Secure`, `SameSite=Strict` cookie; only the SHA-256 hash is stored. Idle timeout 12h, absolute 7 days (configurable). Sensitive actions (exporting, deleting projects, changing roles) require re-authentication within 10 minutes.
  - *Local development note:* `__Host-` cookies require `Secure`. Chrome and Firefox treat `http://localhost` as secure, Safari does not. In explicit dev mode (`--dev`, localhost only) the server falls back to a non-prefixed cookie and logs a warning.
- **Brute-force protection:** per-account and per-IP rate limits with exponential backoff; no account enumeration (identical responses and timings).

### CLI
- **Device authorization flow (RFC 8628):** `clavis login` opens the browser, the user approves, and the CLI receives a scoped, refreshable token.
- Tokens are stored in the **OS keychain** (macOS Keychain, Windows Credential Manager, Secret Service/libsecret). An encrypted file fallback is used only with an explicit flag.

### Machines and agents
- **Token format:** `clv_<type>_<base62 random 32 bytes><crc32 checksum>`, e.g. `clv_mt_…` (machine token), `clv_pat_…` (personal), `clv_at_…` (agent). The prefix and checksum enable GitHub secret scanning partnership and local leak detection.
- **Only hashes are stored.** Tokens are shown once.
- **OIDC workload federation (preferred):** GitHub Actions, GitLab CI, Kubernetes service accounts, AWS IAM (STS `GetCallerIdentity` signature), GCP, and Azure exchange their native identity for a **short-lived (≤1h) Clavis access token**. No long-lived secrets in CI.
- Optional IP/CIDR allowlists and expiry on every token.

## 4. Authorization

- **Deny by default.** Every API handler declares its required permission; a CI test fails if a route has none.
- **Model:** subject (user / group / machine / agent) → role → permissions, scoped to `org`, `project`, `environment` (glob), and `path` (glob).
- **Permissions:** `secrets:read-names`, `secrets:read-values`, `secrets:use` (inject via run/sync without reveal), `secrets:write`, `secrets:delete`, `envs:manage`, `members:manage`, `roles:manage`, `syncs:manage`, `audit:read`, `settings:manage`, etc.
- **`secrets:use` vs `secrets:read-values`:** this is the foundation of agent-safe mode. `use` lets the server/CLI inject values into a child process (with masking) but not return them via API or print them.
  - *Honest limitation:* any process that receives env vars can read them. An agent that can run arbitrary commands can exfiltrate values (`env`, `curl -d "$KEY" …`, or printing them encoded or reversed). **Output masking is a convenience, not a security boundary.**
  - Therefore agent identities have a **required command allowlist, deny-all by default**. A human must approve command patterns (e.g. `npm test`, `go test ./...`) before `run_with_secrets` works. Agent identities are denied staging and production; they should receive development or preview environments only. Pair with short TTLs, audit, and dynamic secrets for strong guarantees.
- **Built-in roles:** Owner, Admin, Developer (read/write non-protected envs, `use` on protected), Viewer (names only). Custom roles via a visual editor.
- **No privilege escalation:** a user can only grant permissions they hold.
- **Temporary grants:** any role binding can have `expires_at` (powers JIT access).
- **Environment types** (`development`, `preview`, `staging`, `production`) set default policies. Production defaults: protected, change requests required, no personal overrides, revealing a value requires a reason, CLI writes require typed confirmation, agents denied.
- **Protected environments:** non-admin writes require an approved change request or promotion request; Admin/Owner writes are allowed but audited. Reads of values can require an approved access request.
- **Change freezes:** an environment can be frozen for a time window. Only break-glass access (loudly audited, admins notified) can change it.
- **Offboarding:** when a member is removed, all their sessions, PATs, and temporary grants are revoked immediately, and the offboarding assistant lists every secret they revealed or exported so those can be rotated.

## 5. Audit log

- Every authentication event, secret read/write/delete, permission change, export, and settings change is logged. **Secret values are never logged**; key names are logged only as blind-index references plus the encrypted name.
- **Tamper-evident:** each event stores `hash = SHA-256(prev_hash || canonical(event))` per org. A periodic signed checkpoint (Ed25519 instance key) is written and optionally exported.
- **Exports:** webhook, syslog, Splunk HEC, Datadog, Elastic/OpenSearch, and S3. Formats are JSON or CEF. Exporters run asynchronously with bounded queues, so a slow destination cannot block writes. Retention is configurable and unlimited by default.

```yaml
audit:
  exporters:
    - type: webhook
      url: https://siem.example.com/clavis
      format: json
      token: ${AUDIT_WEBHOOK_TOKEN}
    - type: syslog
      address: siem.example.com:514
      network: udp
      format: cef
    - type: splunk
      url: https://splunk.example.com:8088/services/collector/event
      token: ${SPLUNK_HEC_TOKEN}
      format: json
    - type: datadog
      url: https://http-intake.logs.datadoghq.com/api/v2/logs
      api_key: ${DD_API_KEY}
      format: json
    - type: elastic
      url: https://opensearch.example.com/clavis-audit/_doc
      api_key: ${ELASTIC_API_KEY}
      format: json
    - type: s3
      bucket: company-audit
      region: eu-central-1
      prefix: clavis
      access_key: ${AWS_ACCESS_KEY_ID}
      secret_key: ${AWS_SECRET_ACCESS_KEY}
      format: json
```
- A verification command, `clavis admin audit verify`, recomputes the chain.

## 6. Application security

- **CSP:** `default-src 'self'; script-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'none'`. No inline scripts; the SPA is built accordingly.
- Headers: HSTS, `X-Content-Type-Options`, `Referrer-Policy: no-referrer`, `Cross-Origin-Opener-Policy`, `Permissions-Policy`.
- **CSRF:** SameSite=Strict plus a double-submit token on state-changing requests from browsers.
- **SSRF:** outbound HTTP (webhooks, syncs, OIDC discovery) goes through a client that blocks private, link-local, and metadata IP ranges after DNS resolution (configurable allowlist for internal targets).
- **Input limits:** max key length 256, value size 64 KiB (configurable), request body limits, strict JSON decoding.
- **Logging redaction:** the logger wraps secret types (`type Secret []byte` with `LogValue()` returning `[REDACTED]`); tests assert no plaintext ever reaches logs.
- **Share links:** the value is encrypted client-side in the browser with a random key placed in the URL **fragment** (never sent to the server). Supports view limits, expiry, and an optional password. The server stores only the ciphertext and a bcrypt password hash; the key never reaches the server.

## 7. CLI security

- Output masking in `clavis run` is **on by default**: any occurrence of a secret value (and its base64/URL-encoded forms) in stdout/stderr is replaced with `••••••`. It prevents accidental leaks into logs and terminals. It is **not** a defense against a process that deliberately exfiltrates values.
- `clavis run --mount` writes file secrets to a private temp dir (0700, tmpfs where available) and deletes it when the command exits.
- **Local cache** (offline mode) is encrypted with AES-256-GCM using a key stored in the OS keychain; TTL-bounded; can be disabled per project.
- Never writes secrets to disk unless explicitly asked (`clavis export --out .env` warns and offers to add the file to `.gitignore`).
- `clavis scan` checks staged files for known token formats (`clv_`, AWS, Stripe, GitHub, …) for everyone. **Real-value matching** is available only to identities with `secrets:read-values`. It fetches the values into memory, never to disk, and matches them locally. Identities with only `use` get format-based scanning.

## 8. Supply chain & process

- Minimal dependencies; each new dependency requires justification in its PR.
- Go modules and npm packages pinned with checksums; Renovate for updates; `govulncheck` and dependency review in CI.
- Releases built in CI only, **reproducible**, signed with **cosign (keyless/Sigstore)**, with **SBOM (SPDX)** and **SLSA level 3 provenance**.
- Docker images are distroless/static, run as non-root, with a read-only root filesystem.
- Branch protection, required reviews, signed commits for maintainers.
- `SECURITY.md` with a private disclosure process (GitHub Security Advisories) and a 90-day disclosure policy.
- **External security audit before 1.0**; findings published.
- Continuous fuzzing of parsers and crypto envelope code.

## 9. Compliance aids (not certifications)

- Audit exports, access reviews (a "who can access prod" report), key rotation reports, and data residency (self-host anywhere).
- Documentation mapping controls to SOC 2 / ISO 27001 for teams that need evidence.
