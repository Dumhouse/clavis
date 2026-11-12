---
description: "Let Claude Code, Cursor, or Codex use project secrets through MCP without ever reading their values."
---

# Give Claude Code safe access to secrets

Coding agents need secrets to run tests and start dev servers, but they should
not read the values. Clavis exposes an MCP server where an agent can list key
names and run an approved command with secrets injected, with output masked.

## Set up

Sign in once, then register the MCP server with an agent session. No token goes
into the config file:

```sh
clavis login
claude mcp add clavis -- clavis mcp --agent claude-code
```

Cursor and Codex use the same `clavis mcp --agent NAME` command. See
[AI coding agents](../agents) for each client's config.

## Approve commands

The agent's command allowlist starts empty. Allow only what it needs:

```sh
clavis identities create claude-code --type agent --project web-app --identity-env '*'
clavis agents allow claude-code "go test ./..." "pnpm test"
```

## What the agent can and cannot do

- It can list key names, check coverage, and run an allowlisted command.
- Staging and production are denied.
- It cannot read, write, or share values, or change its own allowlist.
- Every command is audited before values are decrypted.

## Know the limit

Clavis controls what reaches the agent through MCP. An agent with an
unrestricted shell running as you can still do anything you can. Run the agent
in a sandbox and keep the MCP server outside it. The
[agents page](../agents#what-this-protects-and-what-it-does-not) explains this
in detail.
