# AI coding agents

Clavis gives coding agents a narrow, auditable way to discover and use secrets
without handing them a general-purpose token.

## What an agent gets

- Token prefix `clv_at_`.
- Only `secrets:read-names` and `secrets:use`.
- One-day lifetime by default, seven-day maximum.
- Staging and production are denied; development and preview are allowed.
- A server-side command allowlist that starts **deny-all**.
- Values are returned only after a command matches the allowlist, and the
  command text is audited before decryption.

Agents cannot read values directly, write secrets, manage identities, or change
their own allowlist.

## Create an agent

```sh
clavis identities create coding-agent --type agent --project web-app --identity-env '*'
clavis agents allow coding-agent "go test ./..." "npm test" "pnpm dev"
```

The token is printed once. Store it in your MCP client configuration or a local
credential store. `clavis agents revoke coding-agent --clear` returns the agent
to deny-all.

## MCP server

`clavis mcp` speaks the Model Context Protocol over stdio and exposes:

| Tool | Behavior |
| --- | --- |
| `list_secrets` | Key names, kind, schema type, required environments, descriptions. Never values. |
| `check_env` | Required-key and schema coverage checks. Type checks that need values are reported as unchecked. |
| `run_with_secrets` | Checks the command allowlist, decrypts values, runs the command with values in the environment, and masks output. |
| `request_secret` | Records an audited human review request for a missing key. Never creates a value. |

The client must run `clavis mcp` with `CLAVIS_TOKEN` set to the agent token and
the usual `CLAVIS_ORG`, `CLAVIS_PROJECT`, and `CLAVIS_ENV` context.

### Claude Code

Add a project `.mcp.json`:

```json
{
  "mcpServers": {
    "clavis": {
      "command": "clavis",
      "args": ["mcp"],
      "env": {
        "CLAVIS_TOKEN": "clv_at_…",
        "CLAVIS_ORG": "acme",
        "CLAVIS_PROJECT": "web-app",
        "CLAVIS_ENV": "dev"
      }
    }
  }
}
```

Or register it from the CLI:

```sh
claude mcp add clavis --env CLAVIS_TOKEN=clv_at_… --env CLAVIS_ORG=acme \
  --env CLAVIS_PROJECT=web-app --env CLAVIS_ENV=dev -- clavis mcp
```

### Cursor

Add to `~/.cursor/mcp.json` (or a project `.cursor/mcp.json`):

```json
{
  "mcpServers": {
    "clavis": {
      "command": "clavis",
      "args": ["mcp"],
      "env": {
        "CLAVIS_TOKEN": "clv_at_…",
        "CLAVIS_ORG": "acme",
        "CLAVIS_PROJECT": "web-app",
        "CLAVIS_ENV": "dev"
      }
    }
  }
}
```

### Codex

Add to `~/.codex/config.toml`:

```toml
[mcp_servers.clavis]
command = "clavis"
args = ["mcp"]
env = { CLAVIS_TOKEN = "clv_at_…", CLAVIS_ORG = "acme", CLAVIS_PROJECT = "web-app", CLAVIS_ENV = "dev" }
```

### Windsurf

Add to `~/.codeium/windsurf/mcp_config.json`:

```json
{
  "mcpServers": {
    "clavis": {
      "command": "clavis",
      "args": ["mcp"],
      "env": {
        "CLAVIS_TOKEN": "clv_at_…",
        "CLAVIS_ORG": "acme",
        "CLAVIS_PROJECT": "web-app",
        "CLAVIS_ENV": "dev"
      }
    }
  }
}
```

### Other clients

Any MCP client that supports stdio servers can run `clavis mcp` the same way.
Restart the client after changing the configuration or rotating the token.

## Limitations

- **An allowed command receives the secrets.** If that command runs code the
  agent wrote (for example a generated test file), the code can print or send
  the values. Masking is a convenience, not a security boundary.
- **Masking is best-effort.** Encoded, split, transformed, or compressed values
  can evade it. Do not treat masked output as safe to paste into a model.
- **Allowlist patterns are coarse.** `go test *` is a prefix match, not an
  argument-level policy. Prefer exact commands and short-lived tokens.
- **Agents are not sandboxed.** Clavis controls which Clavis values a command can
  receive; it does not restrict filesystem, network, or process access.
- **Development and preview are still real environments.** They may contain
  shared staging credentials. Put high-value credentials behind dynamic,
  short-lived issuance and keep production out of agent scope.
- **Human review is still required** for missing keys. `request_secret` records a
  request in the audit log; it does not grant access.
