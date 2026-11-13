# CLI

Single Go binary `clavis` (same binary as the server). See [Install the CLI](#install-the-cli) for every method. One binary is both the CLI and the server, so the version never drifts between them.

## Install the CLI

| Method | Command | Status |
| --- | --- | --- |
| Install script (signed) | `curl -fsSL https://raw.githubusercontent.com/dumhouse/clavis/main/scripts/install.sh \| bash` | Available |
| Release archive | Download from [GitHub Releases](https://github.com/dumhouse/clavis/releases) | Available |
| `.deb`, `.rpm`, `.apk` | Download from [GitHub Releases](https://github.com/dumhouse/clavis/releases) and install with your package manager | Available |
| Homebrew | `brew install dumhouse/tap/clavis` | Available |
| Scoop | `scoop bucket add dumhouse https://github.com/Dumhouse/scoop-bucket` then `scoop install clavis` | Available |
| Go | `go install github.com/dumhouse/clavis/cmd/clavis@latest` | Available, without the embedded web UI |
| Docker (server) | `docker run dumhouse/clavis server` or `ghcr.io/dumhouse/clavis` | Available |

winget is set up in the release pipeline and goes live once Microsoft accepts the first submission. Hosted apt and rpm repositories are published to Cloudsmith from each release.

## UX rules

- **Pretty by default, scriptable always:** colored tables and prompts on a TTY; `--json` / `--quiet` for scripts; colors disabled automatically when not a TTY or when `NO_COLOR` is set.
- **Every error suggests a fix:** `Not logged in. Run: clavis login`.
- **Context from the repo:** `.clavis.yaml` (committed, contains no secrets) stores org/project/default env, so commands need no flags inside a repo.
- **Non-interactive mode** in CI: authenticates with `CLAVIS_TOKEN` (a machine or agent token) instead of the browser flow; a workload OIDC token can be exchanged for a short-lived token at `POST /api/v1/auth/oidc` (see [ci.md](ci.md)).
- **Production safety:** when the target env is `production`, the CLI shows a colored banner; writes and destructive commands require typing the project name (or `--yes` plus a non-TTY, for CI). Orgs can block CLI writes to production entirely.
- **Stable exit codes:** 0 ok, 1 general error, 2 usage, 3 auth, 4 permission, 5 not found, 6 validation failed (`check`), 7 leak found (`scan`).

## Commands

See [commands.md](commands.md) for the complete, authoritative list of every
implemented command, flag, example, environment variable, and exit code.

Run `clavis --help` or `clavis <command> --help` for live help.

## `.clavis.yaml`

```yaml
server: https://clavis.example.com
org: acme
project: web-app
env: dev               # default env
path: /                # default folder
branch_envs: true      # map git branches to dev/<branch>
cache: true            # encrypted offline cache
```

## Agent-safe mode (`clavis mcp`)

The MCP server exposes tools to AI coding agents:

| Tool | Behavior |
|---|---|
| `list_secrets` | Returns key names, descriptions, and schema info. **Never values.** |
| `check_env` | Reports missing or invalid keys. |
| `run_with_secrets` | Runs an allowed command with secrets injected; output is masked. |
| `request_secret` | Asks a human to add a missing secret (creates a notification). |

It authenticates as an **agent identity** holding only `secrets:read-names` + `secrets:use`. `clavis mcp --agent NAME` starts a one-hour session for that identity with your own `clavis login` and keeps it in memory, so no agent token is written to the MCP config. Logging out ends it. Static agent tokens (`CLAVIS_TOKEN=clv_at_…`) remain for headless agents.

- **Command allowlist is required and deny-all by default.** A human approves patterns (e.g. `npm test`, `go test ./...`, `pnpm dev`) in the UI or with `clavis agents allow`. Allowlists are stored server-side on the identity, not only in `.clavis.yaml`, so the agent can't edit its own allowlist.
- Agents get development/preview environments by default; production requires an explicit org policy.
- Every call is audited with the agent's identity and the exact command.
- **Scope of the guarantee:** Clavis controls what reaches the agent through MCP. An agent with an unrestricted shell running as you can do anything you can; the guarantee holds when the agent's shell is sandboxed.
- **Limitation (stated in docs and UI):** an allowed command still receives the secrets. If that command runs code the agent wrote (e.g. a test file), the agent could exfiltrate values. Masking is a convenience, not a security boundary. Prefer dynamic, short-lived credentials for agent workflows.
