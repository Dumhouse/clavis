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
| Docker (server) | `docker run -d --name clavis --restart unless-stopped -p 80:8080 -v clavis-data:/data dumhouse/clavis` (see the [install wizard](./install)) or `ghcr.io/dumhouse/clavis` | Available |

winget is set up in the release pipeline and goes live once Microsoft accepts the first submission. Hosted apt and rpm repositories are published to Cloudsmith from each release.

## UX rules

- **Pretty by default, scriptable always:** colored tables and prompts on a TTY; `--json` / `--quiet` for scripts; colors disabled automatically when not a TTY or when `NO_COLOR` is set.
- **Every error suggests a fix:** `Not logged in. Run: clavis login`.
- **Context from the repo:** `.clavis.yaml` (committed, contains no secrets) stores org/project/default env, so commands need no flags inside a repo.
- **Non-interactive mode** in CI: authenticates with `CLAVIS_TOKEN` (a machine or agent token) instead of the browser flow; a workload OIDC token can be exchanged for a short-lived token at `POST /api/v1/auth/oidc` (see [ci.md](ci.md)).
- **Production safety:** when the target env is `production`, the CLI shows a colored banner; writes and destructive commands require typing the project name (or `--yes` plus a non-TTY, for CI). Orgs can block CLI writes to production entirely.
- **Stable exit codes:** 0 ok, 1 general error, 2 usage, 3 auth, 4 permission, 5 not found, 6 validation failed (`check`), 7 leak found (`scan`).

## Commands

The [CLI reference](./reference/) has a page for every command with its
usage, flags and examples. It is generated from the command tree and
`internal/cli/reference.yaml`, the same source as `clavis <command> --help`,
so it always matches the binary. Run `make docs-cli` after changing a command.

The everyday loop:

```sh
clavis login --server https://clavis.example.com
clavis init                 # link this folder, writes .clavis.yaml
clavis run npm run dev      # your command, with secrets as env vars
```

`--` before the command is optional: Clavis stops reading its own flags at
the first word of your command, so `clavis run rails s -p 3001` passes
`-p 3001` to Rails. Clavis flags go before the command
(`clavis run --env staging npm start`).

## `.clavis.yaml`

```yaml
server: https://clavis.example.com
org: acme
project: web-app
env: dev               # default env
path: /                # default folder
run: npm run dev       # what a plain `clavis run` starts
branch_envs: true      # map git branches to dev/<branch>
cache: true            # encrypted offline cache
```

With `run:` set, a plain `clavis run` starts your app. `clavis init` fills it
in when it recognizes the project (npm, pnpm, yarn or bun scripts, Rails,
Django, Laravel, Go, Rust, Maven, Gradle, Docker Compose). See
[`.clavis.yaml`](./reference/clavis-yaml) for every key.

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
