# AI coding agents

Clavis gives coding agents a narrow, auditable way to discover and use secrets
without handing them a general-purpose token.

## What an agent gets

- Only `secrets:read-names` and `secrets:use`.
- Staging and production are denied; development and preview are allowed.
- A server-side command allowlist that starts **deny-all**.
- Values are returned only after a command matches the allowlist, and the
  command text is audited before decryption.

Through Clavis, agents cannot read values directly, write secrets, manage
identities, or change their own allowlist.

## What this protects, and what it does not

Clavis controls what reaches the agent **through MCP**. It cannot stop an
agent that has an unrestricted shell running as you: that agent can do
anything you can, including running `clavis` with your login.

The protection is real when the agent's shell is **sandboxed** (for example
Claude Code's sandbox or a devcontainer) and the MCP server runs outside the
sandbox, which is how MCP clients start stdio servers. For that to hold, no
agent credential may sit in a file the agent can read. That is why the
recommended setup below puts no token in the MCP config.

## Create an agent

```sh
clavis identities create claude-code --type agent --project web-app --identity-env '*'
clavis agents allow claude-code "go test ./..." "npm test" "pnpm dev"
```

`clavis agents revoke claude-code --clear` returns the agent to deny-all.

## MCP server

`clavis mcp` speaks the Model Context Protocol over stdio and exposes:

| Tool | Behavior |
| --- | --- |
| `list_secrets` | Key names, kind, schema type, required environments, descriptions. Never values. |
| `check_env` | Required-key and schema coverage checks. Type checks that need values are reported as unchecked. |
| `run_with_secrets` | Checks the command allowlist, decrypts values, runs the command with values in the environment, and masks output. |
| `request_secret` | Records an audited human review request for a missing key. Never creates a value. |

### Recommended: `clavis mcp --agent`

Run `clavis login` once on your machine, then start the MCP server with
`--agent`. It uses your login (stored in the OS keychain) to start a
**one-hour agent session**, keeps the token in memory only, and renews it while
your login stays valid.

- The MCP config contains **no secret**.
- You can only start an agent with access you hold yourself.
- Logging out, or revoking the personal token you used, ends the agent session
  immediately.
- Each session start is audited as `agent.session_start`.

Project, org and environment come from `.clavis.yaml` in the repo (or
`CLAVIS_ORG`, `CLAVIS_PROJECT`, `CLAVIS_ENV`). `CLAVIS_AGENT` can replace the
`--agent` flag.

### Claude Code

```sh
claude mcp add clavis -- clavis mcp --agent claude-code
```

Or add a project `.mcp.json` (safe to commit):

```json
{
  "mcpServers": {
    "clavis": {
      "command": "clavis",
      "args": ["mcp", "--agent", "claude-code"]
    }
  }
}
```

### Cursor

`~/.cursor/mcp.json` or a project `.cursor/mcp.json`:

```json
{
  "mcpServers": {
    "clavis": {
      "command": "clavis",
      "args": ["mcp", "--agent", "cursor"]
    }
  }
}
```

### Codex

`~/.codex/config.toml`:

```toml
[mcp_servers.clavis]
command = "clavis"
args = ["mcp", "--agent", "codex"]
```

### Windsurf

`~/.codeium/windsurf/mcp_config.json`:

```json
{
  "mcpServers": {
    "clavis": {
      "command": "clavis",
      "args": ["mcp", "--agent", "windsurf"]
    }
  }
}
```

### Headless agents (static token)

Agents running without a person logged in (CI bots, remote agents) can use the
agent token printed by `clavis identities create`, with `CLAVIS_TOKEN=clv_at_…`
in the environment of `clavis mcp`. It lasts one day by default, seven days at
most. Keep it out of any file or environment the agent itself can read;
`clavis mcp` prints a warning when it runs this way.

### Other clients

Any MCP client that supports stdio servers can run `clavis mcp --agent <name>`
the same way. Restart the client after changing the configuration.

## Limitations

- **An allowed command receives the secrets.** If that command runs code the
  agent wrote (for example a generated test file), the code can print or send
  the values. Masking is a convenience, not a security boundary.
- **Masking is best-effort.** Encoded, split, transformed, or compressed values
  can evade it. Do not treat masked output as safe to paste into a model.
- **Allowlist patterns are coarse.** `go test *` is a prefix match, not an
  argument-level policy. Prefer exact commands and short-lived tokens.
- **Agents are not sandboxed by Clavis.** Clavis controls which Clavis values a
  command can receive; it does not restrict filesystem, network, or process
  access. Use your agent's sandbox for that.
- **Development and preview are still real environments.** They may contain
  shared staging credentials. Put high-value credentials behind dynamic,
  short-lived issuance and keep production out of agent scope.
- **Human review is still required** for missing keys. `request_secret` records a
  request in the audit log; it does not grant access.
