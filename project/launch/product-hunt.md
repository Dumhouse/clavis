# Product Hunt draft

**Tagline:** Open-source secrets management for humans, machines, and AI agents.

**Description:**

Clavis is a self-hosted secrets manager built for modern app teams and coding
agents. One Go binary with an embedded UI, SQLite or Postgres, typed config
schemas, dynamic database/cloud credentials, JIT access approvals, and an
agent-safe MCP mode with server-side command allowlists.

**First comment:**

We built Clavis around three ideas:

1. Secrets should be typed, reviewed, and checked in CI — not just string blobs.
2. Humans and agents need different permissions. Agents get read-names + use,
   never values directly, and only for approved commands.
3. Self-hosting should be one binary, not a platform project.

Happy to answer questions about the crypto model, agent limitations, and
migration from Vault/Infisical/Doppler.
