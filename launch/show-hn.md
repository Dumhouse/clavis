# Show HN draft

**Title:** Show HN: Clavis – open-source secrets manager for humans and AI agents

**Body:**

Hi HN — I built Clavis because I wanted a secrets manager that was one binary,
pleasant to self-host, and safe to give to coding agents.

What it does:

- Typed schema for config: string/url/int/bool/json/enum, required per
  environment, patterns, and generated TypeScript/Go/Python loaders.
- Dynamic PostgreSQL roles and AWS STS credentials with leases and automatic
  revocation.
- JIT access requests with multiple approvers, break-glass, and freeze windows.
- Agent identities (`clv_at_…`) that only get read-names + use, denied
  staging/production, with a server-side command allowlist.
- `clavis mcp` for Claude Code/Cursor/Codex/Windsurf, with masking and an honest
  limitation: an allowed command can still exfiltrate secrets.
- Client-side encrypted one-time share links; the server only sees ciphertext.
- SQLite or Postgres, AWS/GCP KMS, audit hash chain, Kubernetes operator,
  External Secrets, and a CSI driver.

What it is not: a hosted SaaS, a Vault replacement for every enterprise feature,
or a sandbox for untrusted code.

Would love feedback on the agent threat model and the schema/codegen workflow.

Repo: https://github.com/dumhouse/clavis
